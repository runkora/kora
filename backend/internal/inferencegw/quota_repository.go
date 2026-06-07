package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QuotaRepository defines the storage contract for budget quotas.
// Resolve is the hot path called on every inference request (via QuotaCache).
// The CRUD methods are used by the admin quota API.
type QuotaRepository interface {
	// Resolve returns the effective monthly budget for the principal.
	// Priority: individual principal_quotas entry → quota_groups membership → no limit.
	// Returns (0, false, nil) when neither table has an entry for the principal.
	Resolve(ctx context.Context, tenantID, principalID string) (budgetUSD float64, hasLimit bool, err error)

	CreateGroup(ctx context.Context, g QuotaGroup) error
	GetGroup(ctx context.Context, tenantID, id string) (QuotaGroup, error)
	ListGroups(ctx context.Context, tenantID string) ([]QuotaGroup, error)
	UpdateGroupBudget(ctx context.Context, tenantID, id string, budgetUSD float64) error
	DeleteGroup(ctx context.Context, tenantID, id string) error

	SetMembership(ctx context.Context, tenantID, principalID, groupID string) error
	RemoveMembership(ctx context.Context, tenantID, principalID string) error

	SetPrincipalQuota(ctx context.Context, tenantID, principalID string, budgetUSD float64) error
	GetPrincipalQuota(ctx context.Context, tenantID, principalID string) (float64, bool, error)
	ClearPrincipalQuota(ctx context.Context, tenantID, principalID string) error
}

// ─── PostgresQuotaRepository ──────────────────────────────────────────────────

// PostgresQuotaRepository implements QuotaRepository against PostgreSQL.
type PostgresQuotaRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresQuotaRepository constructs the repository.
func NewPostgresQuotaRepository(pool *pgxpool.Pool) *PostgresQuotaRepository {
	return &PostgresQuotaRepository{pool: pool}
}

// Resolve returns the effective monthly budget for the principal.
// Individual principal_quotas takes priority over group membership.
func (r *PostgresQuotaRepository) Resolve(ctx context.Context, tenantID, principalID string) (float64, bool, error) {
	// 1. Individual override.
	const indivQ = `
		SELECT monthly_budget_usd FROM principal_quotas
		WHERE tenant_id = $1 AND principal_id = $2`

	var budget float64
	err := r.pool.QueryRow(ctx, indivQ, tenantID, principalID).Scan(&budget)
	if err == nil {
		return budget, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("quota resolve individual: %w", err)
	}

	// 2. Group membership fallback.
	const groupQ = `
		SELECT qg.monthly_budget_usd
		FROM principal_quota_group_memberships m
		JOIN quota_groups qg ON qg.id = m.group_id
		WHERE m.tenant_id = $1 AND m.principal_id = $2`

	err = r.pool.QueryRow(ctx, groupQ, tenantID, principalID).Scan(&budget)
	if err == nil {
		return budget, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("quota resolve group: %w", err)
	}

	// 3. No limit configured.
	return 0, false, nil
}

// ─── Group CRUD ───────────────────────────────────────────────────────────────

