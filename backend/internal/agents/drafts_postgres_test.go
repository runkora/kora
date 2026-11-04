package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the draft repository. They run only when
// KORA_TEST_DB_URL points at a database with the kora-agents migrations
// applied (e.g. the local docker-compose Postgres after `make migrate-up`);
// otherwise they skip. Each test isolates itself under a random tenant id and
// cleans up. Mirrors internal/triggers/postgres_test.go.
//
//	KORA_TEST_DB_URL='postgres://kora:kora@localhost:5432/kora?sslmode=disable' \
//	  go test ./internal/agents/ -run Postgres_Draft
func testDraftRepo(t *testing.T) (*PostgresRepository, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run draft Postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	tenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM agent_type_drafts WHERE tenant_id=$1`, tenant)
		pool.Close()
	})
	return NewPostgresRepository(pool), tenant
}

func TestPostgres_DraftCRUD(t *testing.T) {
	r, tenant := testDraftRepo(t)
	ctx := context.Background()

	d := AgentTypeDraft{
		ID:        uuid.NewString(),
		TenantID:  tenant,
		CreatedBy: "user-1",
		Payload:   json.RawMessage(`{"name":"My Bot","slug":"my-bot"}`),
	}
	if err := r.CreateDraft(ctx, d); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}

	got, err := r.GetDraft(ctx, d.ID)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if got.TenantID != tenant || got.CreatedBy != "user-1" {
		t.Errorf("owner round-trip mismatch: %+v", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil || payload["name"] != "My Bot" {
		t.Errorf("payload round-trip failed: %s (%v)", got.Payload, err)
	}

	// A second draft may carry the same payload (and therefore the same slug):
	// drafts have no uniqueness constraints — that is the point of the table.
	dup := AgentTypeDraft{ID: uuid.NewString(), TenantID: tenant, CreatedBy: "user-1", Payload: d.Payload}
	if err := r.CreateDraft(ctx, dup); err != nil {
		t.Fatalf("CreateDraft(duplicate slug payload): %v", err)
	}

	list, err := r.ListDrafts(ctx, tenant, "user-1")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListDrafts: %v (n=%d, want 2)", err, len(list))
	}
	// Listing filters by owner: another user in the tenant sees nothing.
	if other, _ := r.ListDrafts(ctx, tenant, "user-2"); len(other) != 0 {
		t.Errorf("ListDrafts other user = %d, want 0", len(other))
	}

	if err := r.UpdateDraft(ctx, d.ID, json.RawMessage(`{"name":"Renamed"}`)); err != nil {
		t.Fatalf("UpdateDraft: %v", err)
	}
	got, _ = r.GetDraft(ctx, d.ID)
	if err := json.Unmarshal(got.Payload, &payload); err != nil || payload["name"] != "Renamed" {
		t.Errorf("update not applied: %s", got.Payload)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Errorf("updated_at not bumped: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}

	if err := r.UpdateDraft(ctx, uuid.NewString(), json.RawMessage(`{}`)); !errors.Is(err, ErrDraftNotFound) {
		t.Errorf("UpdateDraft(missing) = %v, want ErrDraftNotFound", err)
	}

	if err := r.DeleteDraft(ctx, d.ID); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	if _, err := r.GetDraft(ctx, d.ID); !errors.Is(err, ErrDraftNotFound) {
		t.Errorf("after delete GetDraft = %v, want ErrDraftNotFound", err)
	}
	if err := r.DeleteDraft(ctx, d.ID); !errors.Is(err, ErrDraftNotFound) {
		t.Errorf("double DeleteDraft = %v, want ErrDraftNotFound", err)
	}
}

func TestPostgres_DraftReapCutoff(t *testing.T) {
	r, tenant := testDraftRepo(t)
	ctx := context.Background()

	stale := AgentTypeDraft{ID: uuid.NewString(), TenantID: tenant, CreatedBy: "user-1"}
	fresh := AgentTypeDraft{ID: uuid.NewString(), TenantID: tenant, CreatedBy: "user-1"}
	for _, d := range []AgentTypeDraft{stale, fresh} {
		if err := r.CreateDraft(ctx, d); err != nil {
			t.Fatalf("CreateDraft: %v", err)
		}
	}
	// Age the stale draft past the cutoff.
	if _, err := r.pool.Exec(ctx, `UPDATE agent_type_drafts SET updated_at = now() - interval '31 days' WHERE id=$1`, stale.ID); err != nil {
		t.Fatalf("age draft: %v", err)
	}

	deleted, err := r.DeleteDraftsOlderThan(ctx, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteDraftsOlderThan: %v", err)
	}
	if deleted < 1 {
		t.Errorf("deleted = %d, want >= 1", deleted)
	}
	if _, err := r.GetDraft(ctx, stale.ID); !errors.Is(err, ErrDraftNotFound) {
		t.Errorf("stale draft should be reaped, got %v", err)
	}
	if _, err := r.GetDraft(ctx, fresh.ID); err != nil {
		t.Errorf("fresh draft must survive the sweep: %v", err)
	}
}
