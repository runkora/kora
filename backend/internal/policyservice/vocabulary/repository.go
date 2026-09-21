// Package vocabulary provides the Postgres-backed scope vocabulary store.
// The scope vocabulary is the finite, registered set of valid OAuth scope
// strings used for grant validation at write time. It is not consulted
// at runtime — hierarchy resolution uses structural string parsing in Rego.
package vocabulary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScopeEntry is a registered scope with its hierarchy metadata.
type ScopeEntry struct {
	// Scope is the full scope string, e.g. "inference:anthropic:invoke".
	Scope string `json:"scope"`
	// ResourceType is the first segment, e.g. "inference".
	ResourceType string `json:"resource_type"`
	// PathSegments are the middle segments, e.g. ["anthropic"].
	PathSegments []string `json:"path"`
	// Action is the last segment, e.g. "invoke".
	Action string `json:"action"`
	// Description is a human-readable label.
	Description string `json:"description,omitempty"`
	// RegisteredBy is the service that owns this scope (e.g. "inference-gateway").
	// Empty for manually-created scopes.
	RegisteredBy string `json:"registered_by,omitempty"`
	// Requires lists scopes this scope depends on: a grant holding this scope
	// confers the required scopes in the compiled effective set. Empty for
	// ordinary scopes; set by first-party platform MCP tool leaf scopes.
	Requires  []string  `json:"requires,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ScopeEntryWithUsage extends ScopeEntry with usage counts from grants and roles.
type ScopeEntryWithUsage struct {
	ScopeEntry
	GrantCount int `json:"grant_count"`
	RoleCount  int `json:"role_count"`
}

// ErrScopeNotFound is returned when a scope is not in the vocabulary.
var ErrScopeNotFound = errors.New("scope not found in vocabulary")

// Repository defines the scope vocabulary storage contract.
type Repository interface {
	// GetByScope retrieves a scope entry by its full scope string.
	GetByScope(ctx context.Context, scope string) (*ScopeEntry, error)

	// Exists checks whether a scope is registered.
	Exists(ctx context.Context, scope string) (bool, error)

	// List returns all registered scopes, optionally filtered by resource type.
	List(ctx context.Context, resourceType string) ([]*ScopeEntry, error)

	// Upsert inserts a scope entry or updates its metadata if it already exists,
	// and reports whether the catalog actually changed. Used by route
	// registration so services can declare their scopes idempotently, and by
	// the caller to tell a re-registration that changed nothing from one that
	// needs the bundles recompiled.
	Upsert(ctx context.Context, e *ScopeEntry) (bool, error)

	// DeleteOrphans deletes all scopes owned by registeredBy whose scope strings
	// are not in keepScopes, and returns the deleted scope strings. Called on
	// re-registration to prune scopes for virtual models that have been deleted.
	DeleteOrphans(ctx context.Context, registeredBy string, keepScopes []string) ([]string, error)

	// ListWithUsage returns all scopes with grant and role usage counts for a tenant.
	ListWithUsage(ctx context.Context, resourceType, tenantID string) ([]*ScopeEntryWithUsage, error)

	// CountByResourceType returns the count of scopes grouped by resource type.
	CountByResourceType(ctx context.Context) (map[string]int, error)
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a Repository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) Upsert(ctx context.Context, e *ScopeEntry) (bool, error) {
	if e.PathSegments == nil {
		e.PathSegments = []string{}
	}
	pathJSON, err := json.Marshal(e.PathSegments)
	if err != nil {
		return false, fmt.Errorf("marshal path segments: %w", err)
	}

	var registeredBy *string
	if e.RegisteredBy != "" {
		registeredBy = &e.RegisteredBy
	}

	requires := e.Requires
	if requires == nil {
		requires = []string{}
	}

	// The DO UPDATE is guarded so a row that already matches is left alone and
	// RETURNING yields nothing. That is what makes an idempotent
	// re-registration reportable as "nothing changed".
	row := r.pool.QueryRow(ctx, `
		INSERT INTO policy_scope_vocabulary (scope, resource_type, path_segments, action, description, registered_by, requires, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (scope) DO UPDATE SET
			resource_type  = EXCLUDED.resource_type,
			path_segments  = EXCLUDED.path_segments,
			action         = EXCLUDED.action,
			description    = EXCLUDED.description,
			registered_by  = COALESCE(policy_scope_vocabulary.registered_by, EXCLUDED.registered_by),
			-- Only overwrite requires with a non-empty incoming set. A degraded or
			-- partial (SkipPrune) introspection can re-register a scope with empty
			-- requires; without this it would silently wipe the upstream-scope
			-- conferral and break grants on the next bundle compile.
			requires       = CASE
				WHEN array_length(EXCLUDED.requires, 1) > 0 THEN EXCLUDED.requires
				ELSE policy_scope_vocabulary.requires
			END
		WHERE policy_scope_vocabulary.resource_type IS DISTINCT FROM EXCLUDED.resource_type
			OR policy_scope_vocabulary.path_segments IS DISTINCT FROM EXCLUDED.path_segments
			OR policy_scope_vocabulary.action IS DISTINCT FROM EXCLUDED.action
			OR policy_scope_vocabulary.description IS DISTINCT FROM EXCLUDED.description
			OR (policy_scope_vocabulary.registered_by IS NULL AND EXCLUDED.registered_by IS NOT NULL)
			OR (array_length(EXCLUDED.requires, 1) > 0
				AND policy_scope_vocabulary.requires IS DISTINCT FROM EXCLUDED.requires)
		RETURNING scope
	`, e.Scope, e.ResourceType, pathJSON, e.Action, e.Description, registeredBy, requires)

	var scope string
	switch err := row.Scan(&scope); {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("upsert scope: %w", err)
	}
	return true, nil
}

func (r *postgresRepository) DeleteOrphans(ctx context.Context, registeredBy string, keepScopes []string) ([]string, error) {
	var rows pgx.Rows
	var err error
	if len(keepScopes) == 0 {
		rows, err = r.pool.Query(ctx,
			`DELETE FROM policy_scope_vocabulary WHERE registered_by = $1 RETURNING scope`,
			registeredBy)
	} else {
		rows, err = r.pool.Query(ctx,
			`DELETE FROM policy_scope_vocabulary WHERE registered_by = $1 AND scope != ALL($2) RETURNING scope`,
			registeredBy, keepScopes)
	}
	if err != nil {
		return nil, fmt.Errorf("delete orphan scopes: %w", err)
	}
	defer rows.Close()

	var deleted []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, fmt.Errorf("scan deleted scope: %w", err)
		}
		deleted = append(deleted, scope)
	}
	return deleted, rows.Err()
}

func (r *postgresRepository) GetByScope(ctx context.Context, scope string) (*ScopeEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT scope, resource_type, path_segments, action, description, requires, created_at
		FROM policy_scope_vocabulary WHERE scope = $1
	`, scope)
	return r.scanEntry(row)
}

