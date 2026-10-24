// Package roles provides the Postgres-backed role→scope mapping store for the
// Policy Service. A role entry maps an OIDC role name within a tenant to a set
// of platform scopes. Users carrying that role in their JWT are granted those
// scopes without a per-user grant.
//
// Reserved roles (platform-admin and the built-in kora-* roles) are derived from
// the vocabulary in the bundle compiler and are not managed here.
package roles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

// BudgetAssignment is the inline budget configuration on a role.
// A nil pointer means "no budget configured for this category".
// Pooled=false (default): per-member allowance — every principal asserting the
// role gets LimitUSD per window. Pooled=true: one shared pot — all members'
// spend accumulates against a single counter.
type BudgetAssignment struct {
	LimitUSD     float64 `json:"limit_usd"`
	ResetCadence string  `json:"reset_cadence"`
	Pooled       bool    `json:"pooled,omitempty"`
}

// RoleEntry maps an OIDC role name to platform scopes within a tenant.
type RoleEntry struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	Role      string            `json:"role"`
	Scopes    []string          `json:"scopes"`
	Budget    *BudgetAssignment `json:"budget,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	// Source is declarative.SourceAPI or declarative.SourceCRD. Derived reserved roles have none.
	Source string `json:"source,omitempty"`
	// ResourceName and ResourceUID name the PolicyRole a crd row is projected from.
	ResourceName string `json:"-"`
	ResourceUID  string `json:"-"`

	// Reserved and UnregisteredScopes are set only on derived reserved roles
	// and are never stored. UnregisteredScopes lists a built-in role's declared
	// scopes the vocabulary does not hold yet.
	Reserved           bool     `json:"reserved,omitempty"`
	UnregisteredScopes []string `json:"unregistered_scopes,omitempty"`
}

// ErrRoleNotFound is returned when no mapping exists for the given role.
var ErrRoleNotFound = errors.New("role not found")

// ErrRoleTaken is returned when a declared write targets a role held by an api
// row or by another resource.
var ErrRoleTaken = errors.New("role is held by another item")

// Repository defines the role→scope storage contract.
type Repository interface {
	// Upsert creates or replaces the scope mapping for a role within a tenant.
	// If the role already exists, its scopes are replaced in full.
	Upsert(ctx context.Context, entry *RoleEntry) error

	// UpdateBudget sets or clears a role's budget and leaves its scopes alone.
	// It returns the updated entry, or ErrRoleNotFound.
	UpdateBudget(ctx context.Context, tenantID, role string, budget *BudgetAssignment) (*RoleEntry, error)

	// GetByRole retrieves the scope mapping for a specific role within a tenant.
	GetByRole(ctx context.Context, tenantID, role string) (*RoleEntry, error)

	// ListByTenant returns role→scope mappings for a tenant, without the roles named in exclude,
	// paginated by limit and offset. Use limit=0 for unbounded (e.g. bundle compilation).
	ListByTenant(ctx context.Context, tenantID string, exclude []string, limit, offset int) ([]*RoleEntry, error)

	// Delete removes the scope mapping for a role within a tenant.
	Delete(ctx context.Context, tenantID, role string) error

	// CountByTenant returns the number of stored roles for a tenant, without the roles named in exclude.
	CountByTenant(ctx context.Context, tenantID string, exclude []string) (int, error)

	// PurgeScopeFromAll removes a scope string from every role's scopes array
	// across all tenants. Called when a vocabulary scope is deleted.
	PurgeScopeFromAll(ctx context.Context, scope string) error

	// UpsertDeclared writes a crd row keyed by tenant and role, scopes and budget in full.
	// It drops the resource's row under a previous role name. It returns ErrRoleTaken
	// when an api row or another resource's row holds the role.
	UpsertDeclared(ctx context.Context, entry *RoleEntry) error

	// ListDeclared returns the resource behind each of the tenant's crd rows.
	ListDeclared(ctx context.Context, tenantID string) ([]declarative.Declared, error)

	// DeleteDeclared removes the tenant's crd row projected from the named
	// resource. It reports whether a row was deleted.
	DeleteDeclared(ctx context.Context, tenantID, resourceName string) (bool, error)
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a Repository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) Upsert(ctx context.Context, entry *RoleEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	scopesJSON, err := json.Marshal(entry.Scopes)
	if err != nil {
		return fmt.Errorf("marshal scopes: %w", err)
	}

	var budgetJSON []byte
	if entry.Budget != nil {
		if budgetJSON, err = json.Marshal(entry.Budget); err != nil {
			return fmt.Errorf("marshal budget: %w", err)
		}
	}

	// RETURNING ensures we get the actual stored id and created_at — on conflict
	// the original row's id and created_at are preserved, not the new values.
	row := r.pool.QueryRow(ctx, `
		INSERT INTO policy_role_scopes (id, tenant_id, role, scopes, budget, created_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (tenant_id, role)
		DO UPDATE SET
			scopes = EXCLUDED.scopes,
			budget = COALESCE(EXCLUDED.budget, policy_role_scopes.budget)
		WHERE policy_role_scopes.source = 'api'
		RETURNING id, created_at
	`, entry.ID, entry.TenantID, entry.Role, scopesJSON, budgetJSON)

	err = row.Scan(&entry.ID, &entry.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return declarative.ErrManagedByResource
	}
	if err != nil {
		return fmt.Errorf("upsert role: %w", err)
	}
	entry.Source = declarative.SourceAPI
	return nil
}

func (r *postgresRepository) UpsertDeclared(ctx context.Context, entry *RoleEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	scopesJSON, err := json.Marshal(entry.Scopes)
	if err != nil {
		return fmt.Errorf("marshal scopes: %w", err)
	}
	var budgetJSON []byte
	if entry.Budget != nil {
		if budgetJSON, err = json.Marshal(entry.Budget); err != nil {
			return fmt.Errorf("marshal budget: %w", err)
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin declared role upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, `
		DELETE FROM policy_role_scopes
		WHERE tenant_id = $1 AND source = 'crd' AND resource_name = $2 AND role <> $3
	`, entry.TenantID, entry.ResourceName, entry.Role); err != nil {
		return fmt.Errorf("drop renamed declared role: %w", err)
	}
	// A resource recreated under the same name takes its old row back with its new UID.
	err = tx.QueryRow(ctx, `
		INSERT INTO policy_role_scopes (id, tenant_id, role, scopes, budget, created_at, source, resource_uid, resource_name)
		VALUES ($1, $2, $3, $4, $5, now(), 'crd', $6, $7)
		ON CONFLICT (tenant_id, role)
		DO UPDATE SET
			scopes = EXCLUDED.scopes,
			budget = EXCLUDED.budget,
			resource_uid = EXCLUDED.resource_uid
		WHERE policy_role_scopes.source = 'crd'
		  AND (policy_role_scopes.resource_uid = EXCLUDED.resource_uid OR policy_role_scopes.resource_name = EXCLUDED.resource_name)
		RETURNING id, created_at
	`, entry.ID, entry.TenantID, entry.Role, scopesJSON, budgetJSON, entry.ResourceUID, entry.ResourceName).Scan(&entry.ID, &entry.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRoleTaken
	}
	if err != nil {
		return fmt.Errorf("upsert declared role: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit declared role upsert: %w", err)
	}
	entry.Source = declarative.SourceCRD
	return nil
}

func (r *postgresRepository) ListDeclared(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	rows, err := r.pool.Query(ctx, `SELECT resource_name, resource_uid
		FROM policy_role_scopes WHERE tenant_id = $1 AND source = 'crd' ORDER BY resource_name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list declared roles: %w", err)
	}
	declared, err := pgx.CollectRows(rows, pgx.RowToStructByPos[declarative.Declared])
	if err != nil {
		return nil, fmt.Errorf("list declared roles: %w", err)
	}
	return declared, nil
}

