package roles

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the roles Repository. They run only when
// ORBIT_TEST_DB_URL points at a database with policy_role_scopes migrated;
// otherwise they skip. Each test isolates itself under a random tenant and
// cleans up on exit.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/policyservice/roles/ -run Postgres
func testRolesRepo(t *testing.T) (Repository, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run roles Postgres integration tests")
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
		_, _ = pool.Exec(ctx, `DELETE FROM policy_role_scopes WHERE tenant_id = $1`, tenant)
		pool.Close()
	})
	return NewPostgresRepository(pool), tenant
}

func TestPostgres_PurgeScopeFromAll_Roles(t *testing.T) {
	repo, tenant := testRolesRepo(t)
	ctx := context.Background()

	keep := "keep:scope"
	purge := "purge:scope"

	if err := repo.Upsert(ctx, &RoleEntry{
		TenantID: tenant,
		Role:     "role-a",
		Scopes:   []string{keep, purge},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := repo.Upsert(ctx, &RoleEntry{
		TenantID: tenant,
		Role:     "role-b",
		Scopes:   []string{keep},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := repo.PurgeScopeFromAll(ctx, purge); err != nil {
		t.Fatalf("PurgeScopeFromAll: %v", err)
	}

	roleA, err := repo.GetByRole(ctx, tenant, "role-a")
	if err != nil {
		t.Fatalf("GetByRole role-a: %v", err)
	}
	if slices.Contains(roleA.Scopes, purge) {
		t.Errorf("role-a still contains purged scope %q; scopes=%v", purge, roleA.Scopes)
	}
	if !slices.Contains(roleA.Scopes, keep) {
		t.Errorf("role-a lost scope %q that should be kept; scopes=%v", keep, roleA.Scopes)
	}

	roleB, err := repo.GetByRole(ctx, tenant, "role-b")
	if err != nil {
		t.Fatalf("GetByRole role-b: %v", err)
	}
	if slices.Contains(roleB.Scopes, purge) {
		t.Errorf("role-b (unaffected row) now contains purged scope")
	}
}

func TestPostgres_UpdateBudget_Roles(t *testing.T) {
	repo, tenant := testRolesRepo(t)
	ctx := context.Background()

	if err := repo.Upsert(ctx, &RoleEntry{TenantID: tenant, Role: "role-a", Scopes: []string{"keep:scope"}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	budget := &BudgetAssignment{LimitUSD: 50, ResetCadence: "monthly"}
	got, err := repo.UpdateBudget(ctx, tenant, "role-a", budget)
	if err != nil {
		t.Fatalf("UpdateBudget set: %v", err)
	}
	if got.Budget == nil || got.Budget.LimitUSD != 50 || !slices.Equal(got.Scopes, []string{"keep:scope"}) {
		t.Fatalf("after set: budget %+v scopes %v", got.Budget, got.Scopes)
	}

	if _, err := repo.UpdateBudget(ctx, tenant, "role-a", nil); err != nil {
		t.Fatalf("UpdateBudget clear: %v", err)
	}
	stored, err := repo.GetByRole(ctx, tenant, "role-a")
	if err != nil {
		t.Fatalf("GetByRole: %v", err)
	}
	if stored.Budget != nil || !slices.Equal(stored.Scopes, []string{"keep:scope"}) {
		t.Errorf("after clear: budget %+v scopes %v, want no budget and scopes kept", stored.Budget, stored.Scopes)
	}

	if _, err := repo.UpdateBudget(ctx, tenant, "missing", budget); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("UpdateBudget on a missing role = %v, want ErrRoleNotFound", err)
	}
}
