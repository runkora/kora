package quotas

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository implements Repository against PostgreSQL.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository constructs the repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// ─── Groups ───────────────────────────────────────────────────────────────────

func (r *PostgresRepository) CreateRole(ctx context.Context, g QuotaRole) error {
	const q = `
		INSERT INTO quota_roles (id, tenant_id, name, monthly_budget_usd, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, q, g.ID, g.TenantID, g.Name, g.MonthlyBudgetUSD, now, now)
	if err != nil {
		return fmt.Errorf("quota role create: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetRole(ctx context.Context, tenantID, id string) (QuotaRole, error) {
	const q = `
		SELECT id, tenant_id, name, monthly_budget_usd, created_at, updated_at
		FROM quota_roles WHERE id = $1 AND tenant_id = $2`

	var g QuotaRole
	err := r.pool.QueryRow(ctx, q, id, tenantID).Scan(
		&g.ID, &g.TenantID, &g.Name, &g.MonthlyBudgetUSD, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return QuotaRole{}, fmt.Errorf("quota role get %s: %w", id, ErrRoleNotFound)
		}
		return QuotaRole{}, fmt.Errorf("quota role get %s: %w", id, err)
	}
	return g, nil
}

func (r *PostgresRepository) ListRoles(ctx context.Context, tenantID string) ([]QuotaRole, error) {
	const q = `
		SELECT id, tenant_id, name, monthly_budget_usd, created_at, updated_at
		FROM quota_roles WHERE tenant_id = $1 ORDER BY name ASC`

	rows, err := r.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("quota role list: %w", err)
	}
	defer rows.Close()

	var groups []QuotaRole
	for rows.Next() {
		var g QuotaRole
		if err := rows.Scan(&g.ID, &g.TenantID, &g.Name, &g.MonthlyBudgetUSD, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("quota role list scan: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quota role list rows: %w", err)
	}
	if groups == nil {
		groups = []QuotaRole{}
	}
	return groups, nil
}

func (r *PostgresRepository) UpdateRoleBudget(ctx context.Context, tenantID, id string, budgetUSD float64) error {
	const q = `
		UPDATE quota_roles SET monthly_budget_usd = $1, updated_at = now()
		WHERE id = $2 AND tenant_id = $3`

	tag, err := r.pool.Exec(ctx, q, budgetUSD, id, tenantID)
	if err != nil {
		return fmt.Errorf("quota role update %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("quota role update %s: %w", id, ErrRoleNotFound)
	}
	return nil
}

func (r *PostgresRepository) DeleteRole(ctx context.Context, tenantID, id string) error {
	const q = `DELETE FROM quota_roles WHERE id = $1 AND tenant_id = $2`

	tag, err := r.pool.Exec(ctx, q, id, tenantID)
	if err != nil {
		return fmt.Errorf("quota role delete %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("quota role delete %s: %w", id, ErrRoleNotFound)
	}
	return nil
}

// ─── Memberships ─────────────────────────────────────────────────────────────

func (r *PostgresRepository) SetMembership(ctx context.Context, tenantID, principalID, roleID string) error {
	return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		// A principal can only hold one quota slot — clear any individual quota.
		if _, err := tx.Exec(ctx,
			`DELETE FROM principal_quotas WHERE tenant_id = $1 AND principal_id = $2`,
			tenantID, principalID,
		); err != nil {
			return fmt.Errorf("quota membership set (clear individual): %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO principal_quota_role_memberships (principal_id, tenant_id, role_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (principal_id, tenant_id) DO UPDATE SET role_id = EXCLUDED.role_id`,
			principalID, tenantID, roleID,
		); err != nil {
			return fmt.Errorf("quota membership set: %w", err)
		}
		return nil
	})
}

