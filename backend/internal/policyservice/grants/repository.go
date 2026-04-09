// Package grants provides the Postgres-backed grant store for the Policy Service.
// A grant asserts that a named principal within a tenant holds a set of
// OAuth-style permission scopes. It is the unit of policy in the Orbit platform.
package grants

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

// PrincipalType identifies the category of actor a grant applies to.
type PrincipalType string

const (
	PrincipalTypeAgent   PrincipalType = "AGENT"
	PrincipalTypeHuman   PrincipalType = "HUMAN"
	PrincipalTypeService PrincipalType = "SERVICE"
)

// Grant asserts that a named principal within a tenant holds a set of
// OAuth-style permission scopes. Cross-tenant grants do not exist.
type Grant struct {
	ID            string        `json:"id"`
	TenantID      string        `json:"tenant_id"`
	PrincipalType PrincipalType `json:"principal_type"`
	PrincipalID   string        `json:"principal_id"`
	Scopes        []string      `json:"scopes"`
	CreatedAt     time.Time     `json:"created_at"`
}

// ErrGrantNotFound is returned when a grant with the given ID does not exist.
var ErrGrantNotFound = errors.New("grant not found")

// Repository defines the grant storage contract.
type Repository interface {
	// Create persists a new grant. The grant ID is generated internally.
	Create(ctx context.Context, g *Grant) error

	// GetByID retrieves a grant by its primary ID.
	GetByID(ctx context.Context, id string) (*Grant, error)

	// ListByPrincipal returns all grants for a given principal within a tenant.
	ListByPrincipal(ctx context.Context, tenantID string, principalType PrincipalType, principalID string) ([]*Grant, error)

	// ListByTenant returns all grants for a tenant. Used for bundle compilation.
	ListByTenant(ctx context.Context, tenantID string) ([]*Grant, error)

	// Delete removes a grant by ID. Returns ErrGrantNotFound if it does not exist.
	Delete(ctx context.Context, id string) error

	// ListDistinctTenants returns the unique tenant IDs that have at least one grant.
	// Used at startup to pre-compile bundles for all active tenants.
	ListDistinctTenants(ctx context.Context) ([]string, error)
}

// postgresRepository is the Postgres-backed implementation.
type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a Repository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) Create(ctx context.Context, g *Grant) error {
	g.ID = uuid.New().String()
	g.CreatedAt = time.Now().UTC()

	scopesJSON, err := json.Marshal(g.Scopes)
	if err != nil {
		return fmt.Errorf("marshal scopes: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO policy_grants (id, tenant_id, principal_type, principal_id, scopes, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, g.ID, g.TenantID, string(g.PrincipalType), g.PrincipalID, scopesJSON, g.CreatedAt)
	if err != nil {
		return fmt.Errorf("create grant: %w", err)
	}
	return nil
}

func (r *postgresRepository) GetByID(ctx context.Context, id string) (*Grant, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, principal_type, principal_id, scopes, created_at
		FROM policy_grants WHERE id = $1
	`, id)
	return r.scanGrant(row)
}

func (r *postgresRepository) ListByPrincipal(ctx context.Context, tenantID string, principalType PrincipalType, principalID string) ([]*Grant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, principal_type, principal_id, scopes, created_at
		FROM policy_grants
		WHERE tenant_id = $1 AND principal_type = $2 AND principal_id = $3
		ORDER BY created_at
	`, tenantID, string(principalType), principalID)
	if err != nil {
		return nil, fmt.Errorf("list grants by principal: %w", err)
	}
	defer rows.Close()
	return r.collectGrants(rows)
}

func (r *postgresRepository) ListByTenant(ctx context.Context, tenantID string) ([]*Grant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, principal_type, principal_id, scopes, created_at
		FROM policy_grants
		WHERE tenant_id = $1
		ORDER BY principal_type, principal_id, created_at
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list grants by tenant: %w", err)
	}
	defer rows.Close()
	return r.collectGrants(rows)
}

func (r *postgresRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM policy_grants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete grant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrGrantNotFound
	}
	return nil
}

func (r *postgresRepository) ListDistinctTenants(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT tenant_id FROM policy_grants ORDER BY tenant_id`)
	if err != nil {
		return nil, fmt.Errorf("list distinct tenants: %w", err)
	}
	defer rows.Close()

	var tenants []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tenant id: %w", err)
		}
		tenants = append(tenants, id)
	}
	return tenants, rows.Err()
}

func (r *postgresRepository) scanGrant(row pgx.Row) (*Grant, error) {
	var g Grant
	var principalType string
	var scopesJSON []byte

	err := row.Scan(&g.ID, &g.TenantID, &principalType, &g.PrincipalID, &scopesJSON, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGrantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan grant: %w", err)
	}

	g.PrincipalType = PrincipalType(principalType)
	if err := json.Unmarshal(scopesJSON, &g.Scopes); err != nil {
		return nil, fmt.Errorf("unmarshal scopes: %w", err)
	}
	return &g, nil
}

func (r *postgresRepository) collectGrants(rows pgx.Rows) ([]*Grant, error) {
	var grants []*Grant
	for rows.Next() {
		var g Grant
		var principalType string
		var scopesJSON []byte
		if err := rows.Scan(&g.ID, &g.TenantID, &principalType, &g.PrincipalID, &scopesJSON, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan grant row: %w", err)
		}
		g.PrincipalType = PrincipalType(principalType)
		if err := json.Unmarshal(scopesJSON, &g.Scopes); err != nil {
			return nil, fmt.Errorf("unmarshal scopes: %w", err)
		}
		grants = append(grants, &g)
	}
	return grants, rows.Err()
}
