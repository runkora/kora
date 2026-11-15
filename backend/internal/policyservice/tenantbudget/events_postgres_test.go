package tenantbudget

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// errDiscard rolls back a test transaction so no row reaches a running relay.
var errDiscard = errors.New("discard test rows")

func TestPostgres_TenantBudgetEvents(t *testing.T) {
	repo, tenant := testRepo(t)
	pool := repo.(*postgresRepository).pool
	err := storage.WithTransaction(context.Background(), pool, func(ctx context.Context, _ pgx.Tx) error {
		if err := repo.Upsert(ctx, &TenantBudget{TenantID: tenant, LimitUSD: 10, ResetCadence: "monthly"}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if err := repo.Upsert(ctx, &TenantBudget{TenantID: tenant, LimitUSD: 20, ResetCadence: "monthly"}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		// A resync of the same budget changes nothing and records nothing.
		if err := repo.Upsert(ctx, &TenantBudget{TenantID: tenant, LimitUSD: 20, ResetCadence: "monthly"}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if err := repo.Delete(ctx, tenant); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		rows, err := pool.Query(ctx, `SELECT type FROM event_outbox WHERE tenant_id = $1 ORDER BY position`, tenant)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		want := []string{string(events.TenantBudgetCreated), string(events.TenantBudgetUpdated), string(events.TenantBudgetDeleted)}
		if !slices.Equal(got, want) {
			t.Errorf("events = %v, want %v", got, want)
		}
		return errDiscard
	})
	if !errors.Is(err, errDiscard) {
		t.Fatal(err)
	}
}
