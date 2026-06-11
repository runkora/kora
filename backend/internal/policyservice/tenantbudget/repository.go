// Package tenantbudget provides the Postgres-backed tenant budget cap store
// for the Policy Service. A tenant budget is a single rollup spending ceiling:
// all principals' spend accumulates against it, and AGENT/SERVICE principals
// are blocked at the limit (humans are not).
// See docs/adr/2026-06-10-hierarchical-budget-enforcement.md.
package tenantbudget

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantBudget is the rollup spending cap for a tenant.
type TenantBudget struct {
	TenantID     string    `json:"tenant_id"`
	LimitUSD     float64   `json:"limit_usd"`
	ResetCadence string    `json:"reset_cadence"` // "weekly" or "monthly"
	UpdatedAt    time.Time `json:"updated_at"`
}

// ErrTenantBudgetNotFound is returned when a tenant has no budget configured.
var ErrTenantBudgetNotFound = errors.New("tenant budget not found")

// Repository defines the tenant budget storage contract.
type Repository interface {
	// Upsert creates or replaces the budget cap for a tenant.
	Upsert(ctx context.Context, tb *TenantBudget) error

	// Get retrieves the budget cap for a tenant.
	// Returns ErrTenantBudgetNotFound when none is configured.
	Get(ctx context.Context, tenantID string) (*TenantBudget, error)

	// Delete removes the budget cap for a tenant.
	// Returns ErrTenantBudgetNotFound when none is configured.
	Delete(ctx context.Context, tenantID string) error
}

// postgresRepository is the Postgres-backed implementation.
type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a Repository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) Upsert(ctx context.Context, tb *TenantBudget) error {
	tb.UpdatedAt = time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO policy_tenant_budgets (tenant_id, limit_usd, reset_cadence, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id) DO UPDATE
		SET limit_usd = EXCLUDED.limit_usd,
		    reset_cadence = EXCLUDED.reset_cadence,
		    updated_at = EXCLUDED.updated_at
	`, tb.TenantID, tb.LimitUSD, tb.ResetCadence, tb.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert tenant budget: %w", err)
	}
	return nil
}

func (r *postgresRepository) Get(ctx context.Context, tenantID string) (*TenantBudget, error) {
	var tb TenantBudget
	err := r.pool.QueryRow(ctx, `
		SELECT tenant_id, limit_usd, reset_cadence, updated_at
		FROM policy_tenant_budgets WHERE tenant_id = $1
	`, tenantID).Scan(&tb.TenantID, &tb.LimitUSD, &tb.ResetCadence, &tb.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTenantBudgetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant budget: %w", err)
	}
	return &tb, nil
}

func (r *postgresRepository) Delete(ctx context.Context, tenantID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM policy_tenant_budgets WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return fmt.Errorf("delete tenant budget: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTenantBudgetNotFound
	}
	return nil
}
