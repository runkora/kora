package inferencegw

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
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

func TestPostgres_DiscoveredPricesKeepOverrides(t *testing.T) {
	repo, provider := testPricingRepo(t)
	ctx := context.Background()
	override := 1.0
	discover := func(in, out float64) {
		NewDiscoveredPrices(repo, noopRefresher{}, logging.NewLoggerWithOutput(io.Discard)).Record(ctx, provider, []ProviderModel{
			{Provider: provider, ModelID: "vendor/model", Price: &ModelPriceEntry{InputCostPer1M: in, OutputCostPer1M: out}},
		})
	}

	discover(3, 15)
	if err := repo.SetOverride(ctx, provider, "vendor/model", &override, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	discover(4, 16)

	rows := providerRows(t, repo, provider)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want exactly one", rows)
	}
	e := rows["vendor/model"]
	if e.SyncedInput != 4 || e.SyncedOutput != 16 {
		t.Errorf("synced = %v in, %v out, want the second discovery's 4 and 16", e.SyncedInput, e.SyncedOutput)
	}
	if e.EffectiveInput() != override || e.EffectiveOutput() != 16 {
		t.Errorf("effective = %v in, %v out, want the override and the synced output", e.EffectiveInput(), e.EffectiveOutput())
	}
}

func TestPostgres_ReplaceSyncedDropsUnlistedPrices(t *testing.T) {
	repo, provider := testPricingRepo(t)
	ctx := context.Background()
	price := func(id string) ModelPriceEntry {
		return ModelPriceEntry{Provider: provider, ModelID: id, InputCostPer1M: 2, OutputCostPer1M: 8}
	}
	if err := repo.ReplaceSynced(ctx, provider, []ModelPriceEntry{price("kept"), price("gone"), price("overridden")}); err != nil {
		t.Fatal(err)
	}
	override := 5.0
	if err := repo.SetOverride(ctx, provider, "overridden", &override, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := repo.ReplaceSynced(ctx, provider, []ModelPriceEntry{price("kept")}); err != nil {
		t.Fatal(err)
	}

	rows := providerRows(t, repo, provider)
	if _, ok := rows["gone"]; ok || len(rows) != 2 {
		t.Fatalf("rows = %v, want kept and overridden only", rows)
	}
	o := rows["overridden"]
	if o.EffectiveInput() != override || o.SyncedOutput != 0 || o.LastSyncedAt.Year() != 1970 {
		t.Errorf("overridden row = %+v, want its override kept and its synced prices cleared", o)
	}
}

func TestPostgres_ReplaceSyncedIsAllOrNothing(t *testing.T) {
	repo, provider := testPricingRepo(t)
	ctx := context.Background()
	// NUMERIC(10,6) holds at most 9999.999999, so the second entry fails.
	err := repo.ReplaceSynced(ctx, provider, []ModelPriceEntry{
		{Provider: provider, ModelID: "first", InputCostPer1M: 1},
		{Provider: provider, ModelID: "second", InputCostPer1M: 1e6},
	})
	if err == nil {
		t.Fatal("want an overflow error")
	}
	if rows := providerRows(t, repo, provider); len(rows) != 0 {
		t.Errorf("rows = %v, want none after a failed batch", rows)
	}
}

func TestPostgres_ReplaceSyncedWithNoEntries(t *testing.T) {
	repo, provider := testPricingRepo(t)
	ctx := context.Background()
	if err := repo.ReplaceSynced(ctx, provider, []ModelPriceEntry{
		{Provider: provider, ModelID: "plain", InputCostPer1M: 2},
		{Provider: provider, ModelID: "overridden", InputCostPer1M: 2},
	}); err != nil {
		t.Fatal(err)
	}
	override := 5.0
	if err := repo.SetOverride(ctx, provider, "overridden", &override, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := repo.ReplaceSynced(ctx, provider, nil); err != nil {
		t.Fatal(err)
	}

	rows := providerRows(t, repo, provider)
	if len(rows) != 1 || rows["overridden"].EffectiveInput() != override || rows["overridden"].SyncedInput != 0 {
		t.Errorf("rows = %+v, want only the override-only row", rows)
	}
}

// providerRows returns the pricing rows of provider by model id.
func providerRows(t *testing.T, repo *PostgresPricingCatalogRepository, provider ProviderName) map[string]PricingCatalogEntry {
	t.Helper()
	entries, err := repo.LoadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]PricingCatalogEntry{}
	for _, e := range entries {
		if e.Provider == provider {
			if _, dup := rows[e.ModelID]; dup {
				t.Fatalf("duplicate row for %s", e.ModelID)
			}
			rows[e.ModelID] = e
		}
	}
	return rows
}

type noopRefresher struct{}

func (noopRefresher) Refresh(context.Context) error { return nil }
