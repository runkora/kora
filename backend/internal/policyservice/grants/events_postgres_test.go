package grants

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

func TestPostgres_GrantEvents(t *testing.T) {
	repo, tenant := testGrantsRepo(t)
	pool := repo.(*postgresRepository).pool
	err := storage.WithTransaction(context.Background(), pool, func(ctx context.Context, _ pgx.Tx) error {
		g := &Grant{TenantID: tenant, PrincipalType: PrincipalTypeHuman, PrincipalID: "user-1", Scopes: []string{"agents:read"}}
		if err := repo.Create(ctx, g); err != nil {
			t.Fatalf("Create: %v", err)
		}
		budget := &BudgetAssignment{LimitUSD: 5, ResetCadence: "monthly"}
		if err := repo.UpdateBudget(ctx, g.ID, tenant, budget); err != nil {
			t.Fatalf("UpdateBudget: %v", err)
		}
		// Writing the same budget again changes nothing and records nothing.
		if err := repo.UpdateBudget(ctx, g.ID, tenant, budget); err != nil {
			t.Fatalf("UpdateBudget: %v", err)
		}
		if err := repo.Delete(ctx, g.ID, tenant); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		rows, err := pool.Query(ctx, `SELECT type, coalesce(envelope->'data'->>'changed', '') FROM event_outbox WHERE tenant_id = $1 ORDER BY position`, tenant)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ Type, Changed string }])
		if err != nil {
			t.Fatal(err)
		}
		want := []struct{ Type, Changed string }{
			{string(events.GrantCreated), ""}, {string(events.GrantUpdated), `["budget"]`}, {string(events.GrantDeleted), ""},
		}
		if !slices.Equal(got, want) {
			t.Errorf("events = %v, want %v", got, want)
		}
		return errDiscard
	})
	if !errors.Is(err, errDiscard) {
		t.Fatal(err)
	}
}
