package inferencegw

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// testPricingRepo connects to KORA_TEST_DB_URL and removes the rows a test
// writes under its random provider name.
func testPricingRepo(t *testing.T) (*PostgresPricingCatalogRepository, ProviderName) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run pricing catalog Postgres integration tests")
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

// A regional profile's row would shadow the base model's row, so it must end up
// with an input and an output price: a new row, or an override-only row a clear
// left empty. Updating a row that already has both, and any non-regional ID,
// keep accepting a partial override.
func TestPostgres_SetOverrideRegionalRowNeedsInputAndOutput(t *testing.T) {
	repo, compatible := testPricingRepo(t)
	ctx := context.Background()
	bedrock := ProviderName("bedrock:test-" + uuid.NewString()[:8])
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(ctx, `DELETE FROM model_pricing WHERE provider = $1`, string(bedrock))
	})
	in, out := 9.0, 45.0

	if err := repo.SetOverride(ctx, bedrock, "us.anthropic.claude-haiku-4-5", &in, nil, nil, nil); !errors.Is(err, ErrIncompleteOverride) {
		t.Fatalf("input-only override of a new regional row: err = %v, want ErrIncompleteOverride", err)
	}
	if err := repo.SetOverride(ctx, bedrock, "us.anthropic.claude-haiku-4-5", &in, &out, nil, nil); err != nil {
		t.Fatalf("input and output on a new regional row: %v", err)
	}
	if err := repo.SetOverride(ctx, bedrock, "us.anthropic.claude-haiku-4-5", nil, &in, nil, nil); err != nil {
		t.Errorf("partial override of an existing regional row: %v", err)
	}
	if err := repo.SetOverride(ctx, bedrock, "anthropic.claude-haiku-4-5", &in, nil, nil, nil); err != nil {
		t.Errorf("partial override of a base row: %v", err)
	}

	// Clearing an override-only row deletes it: with no price left it would only
	// shadow the prices the route falls back to. A partial update is then
	// refused again, as for any new regional row.
	if err := repo.ClearOverride(ctx, bedrock, "us.anthropic.claude-haiku-4-5"); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	if err := repo.ClearOverride(ctx, bedrock, "us.anthropic.claude-haiku-4-5"); !errors.Is(err, ErrModelPricingNotFound) {
		t.Errorf("second clear of a deleted override-only row: err = %v, want ErrModelPricingNotFound", err)
	}
	if err := repo.SetOverride(ctx, bedrock, "us.anthropic.claude-haiku-4-5", &in, nil, nil, nil); !errors.Is(err, ErrIncompleteOverride) {
		t.Errorf("input-only override after the row was deleted: err = %v, want ErrIncompleteOverride", err)
	}
	if err := repo.SetOverride(ctx, bedrock, "us.anthropic.claude-haiku-4-5", &in, &out, nil, nil); err != nil {
		t.Errorf("input and output after the row was deleted: %v", err)
	}

	// A synced row keeps its synced prices when its override is cleared.
	if err := repo.UpsertSynced(ctx, []ModelPriceEntry{{Provider: bedrock, ModelID: "ap.anthropic.claude-haiku-4-5", InputCostPer1M: 1.1, OutputCostPer1M: 5.5}}); err != nil {
		t.Fatalf("upsert synced: %v", err)
	}
	if err := repo.SetOverride(ctx, bedrock, "ap.anthropic.claude-haiku-4-5", &in, nil, nil, nil); err != nil {
		t.Fatalf("partial override of a synced regional row: %v", err)
	}
	if err := repo.ClearOverride(ctx, bedrock, "ap.anthropic.claude-haiku-4-5"); err != nil {
		t.Fatalf("clear override of a synced row: %v", err)
	}
	entries, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var kept *PricingCatalogEntry
	for i := range entries {
		if entries[i].Provider == bedrock && entries[i].ModelID == "ap.anthropic.claude-haiku-4-5" {
			kept = &entries[i]
		}
	}
	if kept == nil || kept.SyncedInput != 1.1 || kept.HasOverride() {
		t.Errorf("synced row after clear = %+v, want it kept with synced input 1.1 and no override", kept)
	}

	// An explicit 0 is a price: a row overridden to free is complete.
	zero := 0.0
	if err := repo.SetOverride(ctx, bedrock, "eu.anthropic.claude-haiku-4-5", &zero, &zero, nil, nil); err != nil {
		t.Fatalf("free override on a new regional row: %v", err)
	}
	if err := repo.SetOverride(ctx, bedrock, "eu.anthropic.claude-haiku-4-5", &in, nil, nil, nil); err != nil {
		t.Errorf("partial update of a row overridden to free: %v", err)
	}
	if err := repo.SetOverride(ctx, compatible, "vendor/model", &in, nil, nil, nil); err != nil {
		t.Errorf("partial override of a non-Bedrock row: %v", err)
	}
}

// Rows are inserted out of order: without an ORDER BY, LoadAll returns them in
// storage order, which changes whenever the sync rewrites rows.
func TestPostgres_LoadAllReturnsRowsInPrimaryKeyOrder(t *testing.T) {
	repo, provider := testPricingRepo(t)
	ctx := context.Background()
	price := 1.0
	for _, id := range []string{"vendor/c", "vendor/a", "vendor/b"} {
		if err := repo.SetOverride(ctx, provider, id, &price, nil, nil, nil); err != nil {
			t.Fatalf("set override %s: %v", id, err)
		}
	}

	entries, err := repo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if e.Provider == provider {
			got = append(got, e.ModelID)
		}
	}
	if want := []string{"vendor/a", "vendor/b", "vendor/c"}; !slices.Equal(got, want) {
		t.Errorf("LoadAll order = %v, want %v", got, want)
	}
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