func (r *postgresRepository) DeleteDeclared(ctx context.Context, tenantID, resourceName string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM policy_role_scopes WHERE tenant_id = $1 AND source = 'crd' AND resource_name = $2
	`, tenantID, resourceName)
	if err != nil {
		return false, fmt.Errorf("delete declared role: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *postgresRepository) UpdateBudget(ctx context.Context, tenantID, role string, budget *BudgetAssignment) (*RoleEntry, error) {
	var budgetJSON []byte
	if budget != nil {
		var err error
		budgetJSON, err = json.Marshal(budget)
		if err != nil {
			return nil, fmt.Errorf("marshal budget: %w", err)
		}
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE policy_role_scopes SET budget = $3 WHERE tenant_id = $1 AND role = $2 AND source = 'api'
		RETURNING `+entryColumns+`
	`, tenantID, role, budgetJSON)
	return r.scanEntry(row)
}

func (r *postgresRepository) GetByRole(ctx context.Context, tenantID, role string) (*RoleEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+entryColumns+`
		FROM policy_role_scopes WHERE tenant_id = $1 AND role = $2
	`, tenantID, role)
	return r.scanEntry(row)
}

func (r *postgresRepository) ListByTenant(ctx context.Context, tenantID string, exclude []string, limit, offset int) ([]*RoleEntry, error) {
	query := `SELECT ` + entryColumns + `
		FROM policy_role_scopes WHERE tenant_id = $1 AND role <> ALL($2::text[]) ORDER BY role`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	}
	rows, err := r.pool.Query(ctx, query, tenantID, nonNil(exclude))
	if err != nil {
		return nil, fmt.Errorf("list role scopes: %w", err)
	}
	defer rows.Close()

	var entries []*RoleEntry
	for rows.Next() {
		e, err := r.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *postgresRepository) Delete(ctx context.Context, tenantID, role string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM policy_role_scopes WHERE tenant_id = $1 AND role = $2 AND source = 'api'
	`, tenantID, role)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

func (r *postgresRepository) PurgeScopeFromAll(ctx context.Context, scope string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE policy_role_scopes
		SET scopes = (
			SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb)
			FROM jsonb_array_elements_text(scopes) AS elem
			WHERE elem != $1
		)
		WHERE scopes @> jsonb_build_array($1::text)
	`, scope)
	if err != nil {
		return fmt.Errorf("purge scope from roles: %w", err)
	}
	return nil
}

