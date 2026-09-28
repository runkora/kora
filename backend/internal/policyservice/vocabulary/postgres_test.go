package vocabulary

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the vocabulary Repository. They run only when
// ORBIT_TEST_DB_URL points at a database with policy_scope_vocabulary
// migrated; otherwise they skip.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/policyservice/vocabulary/ -run Postgres
func testVocabRepo(t *testing.T) (Repository, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run vocabulary Postgres integration tests")
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
	service := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM policy_scope_vocabulary WHERE registered_by = $1`, service)
		pool.Close()
	})
	return NewPostgresRepository(pool), service
}

// TestPostgres_UpsertReportsChange covers the signal that decides whether a
// route registration recompiles every tenant's bundle. Services re-register on
// a timer, so an unchanged catalog has to report false; the conflict clause
// carries the comparison, which is why this is tested against the database.
func TestPostgres_UpsertReportsChange(t *testing.T) {
	repo, service := testVocabRepo(t)
	ctx := context.Background()

	entry := func() *ScopeEntry {
		return &ScopeEntry{
			Scope:        "test:" + service + ":read",
			ResourceType: "test",
			PathSegments: []string{service},
			Action:       "read",
			Description:  "first description",
			RegisteredBy: service,
		}
	}

	changed, err := repo.Upsert(ctx, entry())
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !changed {
		t.Error("insert reported no change")
	}

	changed, err = repo.Upsert(ctx, entry())
	if err != nil {
		t.Fatalf("idempotent upsert: %v", err)
	}
	if changed {
		t.Error("idempotent upsert reported a change")
	}

	updated := entry()
	updated.Description = "second description"
	changed, err = repo.Upsert(ctx, updated)
	if err != nil {
		t.Fatalf("changed upsert: %v", err)
	}
	if !changed {
		t.Error("changed description reported no change")
	}

	withRequires := entry()
	withRequires.Description = "second description"
	withRequires.Requires = []string{"platform:grants:read"}
	changed, err = repo.Upsert(ctx, withRequires)
	if err != nil {
		t.Fatalf("requires upsert: %v", err)
	}
	if !changed {
		t.Error("added requires reported no change")
	}

	changed, err = repo.Upsert(ctx, withRequires)
	if err != nil {
		t.Fatalf("idempotent requires upsert: %v", err)
	}
	if changed {
		t.Error("idempotent upsert with requires reported a change")
	}
}
