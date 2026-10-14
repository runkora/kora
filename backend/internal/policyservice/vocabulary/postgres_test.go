package vocabulary

import (
	"context"
	"os"
	"strings"
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

// TestPostgres_TenantScoping covers the tenant dimension of the vocabulary:
// two tenants can hold the same scope without overwriting each other, readers
// see the tenant's entries plus global ones, a tenant entry shadows a global
// one, and a global prune never touches tenant entries. These live in SQL
// (the primary key, IN ($tenant, ”), DISTINCT ON), so they run against the
// database.
func TestPostgres_TenantScoping(t *testing.T) {
	repo, service := testVocabRepo(t)
	ctx := context.Background()

	shared := "test:" + service + ":shared"
	globalOnly := "test:" + service + ":global"

	put := func(tenant, scope, desc string) {
		t.Helper()
		_, err := repo.Upsert(ctx, &ScopeEntry{
			Scope: scope, ResourceType: "test", PathSegments: []string{service},
			Action: "read", Description: desc, RegisteredBy: service, TenantID: tenant,
		})
		if err != nil {
			t.Fatalf("upsert %q for tenant %q: %v", scope, tenant, err)
		}
	}
	put("acme", shared, "acme shared")
	put("globex", shared, "globex shared")
	put("", shared, "global shared")
	put("", globalOnly, "global only")

	// Two tenants with the same scope do not overwrite each other.
	for tenant, want := range map[string]string{"acme": "acme shared", "globex": "globex shared"} {
		got, err := repo.GetByScope(ctx, tenant, shared)
		if err != nil {
			t.Fatalf("GetByScope %q: %v", tenant, err)
		}
		if got.Description != want || got.TenantID != tenant {
			t.Errorf("GetByScope(%q) = %q (tenant %q), want %q (the tenant's own entry shadows global)",
				tenant, got.Description, got.TenantID, want)
		}
	}

	// A tenant with no entry of its own falls back to the global one.
	got, err := repo.GetByScope(ctx, "initech", shared)
	if err != nil {
		t.Fatalf("GetByScope initech: %v", err)
	}
	if got.TenantID != "" || got.Description != "global shared" {
		t.Errorf("initech got %q (tenant %q), want the global entry", got.Description, got.TenantID)
	}

	// Exists sees the tenant's and global entries, and no one else's.
	ok, err := repo.Exists(ctx, "initech", globalOnly)
	if err != nil || !ok {
		t.Errorf("Exists(initech, global-only) = %v, %v; want true", ok, err)
	}

	// List returns one entry per scope: the tenant's own plus global ones.
	list, err := repo.List(ctx, "acme", "test")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var mine []*ScopeEntry
	for _, e := range list {
		if strings.HasPrefix(e.Scope, "test:"+service+":") {
			mine = append(mine, e)
		}
	}
	if len(mine) != 2 {
		t.Fatalf("List(acme) returned %d entries for this test, want 2 (shared + global-only): %+v", len(mine), mine)
	}
	for _, e := range mine {
		if e.Scope == shared && e.Description != "acme shared" {
			t.Errorf("List(acme) shared = %q, want acme's own entry", e.Description)
		}
	}

	// A global prune removes only global entries missing from the keep list.
	deleted, err := repo.DeleteOrphans(ctx, service, "", []string{globalOnly})
	if err != nil {
		t.Fatalf("DeleteOrphans: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != shared {
		t.Errorf("global prune deleted %v, want only the global %q", deleted, shared)
	}
	for _, tenant := range []string{"acme", "globex"} {
		got, err := repo.GetByScope(ctx, tenant, shared)
		if err != nil || got.TenantID != tenant {
			t.Errorf("tenant %q entry gone after a global prune: %v", tenant, err)
		}
	}
}