func (r *postgresRepository) CountByTenant(ctx context.Context, tenantID string, exclude []string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM policy_role_scopes WHERE tenant_id = $1 AND role <> ALL($2::text[])`,
		tenantID, nonNil(exclude)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count roles by tenant: %w", err)
	}
	return count, nil
}

// nonNil turns a nil slice into an empty one. pgx sends nil as NULL, and <> ALL(NULL) matches no row.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// entryColumns is the column list scanEntry reads.
const entryColumns = `id, tenant_id, role, scopes, budget, created_at, source, COALESCE(resource_uid, ''), COALESCE(resource_name, '')`

type scanner interface {
	Scan(dest ...any) error
}

func (r *postgresRepository) scanEntry(s scanner) (*RoleEntry, error) {
	var e RoleEntry
	var scopesJSON []byte
	var budgetJSON []byte
	err := s.Scan(&e.ID, &e.TenantID, &e.Role, &scopesJSON, &budgetJSON, &e.CreatedAt, &e.Source, &e.ResourceUID, &e.ResourceName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan role entry: %w", err)
	}
	if err := json.Unmarshal(scopesJSON, &e.Scopes); err != nil {
		return nil, fmt.Errorf("unmarshal scopes: %w", err)
	}
	if len(budgetJSON) > 0 && string(budgetJSON) != "null" {
		var b BudgetAssignment
		if err := json.Unmarshal(budgetJSON, &b); err != nil {
			return nil, fmt.Errorf("unmarshal budget: %w", err)
		}
		e.Budget = &b
	}
	return &e, nil
}
