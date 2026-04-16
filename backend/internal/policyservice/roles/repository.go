// Package roles provides the Postgres-backed role→scope mapping store for the
// Policy Service. A role entry maps an OIDC role name within a tenant to a set
// of platform scopes. Users carrying that role in their JWT are granted those
// scopes without a per-user grant.
//
// Platform admin roles (tenant-admin, platform-admin) are hardcoded in the
// bundle compiler and are not managed here.
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
)

// RoleEntry maps an OIDC role name to platform scopes within a tenant.
type RoleEntry struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Role      string    `json:"role"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
}

// ErrRoleNotFound is returned when no mapping exists for the given role.
var ErrRoleNotFound = errors.New("role not found")

// Repository defines the role→scope storage contract.
type Repository interface {
	// Upsert creates or replaces the scope mapping for a role within a tenant.
	// If the role already exists, its scopes are replaced in full.
	Upsert(ctx context.Context, entry *RoleEntry) error

	// GetByRole retrieves the scope mapping for a specific role within a tenant.
	GetByRole(ctx context.Context, tenantID, role string) (*RoleEntry, error)

	// ListByTenant returns role→scope mappings for a tenant, paginated by limit
	// and offset. Use limit=0 for unbounded (e.g. bundle compilation).
	ListByTenant(ctx context.Context, tenantID string, limit, offset int) ([]*RoleEntry, error)

	// Delete removes the scope mapping for a role within a tenant.
	Delete(ctx context.Context, tenantID, role string) error

	// CountByTenant returns the number of custom (non-reserved) roles for a tenant.
	CountByTenant(ctx context.Context, tenantID string) (int, error)

	// PurgeScopeFromAll removes a scope string from every role's scopes array
	// across all tenants. Called when a vocabulary scope is deleted.
	PurgeScopeFromAll(ctx context.Context, scope string) error
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

	// RETURNING ensures we get the actual stored id and created_at — on conflict
	// the original row's id and created_at are preserved, not the new values.
	row := r.pool.QueryRow(ctx, `
		INSERT INTO policy_role_scopes (id, tenant_id, role, scopes, created_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (tenant_id, role)
		DO UPDATE SET scopes = EXCLUDED.scopes
		RETURNING id, created_at
	`, entry.ID, entry.TenantID, entry.Role, scopesJSON)

	if err := row.Scan(&entry.ID, &entry.CreatedAt); err != nil {
		return fmt.Errorf("upsert role: %w", err)
	}
	return nil
}

func (r *postgresRepository) GetByRole(ctx context.Context, tenantID, role string) (*RoleEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, role, scopes, created_at
		FROM policy_role_scopes WHERE tenant_id = $1 AND role = $2
	`, tenantID, role)
	return r.scanEntry(row)
}

func (r *postgresRepository) ListByTenant(ctx context.Context, tenantID string, limit, offset int) ([]*RoleEntry, error) {
	query := `SELECT id, tenant_id, role, scopes, created_at
		FROM policy_role_scopes WHERE tenant_id = $1 ORDER BY role`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	}
	rows, err := r.pool.Query(ctx, query, tenantID)
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
		DELETE FROM policy_role_scopes WHERE tenant_id = $1 AND role = $2
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
		WHERE scopes @> jsonb_build_array($1)
	`, scope)
	if err != nil {
		return fmt.Errorf("purge scope from roles: %w", err)
	}
	return nil
}

func (r *postgresRepository) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM policy_role_scopes WHERE tenant_id = $1`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count roles by tenant: %w", err)
	}
	return count, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *postgresRepository) scanEntry(s scanner) (*RoleEntry, error) {
	var e RoleEntry
	var scopesJSON []byte
	err := s.Scan(&e.ID, &e.TenantID, &e.Role, &scopesJSON, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan role entry: %w", err)
	}
	if err := json.Unmarshal(scopesJSON, &e.Scopes); err != nil {
		return nil, fmt.Errorf("unmarshal scopes: %w", err)
	}
	return &e, nil
}