func (r *postgresRepository) Exists(ctx context.Context, scope string) (bool, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(1) FROM policy_scope_vocabulary WHERE scope = $1`, scope).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check scope exists: %w", err)
	}
	return count > 0, nil
}

func (r *postgresRepository) List(ctx context.Context, resourceType string) ([]*ScopeEntry, error) {
	var rows pgx.Rows
	var err error
	if resourceType != "" {
		rows, err = r.pool.Query(ctx, `
			SELECT scope, resource_type, path_segments, action, description, requires, created_at
			FROM policy_scope_vocabulary WHERE resource_type = $1 ORDER BY scope
		`, resourceType)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT scope, resource_type, path_segments, action, description, requires, created_at
			FROM policy_scope_vocabulary ORDER BY scope
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("list vocabulary: %w", err)
	}
	defer rows.Close()

	var entries []*ScopeEntry
	for rows.Next() {
		e, err := r.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// scanEntry works for both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func (r *postgresRepository) ListWithUsage(ctx context.Context, resourceType, tenantID string) ([]*ScopeEntryWithUsage, error) {
	query := `
		WITH grant_usage AS (
			SELECT scope, COUNT(*) AS grant_count
			FROM policy_grants g, jsonb_array_elements_text(g.scopes) AS scope
			WHERE g.tenant_id = $1
			GROUP BY scope
		), role_usage AS (
			SELECT scope, COUNT(*) AS role_count
			FROM policy_role_scopes r, jsonb_array_elements_text(r.scopes) AS scope
			WHERE r.tenant_id = $1
			GROUP BY scope
		)
		SELECT v.scope, v.resource_type, v.path_segments, v.action, v.description, v.created_at,
			COALESCE(gu.grant_count, 0),
			COALESCE(ru.role_count, 0)
		FROM policy_scope_vocabulary v
		LEFT JOIN grant_usage gu ON gu.scope = v.scope
		LEFT JOIN role_usage ru ON ru.scope = v.scope`
	args := []any{tenantID}
	if resourceType != "" {
		query += ` WHERE v.resource_type = $2`
		args = append(args, resourceType)
	}
	query += ` ORDER BY v.scope`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list vocabulary with usage: %w", err)
	}
	defer rows.Close()

	var entries []*ScopeEntryWithUsage
	for rows.Next() {
		var e ScopeEntryWithUsage
		var pathJSON []byte
		if err := rows.Scan(&e.Scope, &e.ResourceType, &pathJSON, &e.Action, &e.Description, &e.CreatedAt, &e.GrantCount, &e.RoleCount); err != nil {
			return nil, fmt.Errorf("scan scope entry with usage: %w", err)
		}
		if err := json.Unmarshal(pathJSON, &e.PathSegments); err != nil {
			return nil, fmt.Errorf("unmarshal path segments: %w", err)
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

func (r *postgresRepository) CountByResourceType(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `SELECT resource_type, COUNT(*) FROM policy_scope_vocabulary GROUP BY resource_type`)
	if err != nil {
		return nil, fmt.Errorf("count vocabulary by resource type: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var rt string
		var count int
		if err := rows.Scan(&rt, &count); err != nil {
			return nil, fmt.Errorf("scan vocabulary count: %w", err)
		}
		result[rt] = count
	}
	return result, rows.Err()
}

func (r *postgresRepository) scanEntry(s scanner) (*ScopeEntry, error) {
	var e ScopeEntry
	var pathJSON []byte
	err := s.Scan(&e.Scope, &e.ResourceType, &pathJSON, &e.Action, &e.Description, &e.Requires, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrScopeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan scope entry: %w", err)
	}
	if err := json.Unmarshal(pathJSON, &e.PathSegments); err != nil {
		return nil, fmt.Errorf("unmarshal path segments: %w", err)
	}
	return &e, nil
}
