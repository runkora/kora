package tenantbudget

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

// Postgres integration tests for the tenant budget Repository. They run only
// when KORA_TEST_DB_URL points at a migrated database; otherwise they skip.
func testRepo(t *testing.T) (Repository, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run tenant budget Postgres integration tests")
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
		_, _ = pool.Exec(ctx, `DELETE FROM policy_tenant_budgets WHERE tenant_id = $1`, tenant)
		pool.Close()
	})
	return NewPostgresRepository(pool), tenant
}

func declaredFor(t *testing.T, repo Repository, tenant string) *TenantBudget {
	t.Helper()
	all, err := repo.ListDeclared(context.Background())
	if err != nil {
		t.Fatalf("ListDeclared: %v", err)
	}
	for _, tb := range all {
		if tb.TenantID == tenant {
			return tb
		}
	}
	return nil
}

func TestPostgres_DeclaredTenantBudget(t *testing.T) {
	repo, tenant := testRepo(t)
	ctx := context.Background()

	if err := repo.Upsert(ctx, &TenantBudget{TenantID: tenant, LimitUSD: 5, ResetCadence: "weekly"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got, err := repo.Get(ctx, tenant); err != nil || got.Source != declarative.SourceAPI {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	// An api budget is not declared, so removing the declared budget leaves it.
	if err := repo.DeleteDeclared(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, tenant); err != nil {
		t.Fatalf("api budget lost: %v", err)
	}

	if err := repo.UpsertDeclared(ctx, &TenantBudget{TenantID: tenant, LimitUSD: 1000, ResetCadence: "monthly"}); err != nil {
		t.Fatalf("UpsertDeclared: %v", err)
	}
	got, err := repo.Get(ctx, tenant)
	if err != nil || got.Source != declarative.SourceCRD || got.LimitUSD != 1000 || got.ResetCadence != "monthly" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if tb := declaredFor(t, repo, tenant); tb == nil || tb.LimitUSD != 1000 {
		t.Errorf("ListDeclared entry = %+v", tb)
	}

	if err := repo.Upsert(ctx, &TenantBudget{TenantID: tenant, LimitUSD: 1, ResetCadence: "weekly"}); !errors.Is(err, declarative.ErrManagedByResource) {
		t.Errorf("api upsert over a crd budget: err = %v", err)
	}
	if err := repo.Delete(ctx, tenant); !errors.Is(err, ErrTenantBudgetNotFound) {
		t.Errorf("api delete of a crd budget: err = %v", err)
	}

	if err := repo.DeleteDeclared(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, tenant); !errors.Is(err, ErrTenantBudgetNotFound) {
		t.Errorf("Get after DeleteDeclared: err = %v", err)
	}
}
