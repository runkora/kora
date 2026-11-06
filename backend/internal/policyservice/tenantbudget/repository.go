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

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// TenantBudget is the rollup spending cap for a tenant.
type TenantBudget struct {
	TenantID     string    `json:"tenant_id"`
	LimitUSD     float64   `json:"limit_usd"`
	ResetCadence string    `json:"reset_cadence"` // "weekly" or "monthly"
	UpdatedAt    time.Time `json:"updated_at"`
	// Source is "crd" when the budget comes from the KoraTenant, else "api".
	Source string `json:"source"`
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

	// UpsertDeclared writes the tenant's crd budget, replacing an api budget.
	UpsertDeclared(ctx context.Context, tb *TenantBudget) error

	// ListDeclared returns every crd budget.
	ListDeclared(ctx context.Context) ([]*TenantBudget, error)

	// DeleteDeclared removes the tenant's crd budget and leaves an api budget alone.
	DeleteDeclared(ctx context.Context, tenantID string) error
}

// postgresRepository is the Postgres-backed implementation.
type postgresRepository struct {
	pool *storage.Pool
}

// NewPostgresRepository creates a Repository backed by PostgreSQL.
func NewPostgresRepository(pool *storage.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) Upsert(ctx context.Context, tb *TenantBudget) error {
	tb.UpdatedAt = time.Now().UTC()
	tb.Source = declarative.SourceAPI
	return r.write(ctx, tb, `
		INSERT INTO policy_tenant_budgets (tenant_id, limit_usd, reset_cadence, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id) DO UPDATE
		SET limit_usd = EXCLUDED.limit_usd,
		    reset_cadence = EXCLUDED.reset_cadence,
		    updated_at = EXCLUDED.updated_at
		WHERE policy_tenant_budgets.source = 'api'
	`)
}

func (r *postgresRepository) UpsertDeclared(ctx context.Context, tb *TenantBudget) error {
	tb.UpdatedAt = time.Now().UTC()
	tb.Source = declarative.SourceCRD
	return r.write(ctx, tb, `
		INSERT INTO policy_tenant_budgets (tenant_id, limit_usd, reset_cadence, updated_at, source)
		VALUES ($1, $2, $3, $4, 'crd')
		ON CONFLICT (tenant_id) DO UPDATE
		SET limit_usd = EXCLUDED.limit_usd,
		    reset_cadence = EXCLUDED.reset_cadence,
		    updated_at = EXCLUDED.updated_at,
		    source = 'crd'
	`)
}

// write runs an upsert of tb and records it as created or updated. An upsert
// that writes no row means a resource manages the budget.
func (r *postgresRepository) write(ctx context.Context, tb *TenantBudget, upsert string) error {
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		// FOR UPDATE locks no row on a first write. The lock makes one of two concurrent first writes the creator.
		if _, err := r.pool.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('policy_tenant_budgets/' || $1))`, tb.TenantID); err != nil {
			return fmt.Errorf("lock tenant budget: %w", err)
		}
		before, err := scanBudget(r.pool.QueryRow(ctx,
			`SELECT `+budgetColumns+` FROM policy_tenant_budgets WHERE tenant_id = $1 FOR UPDATE`, tb.TenantID))
		if err != nil && !errors.Is(err, ErrTenantBudgetNotFound) {
			return err
		}
		tag, err := r.pool.Exec(ctx, upsert, tb.TenantID, tb.LimitUSD, tb.ResetCadence, tb.UpdatedAt)
		if err != nil {
			return fmt.Errorf("upsert tenant budget: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return declarative.ErrManagedByResource
		}
		if before == nil {
			return publish(ctx, r.pool, events.TenantBudgetCreated, tb, nil)
		}
		changed := events.Changed(newEntity(before), newEntity(tb))
		if len(changed) == 0 {
			return nil
		}
		return publish(ctx, r.pool, events.TenantBudgetUpdated, tb, changed)
	})
}

func (r *postgresRepository) ListDeclared(ctx context.Context) ([]*TenantBudget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+budgetColumns+`
		FROM policy_tenant_budgets WHERE source = 'crd'
	`)
	if err != nil {
		return nil, fmt.Errorf("list declared tenant budgets: %w", err)
	}
	budgets, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByPos[TenantBudget])
	if err != nil {
		return nil, fmt.Errorf("list declared tenant budgets: %w", err)
	}
	return budgets, nil
}

func (r *postgresRepository) DeleteDeclared(ctx context.Context, tenantID string) error {
	err := r.delete(ctx, `DELETE FROM policy_tenant_budgets WHERE tenant_id = $1 AND source = 'crd' RETURNING `+budgetColumns, tenantID)
	if err != nil && !errors.Is(err, ErrTenantBudgetNotFound) {
		return fmt.Errorf("delete declared tenant budget: %w", err)
	}
	return nil
}

func (r *postgresRepository) Get(ctx context.Context, tenantID string) (*TenantBudget, error) {
	return scanBudget(r.pool.QueryRow(ctx, `SELECT `+budgetColumns+` FROM policy_tenant_budgets WHERE tenant_id = $1`, tenantID))
}

func (r *postgresRepository) Delete(ctx context.Context, tenantID string) error {
	return r.delete(ctx, `DELETE FROM policy_tenant_budgets WHERE tenant_id = $1 AND source = 'api' RETURNING `+budgetColumns, tenantID)
}

// delete runs a DELETE that returns budgetColumns and records the deletion.
func (r *postgresRepository) delete(ctx context.Context, sql, tenantID string) error {
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		deleted, err := scanBudget(r.pool.QueryRow(ctx, sql, tenantID))
		if err != nil {
			return err
		}
		return publish(ctx, r.pool, events.TenantBudgetDeleted, deleted, nil)
	})
}

const budgetColumns = `tenant_id, limit_usd, reset_cadence, updated_at, source`

func scanBudget(row pgx.Row) (*TenantBudget, error) {
	var tb TenantBudget
	err := row.Scan(&tb.TenantID, &tb.LimitUSD, &tb.ResetCadence, &tb.UpdatedAt, &tb.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTenantBudgetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant budget: %w", err)
	}
	return &tb, nil
}

// entity is the tenant budget an event carries.
type entity struct {
	LimitUSD     float64 `json:"limit_usd"`
	ResetCadence string  `json:"reset_cadence"`
	Source       string  `json:"source"`
}

func newEntity(tb *TenantBudget) entity {
	return entity{LimitUSD: tb.LimitUSD, ResetCadence: tb.ResetCadence, Source: tb.Source}
}

func publish(ctx context.Context, pool *storage.Pool, typ events.Type, tb *TenantBudget, changed []string) error {
	return events.Publish(ctx, pool, events.Event{
		Type: typ, Tenant: tb.TenantID, Subject: "tenant_budgets/" + tb.TenantID, Entity: newEntity(tb), Changed: changed,
	})
}