func (r *PostgresRepository) RemoveMembership(ctx context.Context, tenantID, principalID, roleID string) error {
	const q = `
		DELETE FROM principal_quota_role_memberships
		WHERE principal_id = $1 AND tenant_id = $2 AND role_id = $3`

	tag, err := r.pool.Exec(ctx, q, principalID, tenantID, roleID)
	if err != nil {
		return fmt.Errorf("quota membership remove: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

func (r *PostgresRepository) ListMembers(ctx context.Context, tenantID, roleID string) ([]string, error) {
	const q = `
		SELECT principal_id FROM principal_quota_role_memberships
		WHERE tenant_id = $1 AND role_id = $2
		ORDER BY principal_id ASC`

	rows, err := r.pool.Query(ctx, q, tenantID, roleID)
	if err != nil {
		return nil, fmt.Errorf("quota membership list: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("quota membership list scan: %w", err)
		}
		members = append(members, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quota membership list rows: %w", err)
	}
	if members == nil {
		members = []string{}
	}
	return members, nil
}

// ─── Individual principal quotas ─────────────────────────────────────────────

func (r *PostgresRepository) SetPrincipalQuota(ctx context.Context, tenantID, principalID string, budgetUSD float64) error {
	return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		// A principal can only hold one quota slot — clear any group membership.
		if _, err := tx.Exec(ctx,
			`DELETE FROM principal_quota_role_memberships WHERE tenant_id = $1 AND principal_id = $2`,
			tenantID, principalID,
		); err != nil {
			return fmt.Errorf("set principal quota (clear membership): %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO principal_quotas (principal_id, tenant_id, monthly_budget_usd)
			VALUES ($1, $2, $3)
			ON CONFLICT (principal_id, tenant_id) DO UPDATE
			SET monthly_budget_usd = EXCLUDED.monthly_budget_usd,
			    updated_at = now()`,
			principalID, tenantID, budgetUSD,
		); err != nil {
			return fmt.Errorf("set principal quota: %w", err)
		}
		return nil
	})
}

func (r *PostgresRepository) GetPrincipalQuota(ctx context.Context, tenantID, principalID string) (float64, bool, error) {
	const q = `
		SELECT monthly_budget_usd FROM principal_quotas
		WHERE tenant_id = $1 AND principal_id = $2`

	var budget float64
	err := r.pool.QueryRow(ctx, q, tenantID, principalID).Scan(&budget)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("get principal quota: %w", err)
	}
	return budget, true, nil
}

func (r *PostgresRepository) ClearPrincipalQuota(ctx context.Context, tenantID, principalID string) error {
	const q = `DELETE FROM principal_quotas WHERE tenant_id = $1 AND principal_id = $2`

	tag, err := r.pool.Exec(ctx, q, tenantID, principalID)
	if err != nil {
		return fmt.Errorf("clear principal quota: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("clear principal quota %s: %w", principalID, ErrPrincipalQuotaNotFound)
	}
	return nil
}

// ─── BundleData ───────────────────────────────────────────────────────────────

// BundleData returns the quota document for the given tenant, structured for
// inclusion in the OPA bundle as data/quotas/data.json.
func (r *PostgresRepository) BundleData(ctx context.Context, tenantID string) (*BundleDocument, error) {
	doc := &BundleDocument{
		PrincipalQuotas: make(map[string]float64),
		Roles:           make(map[string]RoleDoc),
		Memberships:     make(map[string]string),
	}

	// Load all groups for this tenant.
	const groupQ = `SELECT id, name, monthly_budget_usd FROM quota_roles WHERE tenant_id = $1`
	gRows, err := r.pool.Query(ctx, groupQ, tenantID)
	if err != nil {
		return nil, fmt.Errorf("bundle data groups: %w", err)
	}
	defer gRows.Close()
	for gRows.Next() {
		var id, name string
		var budget float64
		if scanErr := gRows.Scan(&id, &name, &budget); scanErr != nil {
			return nil, fmt.Errorf("bundle data groups scan: %w", scanErr)
		}
		doc.Roles[id] = RoleDoc{Name: name, MonthlyBudgetUSD: budget}
	}
	if err = gRows.Err(); err != nil {
		return nil, fmt.Errorf("bundle data groups rows: %w", err)
	}

	// Load all memberships.
	const memQ = `SELECT principal_id, role_id FROM principal_quota_role_memberships WHERE tenant_id = $1`
	mRows, err := r.pool.Query(ctx, memQ, tenantID)
	if err != nil {
		return nil, fmt.Errorf("bundle data memberships: %w", err)
	}
	defer mRows.Close()
	for mRows.Next() {
		var pid, gid string
		if scanErr := mRows.Scan(&pid, &gid); scanErr != nil {
			return nil, fmt.Errorf("bundle data memberships scan: %w", scanErr)
		}
		doc.Memberships[pid] = gid
	}
	if err = mRows.Err(); err != nil {
		return nil, fmt.Errorf("bundle data memberships rows: %w", err)
	}

	// Load all individual principal quotas.
	const pqQ = `SELECT principal_id, monthly_budget_usd FROM principal_quotas WHERE tenant_id = $1`
	pRows, err := r.pool.Query(ctx, pqQ, tenantID)
	if err != nil {
		return nil, fmt.Errorf("bundle data principal quotas: %w", err)
	}
	defer pRows.Close()
	for pRows.Next() {
		var pid string
		var budget float64
		if err := pRows.Scan(&pid, &budget); err != nil {
			return nil, fmt.Errorf("bundle data principal quotas scan: %w", err)
		}
		doc.PrincipalQuotas[pid] = budget
	}
	if err := pRows.Err(); err != nil {
		return nil, fmt.Errorf("bundle data principal quotas rows: %w", err)
	}

	return doc, nil
}
