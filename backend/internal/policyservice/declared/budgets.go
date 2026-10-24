package declared

import (
	"context"
	"fmt"
	"time"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/policyservice/api"
	"github.com/runkora/kora/backend/internal/policyservice/tenantbudget"
)

// BudgetSyncInterval is how often the registry's tenant budgets are compared with the projected rows.
const BudgetSyncInterval = 30 * time.Second

// budgetService writes the tenant budgets projected from KoraTenant.
type budgetService interface {
	ListDeclaredTenantBudgets(ctx context.Context) ([]*tenantbudget.TenantBudget, error)
	SetDeclaredTenantBudget(ctx context.Context, tenantID string, limitUSD float64, cadence string) error
	RemoveDeclaredTenantBudget(ctx context.Context, tenantID string) error
}

// budgetRegistry is the part of the tenant registry budget projection reads.
type budgetRegistry interface {
	TenantIDs() []string
	TenantBudget(tenantID string) (declarative.Budget, bool)
	Err() error
}

// RunTenantBudgets syncs the declared tenant budgets every interval until ctx
// ends. Each sync is idempotent, so every replica may run it.
func RunTenantBudgets(ctx context.Context, svc budgetService, registry budgetRegistry, interval time.Duration, logger logging.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := syncBudgets(ctx, svc, registry, logger); err != nil {
			logger.Error(ctx, "declared tenant budget sync failed", logging.Field{Key: "error", Value: err.Error()})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// syncBudgets makes the crd tenant budgets match the registry. A budget the
// registry declares replaces an API budget. A crd budget the registry no
// longer declares is deleted.
func syncBudgets(ctx context.Context, svc budgetService, registry budgetRegistry, logger logging.Logger) error {
	if err := registry.Err(); err != nil {
		return fmt.Errorf("read tenant registry: %w", err)
	}
	tenantIDs := registry.TenantIDs()
	// An empty or missing registry is more likely a lost mount than every tenant gone.
	if len(tenantIDs) == 0 {
		return nil
	}
	rows, err := svc.ListDeclaredTenantBudgets(ctx)
	if err != nil {
		return err
	}
	stale := make(map[string]*tenantbudget.TenantBudget, len(rows))
	for _, row := range rows {
		stale[row.TenantID] = row
	}
	for _, tenantID := range tenantIDs {
		b, declared := registry.TenantBudget(tenantID)
		if !declared {
			continue
		}
		row := stale[tenantID]
		delete(stale, tenantID)
		cadence, ok := api.ValidTenantBudget(b.LimitUSD, b.ResetCadence)
		if !ok {
			logger.Warn(ctx, "tenant budget in the registry is invalid", logging.Field{Key: "tenant_id", Value: tenantID})
			continue
		}
		if row != nil && row.LimitUSD == b.LimitUSD && row.ResetCadence == cadence {
			continue
		}
		if err := svc.SetDeclaredTenantBudget(ctx, tenantID, b.LimitUSD, cadence); err != nil {
			return err
		}
	}
	for tenantID := range stale {
		if err := svc.RemoveDeclaredTenantBudget(ctx, tenantID); err != nil {
			return err
		}
	}
	return nil
}
