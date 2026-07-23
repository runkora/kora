package grants

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the grants Repository. They run only when
// ORBIT_TEST_DB_URL points at a database with policy_grants migrated;
// otherwise they skip. Each test isolates itself under a random tenant and
// cleans up on exit.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/policyservice/grants/ -run Postgres
func testGrantsRepo(t *testing.T) (Repository, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run grants Postgres integration tests")
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
		_, _ = pool.Exec(ctx, `DELETE FROM policy_grants WHERE tenant_id = $1`, tenant)
		pool.Close()
	})
	return NewPostgresRepository(pool), tenant
}

func TestPostgres_PurgeScopeFromAll_Grants(t *testing.T) {
	repo, tenant := testGrantsRepo(t)
	ctx := context.Background()

	keep := "keep:scope"
	purge := "purge:scope"

	grantA := &Grant{
		TenantID:      tenant,
		PrincipalType: PrincipalTypeAgent,
		PrincipalID:   "agent:" + uuid.NewString(),
		Scopes:        []string{keep, purge},
	}
	if err := repo.Create(ctx, grantA); err != nil {
		t.Fatalf("Create grant-a: %v", err)
	}

	grantB := &Grant{
		TenantID:      tenant,
		PrincipalType: PrincipalTypeAgent,
		PrincipalID:   "agent:" + uuid.NewString(),
		Scopes:        []string{keep},
	}
	if err := repo.Create(ctx, grantB); err != nil {
		t.Fatalf("Create grant-b: %v", err)
	}

	if err := repo.PurgeScopeFromAll(ctx, purge); err != nil {
		t.Fatalf("PurgeScopeFromAll: %v", err)
	}

	gotA, err := repo.GetByID(ctx, grantA.ID)
	if err != nil {
		t.Fatalf("GetByID grant-a: %v", err)
	}
	if slices.Contains(gotA.Scopes, purge) {
		t.Errorf("grant-a still contains purged scope %q; scopes=%v", purge, gotA.Scopes)
	}
	if !slices.Contains(gotA.Scopes, keep) {
		t.Errorf("grant-a lost scope %q that should be kept; scopes=%v", keep, gotA.Scopes)
	}

	gotB, err := repo.GetByID(ctx, grantB.ID)
	if err != nil {
		t.Fatalf("GetByID grant-b: %v", err)
	}
	if slices.Contains(gotB.Scopes, purge) {
		t.Errorf("grant-b (unaffected row) now contains purged scope")
	}
}
