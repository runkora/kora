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
// KORA_TEST_DB_URL points at a database with policy_role_scopes migrated;
// otherwise they skip. Each test isolates itself under a random tenant and
// cleans up on exit.
//
//	KORA_TEST_DB_URL='postgres://kora:kora@localhost:5432/kora?sslmode=disable' \
//	  go test ./internal/policyservice/roles/ -run Postgres
func testRolesRepo(t *testing.T) (Repository, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run roles Postgres integration tests")
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

// Excluded names are filtered in SQL, so pages stay full and the count matches the list.
func TestPostgres_ListAndCountByTenant_Exclude(t *testing.T) {
	repo, tenant := testRolesRepo(t)
	ctx := context.Background()
	for _, role := range []string{"a-role", "b-reserved", "c-role", "d-role"} {
		if err := repo.Upsert(ctx, &RoleEntry{TenantID: tenant, Role: role, Scopes: []string{"x:read"}}); err != nil {
			t.Fatalf("Upsert %s: %v", role, err)
		}
	}
	names := func(entries []*RoleEntry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.Role)
		}
		return out
	}

	tests := []struct {
		name          string
		exclude       []string
		limit, offset int
		wantList      []string
		wantCount     int
	}{
		{"nil exclude lists every row", nil, 0, 0, []string{"a-role", "b-reserved", "c-role", "d-role"}, 4},
		{"first page skips the excluded row", []string{"b-reserved"}, 2, 0, []string{"a-role", "c-role"}, 3},
		{"second page", []string{"b-reserved"}, 2, 2, []string{"d-role"}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.ListByTenant(ctx, tenant, tt.exclude, tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("ListByTenant: %v", err)
			}
			if !slices.Equal(names(got), tt.wantList) {
				t.Errorf("list = %v, want %v", names(got), tt.wantList)
			}
			count, err := repo.CountByTenant(ctx, tenant, tt.exclude)
			if err != nil {
				t.Fatalf("CountByTenant: %v", err)
			}
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
		})
	}
}

func TestPostgres_DeclaredRoles(t *testing.T) {
	repo, tenant := testRolesRepo(t)
	ctx := context.Background()

	if err := repo.Upsert(ctx, &RoleEntry{TenantID: tenant, Role: "api-role", Scopes: []string{"a:read"}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := repo.UpsertDeclared(ctx, &RoleEntry{TenantID: tenant, Role: "api-role", Scopes: []string{"b:read"}, ResourceName: "x", ResourceUID: "uid-x"}); !errors.Is(err, ErrRoleTaken) {
		t.Fatalf("declared write over an api row: err = %v, want ErrRoleTaken", err)
	}

	budget := &BudgetAssignment{LimitUSD: 10, ResetCadence: "weekly"}
	declared := &RoleEntry{TenantID: tenant, Role: "declared", Scopes: []string{"a:read"}, Budget: budget, ResourceName: "res", ResourceUID: "uid-1"}
	if err := repo.UpsertDeclared(ctx, declared); err != nil {
		t.Fatalf("UpsertDeclared: %v", err)
	}
	got, err := repo.GetByRole(ctx, tenant, "declared")
	if err != nil || got.Source != SourceCRD || got.ResourceUID != "uid-1" || got.ResourceName != "res" || got.Budget == nil {
		t.Fatalf("GetByRole = %+v, %v", got, err)
	}
	if got, _ := repo.GetByRole(ctx, tenant, "api-role"); got.Source != SourceAPI || got.ResourceUID != "" || got.ResourceName != "" {
		t.Errorf("api row = %+v", got)
	}

	if err := repo.Upsert(ctx, &RoleEntry{TenantID: tenant, Role: "declared", Scopes: []string{"c:read"}}); !errors.Is(err, ErrManagedByResource) {
		t.Errorf("api upsert over a crd row: err = %v", err)
	}
	if _, err := repo.UpdateBudget(ctx, tenant, "declared", nil); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("api budget change on a crd row: err = %v", err)
	}
	if err := repo.Delete(ctx, tenant, "declared"); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("api delete of a crd row: err = %v", err)
	}

	// Another resource cannot take the role.
	if err := repo.UpsertDeclared(ctx, &RoleEntry{TenantID: tenant, Role: "declared", Scopes: []string{"a:read"}, ResourceName: "other", ResourceUID: "uid-2"}); !errors.Is(err, ErrRoleTaken) {
		t.Errorf("another resource's write: err = %v, want ErrRoleTaken", err)
	}

	// A declared write replaces the budget in full. A resource recreated under its name takes its row back.
	if err := repo.UpsertDeclared(ctx, &RoleEntry{TenantID: tenant, Role: "declared", Scopes: []string{"a:read"}, ResourceName: "res", ResourceUID: "uid-3"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetByRole(ctx, tenant, "declared"); got.Budget != nil || got.ResourceUID != "uid-3" {
		t.Errorf("row = %+v, want no budget and uid-3", got)
	}

	// Renaming the role moves the resource's row.
	if err := repo.UpsertDeclared(ctx, &RoleEntry{TenantID: tenant, Role: "renamed", Scopes: []string{"a:read"}, ResourceName: "res", ResourceUID: "uid-3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByRole(ctx, tenant, "declared"); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("old role name survived a rename: %v", err)
	}
	list, err := repo.ListDeclared(ctx, tenant)
	if err != nil || len(list) != 1 || list[0].Role != "renamed" || list[0].ResourceName != "res" || list[0].ResourceUID != "uid-3" {
		t.Fatalf("ListDeclared = %+v, %v", list, err)
	}

	if deleted, err := repo.DeleteDeclared(ctx, tenant, "res"); err != nil || !deleted {
		t.Fatalf("DeleteDeclared = %v, %v", deleted, err)
	}
	if deleted, err := repo.DeleteDeclared(ctx, tenant, "res"); err != nil || deleted {
		t.Errorf("second DeleteDeclared = %v, %v", deleted, err)
	}
	if _, err := repo.DeleteDeclared(ctx, tenant, "api-role"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByRole(ctx, tenant, "api-role"); err != nil {
		t.Errorf("api row lost: %v", err)
	}
}
