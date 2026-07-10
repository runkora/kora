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
