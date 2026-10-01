package inferencegw

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPricingRepo connects to ORBIT_TEST_DB_URL and removes the rows a test
// writes under its random provider name.
func testPricingRepo(t *testing.T) (*PostgresPricingCatalogRepository, ProviderName) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run pricing catalog Postgres integration tests")
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
	provider := ProviderName("compatible:test-" + uuid.NewString()[:8])
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM model_pricing WHERE provider = $1`, string(provider))
		pool.Close()
	})
	return NewPostgresPricingCatalogRepository(pool), provider
}

func TestPostgres_SetOverrideCreatesAndUpdatesRow(t *testing.T) {
	repo, provider := testPricingRepo(t)
	ctx := context.Background()
	in, out, newOut := 3.0, 15.0, 12.5

	if err := repo.SetOverride(ctx, provider, "vendor/model", &in, &out, nil, nil); err != nil {
		t.Fatalf("set override with no synced row: %v", err)
	}
	if err := repo.SetOverride(ctx, provider, "vendor/model", nil, &newOut, nil, nil); err != nil {
		t.Fatalf("update override: %v", err)
	}

	entries, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Provider != provider {
			continue
		}
		if e.OverrideInput == nil || *e.OverrideInput != in {
			t.Errorf("input override = %v, want %v kept", e.OverrideInput, in)
		}
		if e.OverrideOutput == nil || *e.OverrideOutput != newOut {
			t.Errorf("output override = %v, want %v", e.OverrideOutput, newOut)
		}
		if e.LastSyncedAt.Year() != 1970 {
			t.Errorf("last_synced_at = %v, want the epoch for an override-only row", e.LastSyncedAt)
		}
		return
	}
	t.Fatal("override-only row not found")
}
