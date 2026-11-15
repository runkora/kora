package roles

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

func TestPostgres_RoleEvents(t *testing.T) {
	repo, tenant := testRolesRepo(t)
	pool := repo.(*postgresRepository).pool
	err := storage.WithTransaction(context.Background(), pool, func(ctx context.Context, _ pgx.Tx) error {
		if err := repo.Upsert(ctx, &RoleEntry{TenantID: tenant, Role: "eng", Scopes: []string{"agents:read"}}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if err := repo.Upsert(ctx, &RoleEntry{TenantID: tenant, Role: "eng", Scopes: []string{"agents:read", "runs:read"}}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if err := repo.Delete(ctx, tenant, "eng"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		declared := &RoleEntry{TenantID: tenant, Role: "sre", Scopes: []string{"agents:read"}, ResourceName: "oncall", ResourceUID: "uid-1"}
		if err := repo.UpsertDeclared(ctx, declared); err != nil {
			t.Fatalf("UpsertDeclared: %v", err)
		}
		// Renaming the role the resource declares deletes the old row and creates the new one.
		renamed := &RoleEntry{TenantID: tenant, Role: "ops", Scopes: []string{"agents:read"}, ResourceName: "oncall", ResourceUID: "uid-1"}
		if err := repo.UpsertDeclared(ctx, renamed); err != nil {
			t.Fatalf("UpsertDeclared: %v", err)
		}
		rows, err := pool.Query(ctx, `SELECT type, envelope->>'subject' FROM event_outbox WHERE tenant_id = $1 ORDER BY position`, tenant)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ Type, Subject string }])
		if err != nil {
			t.Fatal(err)
		}
		want := []struct{ Type, Subject string }{
			{string(events.RoleCreated), "roles/eng"}, {string(events.RoleUpdated), "roles/eng"}, {string(events.RoleDeleted), "roles/eng"},
			{string(events.RoleCreated), "roles/sre"}, {string(events.RoleDeleted), "roles/sre"}, {string(events.RoleCreated), "roles/ops"},
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
