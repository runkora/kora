// Package principalroles reads the per-tenant user→role membership snapshot
// that the bundle compiler emits as principal_roles/data.json.
//
// Role membership is IDP-asserted and single-sourced in the user_profiles
// snapshot, refreshed on every login (see
// docs/adr/2026-06-15-role-budget-attribution-for-non-interactive-credentials.md).
// Interactive callers carry roles in their JWT, but the DELEGATOR of a
// delegated agent run is not the caller — the rego delegator leg must resolve
// the person's role-derived access offline from bundle data
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 5). This package
// is the compile-time counterpart of the request-time lookup the inference
// gateway already performs for PATs (GetPrincipalRoles): same table, same
// source of truth, same login-cadence freshness bound.
package principalroles

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the storage contract for principal→role membership snapshots.
type Repository interface {
	// ListByTenant returns principal ID → current role names for every user in
	// the tenant whose profile snapshot carries at least one role. Principals
	// with no roles are absent (an absent key and an empty role set are
	// equivalent for the rego delegator leg).
	ListByTenant(ctx context.Context, tenantID string) (map[string][]string, error)
}

// PostgresRepository implements Repository against the user_profiles table.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository constructs the repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// ListByTenant returns the tenant's user→roles membership map from the
// user_profiles role snapshot.
func (r *PostgresRepository) ListByTenant(ctx context.Context, tenantID string) (map[string][]string, error) {
	const q = `
		SELECT id, roles
		FROM user_profiles
		WHERE tenant_id = $1
		  AND roles IS NOT NULL
		  AND cardinality(roles) > 0`

	rows, err := r.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list principal roles for tenant %s: %w", tenantID, err)
	}
	defer rows.Close()

	out := make(map[string][]string)
	for rows.Next() {
		var id string
		var roles []string
		if err := rows.Scan(&id, &roles); err != nil {
			return nil, fmt.Errorf("scan principal roles: %w", err)
		}
		out[id] = roles
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("principal roles rows: %w", err)
	}
	return out, nil
}
