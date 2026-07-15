package devenvironments

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the devenvironments repository. They run
// only when ORBIT_TEST_DB_URL points at a database with the dev_environments
// table migrated (e.g. the local docker-compose Postgres after
// `make migrate-up`); otherwise they skip. Each test isolates itself under a
// random owner and cleans up.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/devenvironments/ -run Postgres
func testRepo(t *testing.T) (*PostgresRepository, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run devenvironments Postgres integration tests")
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
	owner := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM dev_environments WHERE owner=$1`, owner)
		pool.Close()
	})
	return NewPostgresRepository(pool), owner
}

func TestPostgres_CreateAndGet(t *testing.T) {
	r, owner := testRepo(t)
	ctx := context.Background()

	rec := &Record{ID: "env-" + uuid.NewString()[:8], Owner: owner, Ref: "main", CreatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := r.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := r.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Owner != owner || got.Ref != "main" || got.DeletedAt != nil {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	if _, err := r.Get(ctx, "does-not-exist"); err != ErrNotFound {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestPostgres_ListByOwnerAndListAll(t *testing.T) {
	r, owner := testRepo(t)
	ctx := context.Background()

	other := "test-" + uuid.NewString()
	t.Cleanup(func() {
		ctx := context.Background()
		pool := r.pool
		_, _ = pool.Exec(ctx, `DELETE FROM dev_environments WHERE owner=$1`, other)
	})

	rec1 := &Record{ID: "env-" + uuid.NewString()[:8], Owner: owner, CreatedAt: time.Now().UTC()}
	rec2 := &Record{ID: "env-" + uuid.NewString()[:8], Owner: other, CreatedAt: time.Now().UTC()}
	if err := r.Create(ctx, rec1); err != nil {
		t.Fatalf("Create rec1: %v", err)
	}
	if err := r.Create(ctx, rec2); err != nil {
		t.Fatalf("Create rec2: %v", err)
	}

	own, err := r.ListByOwner(ctx, owner)
	if err != nil || len(own) != 1 || own[0].ID != rec1.ID {
		t.Fatalf("ListByOwner = %+v, err=%v", own, err)
	}

	all, err := r.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	found := map[string]bool{}
	for _, rec := range all {
		found[rec.ID] = true
	}
	if !found[rec1.ID] || !found[rec2.ID] {
		t.Errorf("ListAll missing seeded rows: %+v", all)
	}
}

func TestPostgres_MarkDeletedAndCountActiveByOwner(t *testing.T) {
	r, owner := testRepo(t)
	ctx := context.Background()

	rec := &Record{ID: "env-" + uuid.NewString()[:8], Owner: owner, CreatedAt: time.Now().UTC()}
	if err := r.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	count, err := r.CountActiveByOwner(ctx, owner)
	if err != nil || count != 1 {
		t.Fatalf("CountActiveByOwner = %d, err=%v, want 1", count, err)
	}

	if err := r.MarkDeleted(ctx, rec.ID); err != nil {
		t.Fatalf("MarkDeleted: %v", err)
	}

	count, err = r.CountActiveByOwner(ctx, owner)
	if err != nil || count != 0 {
		t.Fatalf("CountActiveByOwner after delete = %d, err=%v, want 0", count, err)
	}

	list, err := r.ListByOwner(ctx, owner)
	if err != nil || len(list) != 0 {
		t.Fatalf("ListByOwner after delete = %+v, err=%v, want empty", list, err)
	}

	if err := r.MarkDeleted(ctx, rec.ID); err != ErrNotFound {
		t.Errorf("MarkDeleted twice = %v, want ErrNotFound", err)
	}
}

func TestPostgres_InsertAndListAuditEvents(t *testing.T) {
	r, owner := testRepo(t)
	ctx := context.Background()

	rec := &Record{ID: "env-" + uuid.NewString()[:8], Owner: owner, CreatedAt: time.Now().UTC()}
	if err := r.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	occurredAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := r.InsertAuditEvent(ctx, AuditEvent{
		EnvironmentID: rec.ID, Action: "created", Actor: owner, ActorType: "user",
		OccurredAt: occurredAt, Details: map[string]any{"ref": "main"},
	}); err != nil {
		t.Fatalf("InsertAuditEvent: %v", err)
	}

	events, err := r.ListAuditEvents(ctx, rec.ID)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListAuditEvents = %d events, want 1", len(events))
	}
	got := events[0]
	if got.Action != "created" || got.Actor != owner || got.ActorType != "user" {
		t.Errorf("event = %+v, want action=created actor=%s actorType=user", got, owner)
	}
	if !got.OccurredAt.Equal(occurredAt) {
		t.Errorf("occurredAt = %v, want %v", got.OccurredAt, occurredAt)
	}
	if got.Details["ref"] != "main" {
		t.Errorf("details = %v, want ref=main", got.Details)
	}
}

func TestPostgres_InsertAuditEvent_IdempotentDoubleInsert(t *testing.T) {
	r, owner := testRepo(t)
	ctx := context.Background()

	rec := &Record{ID: "env-" + uuid.NewString()[:8], Owner: owner, CreatedAt: time.Now().UTC()}
	if err := r.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	occurredAt := time.Now().UTC().Truncate(time.Millisecond)
	event := AuditEvent{EnvironmentID: rec.ID, Action: "destroyed", Actor: owner, ActorType: "user", OccurredAt: occurredAt}

	if err := r.InsertAuditEvent(ctx, event); err != nil {
		t.Fatalf("first InsertAuditEvent: %v", err)
	}
	if err := r.InsertAuditEvent(ctx, event); err != nil {
		t.Fatalf("second InsertAuditEvent (identical key): %v", err)
	}

	events, err := r.ListAuditEvents(ctx, rec.ID)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListAuditEvents after duplicate insert = %d events, want exactly 1 (ON CONFLICT DO NOTHING)", len(events))
	}
}

func TestPostgres_HasTerminalAuditEvent(t *testing.T) {
	r, owner := testRepo(t)
	ctx := context.Background()

	rec := &Record{ID: "env-" + uuid.NewString()[:8], Owner: owner, CreatedAt: time.Now().UTC()}
	if err := r.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	terminal, err := r.HasTerminalAuditEvent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("HasTerminalAuditEvent before any event: %v", err)
	}
	if terminal {
		t.Error("HasTerminalAuditEvent = true before any audit event was recorded")
	}

	if err := r.InsertAuditEvent(ctx, AuditEvent{
		EnvironmentID: rec.ID, Action: "provision_ready", Actor: "system:operator", ActorType: "system",
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("InsertAuditEvent (provision_ready): %v", err)
	}
	terminal, err = r.HasTerminalAuditEvent(ctx, rec.ID)
	if err != nil || terminal {
		t.Errorf("HasTerminalAuditEvent after a non-terminal event = %v, err=%v, want false", terminal, err)
	}

	if err := r.InsertAuditEvent(ctx, AuditEvent{
		EnvironmentID: rec.ID, Action: "force_destroyed", Actor: "admin@example.com", ActorType: "user",
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("InsertAuditEvent (force_destroyed): %v", err)
	}
	terminal, err = r.HasTerminalAuditEvent(ctx, rec.ID)
	if err != nil || !terminal {
		t.Errorf("HasTerminalAuditEvent after force_destroyed = %v, err=%v, want true", terminal, err)
	}
}
