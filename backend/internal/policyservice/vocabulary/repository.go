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
	"github.com/jackc/pgx/v5/pgconn"
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
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ErrScopeNotFound is returned when a scope is not in the vocabulary.
var ErrScopeNotFound = errors.New("scope not found in vocabulary")

// ErrScopeAlreadyExists is returned when registering a duplicate scope.
var ErrScopeAlreadyExists = errors.New("scope already registered")

// Repository defines the scope vocabulary storage contract.
type Repository interface {
	// Register adds a new scope to the vocabulary.
	Register(ctx context.Context, e *ScopeEntry) error

	// GetByScope retrieves a scope entry by its full scope string.
	GetByScope(ctx context.Context, scope string) (*ScopeEntry, error)

	// Exists checks whether a scope is registered.
	Exists(ctx context.Context, scope string) (bool, error)

	// List returns all registered scopes, optionally filtered by resource type.
	List(ctx context.Context, resourceType string) ([]*ScopeEntry, error)

	// Delete removes a scope from the vocabulary.
	Delete(ctx context.Context, scope string) error
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a Repository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) Register(ctx context.Context, e *ScopeEntry) error {
	e.CreatedAt = time.Now().UTC()
	pathJSON, err := json.Marshal(e.PathSegments)
	if err != nil {
		return fmt.Errorf("marshal path segments: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO policy_scope_vocabulary (scope, resource_type, path_segments, action, description, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, e.Scope, e.ResourceType, pathJSON, e.Action, e.Description, e.CreatedAt)
	if err != nil {
		// pgx returns a PgError with Code "23505" for unique violation
		if isDuplicateKeyError(err) {
			return ErrScopeAlreadyExists
		}
		return fmt.Errorf("register scope: %w", err)
	}
	return nil
}

func (r *postgresRepository) GetByScope(ctx context.Context, scope string) (*ScopeEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT scope, resource_type, path_segments, action, description, created_at
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
			SELECT scope, resource_type, path_segments, action, description, created_at
			FROM policy_scope_vocabulary WHERE resource_type = $1 ORDER BY scope
		`, resourceType)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT scope, resource_type, path_segments, action, description, created_at
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

func (r *postgresRepository) Delete(ctx context.Context, scope string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM policy_scope_vocabulary WHERE scope = $1`, scope)
	if err != nil {
		return fmt.Errorf("delete scope: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrScopeNotFound
	}
	return nil
}

// scanEntry works for both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func (r *postgresRepository) scanEntry(s scanner) (*ScopeEntry, error) {
	var e ScopeEntry
	var pathJSON []byte
	err := s.Scan(&e.Scope, &e.ResourceType, &pathJSON, &e.Action, &e.Description, &e.CreatedAt)
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

func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
