package vocabulary

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the vocabulary Repository. They run only when
// KORA_TEST_DB_URL points at a database with policy_scope_vocabulary
// migrated; otherwise they skip.
//
//	KORA_TEST_DB_URL='postgres://kora:kora@localhost:5432/kora?sslmode=disable' \
//	  go test ./internal/policyservice/vocabulary/ -run Postgres
func testVocabRepo(t *testing.T) (Repository, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run vocabulary Postgres integration tests")
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

// TestPostgres_UpsertOwnershipAndRequires covers who may change an entry and
// how requires moves. Only the owner writes; nil requires keeps the stored
// edges; a non-nil empty slice clears them.
func TestPostgres_UpsertOwnershipAndRequires(t *testing.T) {
	repo, service := testVocabRepo(t)
	ctx := context.Background()
	scope := "test:" + service + ":write"

	entry := func(owner, desc string, requires []string) *ScopeEntry {
		return &ScopeEntry{
			Scope: scope, ResourceType: "test", PathSegments: []string{service}, Action: "write",
			Description: desc, RegisteredBy: owner, Requires: requires,
		}
	}
	if _, err := repo.Upsert(ctx, entry(service, "owned", []string{"platform:grants:read"})); err != nil {
		t.Fatalf("seed: %v", err)
	}

	steps := []struct {
		name         string
		entry        *ScopeEntry
		wantChanged  bool
		wantErr      error
		wantDesc     string
		wantRequires []string
	}{
		{
			name:    "another service cannot overwrite",
			entry:   entry("other-"+service, "hijacked", []string{"platform:grants:write"}),
			wantErr: ErrScopeOwnedByAnother, wantDesc: "owned", wantRequires: []string{"platform:grants:read"},
		},
		{
			name:    "another service cannot clear",
			entry:   entry("other-"+service, "owned", []string{}),
			wantErr: ErrScopeOwnedByAnother, wantDesc: "owned", wantRequires: []string{"platform:grants:read"},
		},
		{
			name:     "owner with unknown requires keeps the edge",
			entry:    entry(service, "owned", nil),
			wantDesc: "owned", wantRequires: []string{"platform:grants:read"},
		},
		{
			name:        "owner clears requires explicitly",
			entry:       entry(service, "owned", []string{}),
			wantChanged: true, wantDesc: "owned", wantRequires: []string{},
		},
		{
			name:     "owner clearing again changes nothing",
			entry:    entry(service, "owned", []string{}),
			wantDesc: "owned", wantRequires: []string{},
		},
	}
	for _, st := range steps {
		changed, err := repo.Upsert(ctx, st.entry)
		if !errors.Is(err, st.wantErr) {
			t.Fatalf("%s: err = %v, want %v", st.name, err, st.wantErr)
		}
		if changed != st.wantChanged {
			t.Errorf("%s: changed = %v, want %v", st.name, changed, st.wantChanged)
		}
		got, err := repo.GetByScope(ctx, scope)
		if err != nil {
			t.Fatalf("%s: get: %v", st.name, err)
		}
		if got.Description != st.wantDesc || !slices.Equal(got.Requires, st.wantRequires) {
			t.Errorf("%s: stored (%q, %v), want (%q, %v)", st.name, got.Description, got.Requires, st.wantDesc, st.wantRequires)
		}
	}
}