func (r *PostgresQuotaRepository) CreateGroup(ctx context.Context, g QuotaGroup) error {
	const q = `
		INSERT INTO quota_groups (id, tenant_id, name, monthly_budget_usd, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, q, g.ID, g.TenantID, g.Name, g.MonthlyBudgetUSD, now, now)
	if err != nil {
		return fmt.Errorf("quota group create: %w", err)
	}
	return nil
}

func (r *PostgresQuotaRepository) GetGroup(ctx context.Context, tenantID, id string) (QuotaGroup, error) {
	const q = `
		SELECT id, tenant_id, name, monthly_budget_usd, created_at, updated_at
		FROM quota_groups WHERE id = $1 AND tenant_id = $2`

	var g QuotaGroup
	err := r.pool.QueryRow(ctx, q, id, tenantID).Scan(
		&g.ID, &g.TenantID, &g.Name, &g.MonthlyBudgetUSD, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return QuotaGroup{}, fmt.Errorf("quota group get %s: %w", id, ErrQuotaGroupNotFound)
		}
		return QuotaGroup{}, fmt.Errorf("quota group get %s: %w", id, err)
	}
	return g, nil
}

func (r *PostgresQuotaRepository) ListGroups(ctx context.Context, tenantID string) ([]QuotaGroup, error) {
	const q = `
		SELECT id, tenant_id, name, monthly_budget_usd, created_at, updated_at
		FROM quota_groups WHERE tenant_id = $1 ORDER BY name ASC`

	rows, err := r.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("quota group list: %w", err)
	}
	defer rows.Close()

	var groups []QuotaGroup
	for rows.Next() {
		var g QuotaGroup
		if err := rows.Scan(&g.ID, &g.TenantID, &g.Name, &g.MonthlyBudgetUSD, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("quota group list scan: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quota group list rows: %w", err)
	}
	if groups == nil {
		groups = []QuotaGroup{}
	}
	return groups, nil
}

func (r *PostgresQuotaRepository) UpdateGroupBudget(ctx context.Context, tenantID, id string, budgetUSD float64) error {
	const q = `
		UPDATE quota_groups SET monthly_budget_usd = $1, updated_at = now()
		WHERE id = $2 AND tenant_id = $3`

	tag, err := r.pool.Exec(ctx, q, budgetUSD, id, tenantID)
	if err != nil {
		return fmt.Errorf("quota group update %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("quota group update %s: %w", id, ErrQuotaGroupNotFound)
	}
	return nil
}

func (r *PostgresQuotaRepository) DeleteGroup(ctx context.Context, tenantID, id string) error {
	const q = `DELETE FROM quota_groups WHERE id = $1 AND tenant_id = $2`

	tag, err := r.pool.Exec(ctx, q, id, tenantID)
	if err != nil {
		return fmt.Errorf("quota group delete %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("quota group delete %s: %w", id, ErrQuotaGroupNotFound)
	}
	return nil
}

// ─── Membership ───────────────────────────────────────────────────────────────

// SetMembership assigns or re-assigns a principal to a quota group.
// A principal can belong to at most one group per tenant (PK constraint).
func (r *PostgresQuotaRepository) SetMembership(ctx context.Context, tenantID, principalID, groupID string) error {
	const q = `
		INSERT INTO principal_quota_group_memberships (principal_id, tenant_id, group_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (principal_id, tenant_id) DO UPDATE SET group_id = EXCLUDED.group_id`

	_, err := r.pool.Exec(ctx, q, principalID, tenantID, groupID)
	if err != nil {
		return fmt.Errorf("quota membership set: %w", err)
	}
	return nil
}

func (r *PostgresQuotaRepository) RemoveMembership(ctx context.Context, tenantID, principalID string) error {
	const q = `
		DELETE FROM principal_quota_group_memberships
		WHERE principal_id = $1 AND tenant_id = $2`

	_, err := r.pool.Exec(ctx, q, principalID, tenantID)
	if err != nil {
		return fmt.Errorf("quota membership remove: %w", err)
	}
	return nil
}

// ─── Individual principal quotas ─────────────────────────────────────────────

func (r *PostgresQuotaRepository) SetPrincipalQuota(ctx context.Context, tenantID, principalID string, budgetUSD float64) error {
	const q = `
		INSERT INTO principal_quotas (principal_id, tenant_id, monthly_budget_usd)
		VALUES ($1, $2, $3)
		ON CONFLICT (principal_id, tenant_id) DO UPDATE
		SET monthly_budget_usd = EXCLUDED.monthly_budget_usd,
		    updated_at = now()`

	_, err := r.pool.Exec(ctx, q, principalID, tenantID, budgetUSD)
	if err != nil {
		return fmt.Errorf("set principal quota: %w", err)
	}
	return nil
}

func (r *PostgresQuotaRepository) GetPrincipalQuota(ctx context.Context, tenantID, principalID string) (float64, bool, error) {
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

func (r *PostgresQuotaRepository) ClearPrincipalQuota(ctx context.Context, tenantID, principalID string) error {
	const q = `
		DELETE FROM principal_quotas
		WHERE tenant_id = $1 AND principal_id = $2`

	tag, err := r.pool.Exec(ctx, q, tenantID, principalID)
	if err != nil {
		return fmt.Errorf("clear principal quota: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("clear principal quota %s: %w", principalID, ErrPrincipalQuotaNotFound)
	}
	return nil
}
