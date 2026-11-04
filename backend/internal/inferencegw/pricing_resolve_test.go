package inferencegw

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func bedrockPrice(modelID string, in, out float64) ModelPricing {
	return ModelPricing{Provider: ProviderBedrock, ModelID: modelID, InputCostPerMillion: in, OutputCostPerMillion: out}
}

// permutations calls fn with every ordering of rows, so a test can assert that
// the order rows load in never changes what a route is priced at.
func permutations[T any](rows []T, fn func([]T)) {
	var rec func(k int)
	work := append([]T(nil), rows...)
	rec = func(k int) {
		if k == len(work) {
			fn(append([]T(nil), work...))
			return
		}
		for i := k; i < len(work); i++ {
			work[k], work[i] = work[i], work[k]
			rec(k + 1)
			work[k], work[i] = work[i], work[k]
		}
	}
	rec(0)
}

// rotations calls fn with every rotation of rows and of rows reversed: a cheaper
// stand-in for permutations on a larger fixture, which still puts every row
// both first and last.
func rotations[T any](rows []T, fn func([]T)) {
	reversed := make([]T, len(rows))
	for i, r := range rows {
		reversed[len(rows)-1-i] = r
	}
	for _, base := range [][]T{rows, reversed} {
		for i := range base {
			fn(append(append([]T(nil), base[i:]...), base[:i]...))
		}
	}
}

// One model with a row per inference profile, every price distinct so a route
// resolving to a neighbour's row cannot pass by coincidence.
func TestPricingIndexPricesEachProfileByItsOwnRow(t *testing.T) {
	t.Parallel()
	rows := []ModelPricing{
		bedrockPrice("anthropic.claude-sonnet-5", 2, 10),
		bedrockPrice("global.anthropic.claude-sonnet-5", 1.8, 9),
		bedrockPrice("us.anthropic.claude-sonnet-5", 2.2, 11),
		bedrockPrice("eu.anthropic.claude-sonnet-5", 2.4, 12),
	}

	for _, tc := range []struct {
		name     string
		provider ProviderName
		modelID  string
		wantIn   float64
		wantOut  float64
		want     priceMatch
	}{
		{"base ID", ProviderBedrock, "anthropic.claude-sonnet-5", 2, 10, matchExact},
		{"global profile", ProviderBedrock, "global.anthropic.claude-sonnet-5", 1.8, 9, matchExact},
		{"us profile", ProviderBedrock, "us.anthropic.claude-sonnet-5", 2.2, 11, matchExact},
		{"eu profile", ProviderBedrock, "eu.anthropic.claude-sonnet-5", 2.4, 12, matchExact},
		{"aliased provider resolves the type's row", "bedrock:usw", "us.anthropic.claude-sonnet-5", 2.2, 11, matchExact},
		{"version suffix is ignored, region is not", ProviderBedrock, "us.anthropic.claude-sonnet-5-v1:0", 2.2, 11, matchExact},
		{"profile without a row falls back to the base ID", ProviderBedrock, "au.anthropic.claude-sonnet-5", 2, 10, matchBase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			permutations(rows, func(order []ModelPricing) {
				got, match, ok := newModelPricingIndex(order).Resolve(tc.provider, tc.modelID)
				if !ok {
					t.Fatalf("Resolve(%q, %q) missed, rows %v", tc.provider, tc.modelID, order)
				}
				if got.InputCostPerMillion != tc.wantIn || got.OutputCostPerMillion != tc.wantOut || match != tc.want {
					t.Fatalf("Resolve(%q, %q) = %v/%v (%v), want %v/%v (%v), rows %v",
						tc.provider, tc.modelID, got.InputCostPerMillion, got.OutputCostPerMillion, match,
						tc.wantIn, tc.wantOut, tc.want, order)
				}
			})
		})
	}
}

// With no base row, a profile that has no row of its own is priced by the
// global profile rather than whichever regional row was loaded last.
func TestPricingIndexFallbackIgnoresLoadOrder(t *testing.T) {
	t.Parallel()
	rows := []ModelPricing{
		bedrockPrice("us.anthropic.claude-sonnet-5", 2.2, 11),
		bedrockPrice("eu.anthropic.claude-sonnet-5", 2.4, 12),
		bedrockPrice("global.anthropic.claude-sonnet-5", 1.8, 9),
	}
	permutations(rows, func(order []ModelPricing) {
		got, match, ok := newModelPricingIndex(order).Resolve(ProviderBedrock, "au.anthropic.claude-sonnet-5")
		if !ok || match != matchBase || got.InputCostPerMillion != 1.8 {
			t.Fatalf("got %v (%v, ok=%v), want the global row 1.8, rows %v", got.InputCostPerMillion, match, ok, order)
		}
	})
}

// With no row of its own and no base or global row, a route is priced by another
// region's row of the same model as a last resort. It borrows the same region
// whatever order the rows load in, and prefers it to another provider's price.
func TestPricingIndexBorrowsAnotherRegionAsALastResort(t *testing.T) {
	t.Parallel()
	rows := []ModelPricing{
		bedrockPrice("us.anthropic.claude-sonnet-5", 2.2, 11),
		bedrockPrice("eu.anthropic.claude-sonnet-5", 2.4, 12),
	}

	for _, modelID := range []string{
		"au.anthropic.claude-sonnet-5",     // names a region with no row
		"global.anthropic.claude-sonnet-5", // names the global profile
		"anthropic.claude-sonnet-5",        // names no region
	} {
		permutations(rows, func(order []ModelPricing) {
			got, match, ok := newModelPricingIndex(order).Resolve(ProviderBedrock, modelID)
			if !ok || match != matchRegion || got.InputCostPerMillion != 2.4 {
				t.Fatalf("Resolve(%q) = %v (%v, ok=%v), want the plainest region's row 2.4 as a region match, rows %v",
					modelID, got.InputCostPerMillion, match, ok, order)
			}
		})
	}

	azure := ModelPricing{Provider: "azure", ModelID: "claude-sonnet-5", InputCostPerMillion: 3, OutputCostPerMillion: 15}

	t.Run("a nearer row wins over another region", func(t *testing.T) {
		t.Parallel()
		withGlobal := append([]ModelPricing{bedrockPrice("global.anthropic.claude-sonnet-5", 1.8, 9)}, rows...)
		got, match, ok := newModelPricingIndex(withGlobal).Resolve(ProviderBedrock, "au.anthropic.claude-sonnet-5")
		if !ok || match != matchBase || got.InputCostPerMillion != 1.8 {
			t.Errorf("got %v (%v, ok=%v), want the global row 1.8", got.InputCostPerMillion, match, ok)
		}
	})

	t.Run("another region wins over another provider", func(t *testing.T) {
		t.Parallel()
		got, match, ok := newModelPricingIndex(append([]ModelPricing{azure}, rows...)).Resolve(ProviderBedrock, "au.anthropic.claude-sonnet-5")
		if !ok || match != matchRegion || got.Provider != ProviderBedrock {
			t.Errorf("got %+v (%v, ok=%v), want a Bedrock region row, not the azure one", got, match, ok)
		}
	})

	t.Run("another provider prices a model with no Bedrock rows", func(t *testing.T) {
		t.Parallel()
		got, match, ok := newModelPricingIndex([]ModelPricing{azure}).Resolve(ProviderBedrock, "au.anthropic.claude-sonnet-5")
		if !ok || match != matchBare || got.Provider != "azure" {
			t.Errorf("got %+v (%v, ok=%v), want the azure row as a bare match", got, match, ok)
		}
	})
}

// A regional row with no price at all (an override-only row whose override was
// cleared, or a row the sync stopped listing) must not shadow the base price.
func TestPricingIndexUnpricedExactRowYieldsToBase(t *testing.T) {
	t.Parallel()
	rows := []ModelPricing{
		bedrockPrice("anthropic.claude-haiku-4-5", 1, 5),
		bedrockPrice("us.anthropic.claude-haiku-4-5", 0, 0),
	}
	permutations(rows, func(order []ModelPricing) {
		got, match, ok := newModelPricingIndex(order).Resolve(ProviderBedrock, "us.anthropic.claude-haiku-4-5")
		if !ok || match != matchBase || got.InputCostPerMillion != 1 || got.OutputCostPerMillion != 5 {
			t.Fatalf("got %v/%v (%v, ok=%v), want the base row 1/5, rows %v",
				got.InputCostPerMillion, got.OutputCostPerMillion, match, ok, order)
		}
	})

	t.Run("with no priced base row the unpriced row is still returned", func(t *testing.T) {
		t.Parallel()
		_, match, ok := newModelPricingIndex([]ModelPricing{bedrockPrice("us.anthropic.claude-haiku-4-5", 0, 0)}).
			Resolve(ProviderBedrock, "us.anthropic.claude-haiku-4-5")
		if !ok || match != matchExact {
			t.Fatalf("match = %v, ok = %v, want the exact row: a model can be free", match, ok)
		}
	})

	t.Run("a non-token price counts as priced", func(t *testing.T) {
		t.Parallel()
		image := ModelPricing{Provider: ProviderBedrock, ModelID: "us.amazon.nova-canvas", ImageInputCost: 0.04}
		_, match, ok := newModelPricingIndex([]ModelPricing{bedrockPrice("amazon.nova-canvas", 1, 1), image}).
			Resolve(ProviderBedrock, "us.amazon.nova-canvas")
		if !ok || match != matchExact {
			t.Fatalf("match = %v, ok = %v, want the exact image-priced row", match, ok)
		}
	})
}

// An unpriced row must not hide a priced alternative that shares its key, in
// any row order: not an empty base row over a priced global row, nor an empty
// regional row over a priced one.
func TestPricingIndexUnpricedRowsDoNotHidePricedAlternatives(t *testing.T) {
	t.Parallel()

	t.Run("an empty base row does not hide the priced global row", func(t *testing.T) {
		t.Parallel()
		rows := []ModelPricing{
			bedrockPrice("anthropic.claude-sonnet-5", 0, 0),
			bedrockPrice("global.anthropic.claude-sonnet-5", 1.8, 9),
		}
		permutations(rows, func(order []ModelPricing) {
			got, match, ok := newModelPricingIndex(order).Resolve(ProviderBedrock, "au.anthropic.claude-sonnet-5")
			if !ok || match != matchBase || got.InputCostPerMillion != 1.8 {
				t.Fatalf("got %v (%v, ok=%v), want the global row 1.8, rows %v", got.InputCostPerMillion, match, ok, order)
			}
		})
	})

	t.Run("an empty regional row does not hide a priced one", func(t *testing.T) {
		t.Parallel()
		rows := []ModelPricing{
			bedrockPrice("eu.anthropic.claude-sonnet-5", 0, 0),
			bedrockPrice("us.anthropic.claude-sonnet-5", 2.2, 11),
		}
		permutations(rows, func(order []ModelPricing) {
			got, match, ok := newModelPricingIndex(order).Resolve(ProviderBedrock, "au.anthropic.claude-sonnet-5")
			if !ok || match != matchRegion || got.InputCostPerMillion != 2.2 {
				t.Fatalf("got %v (%v, ok=%v), want the us row 2.2, rows %v", got.InputCostPerMillion, match, ok, order)
			}
		})
	})

	t.Run("an empty exact row does not hide its priced version-suffixed twin", func(t *testing.T) {
		t.Parallel()
		rows := []ModelPricing{
			bedrockPrice("us.anthropic.claude-haiku-4-5", 0, 0),
			bedrockPrice("us.anthropic.claude-haiku-4-5-20251001-v1:0", 1.1, 5.5),
		}
		permutations(rows, func(order []ModelPricing) {
			got, match, ok := newModelPricingIndex(order).Resolve(ProviderBedrock, "us.anthropic.claude-haiku-4-5")
			if !ok || match != matchExact || got.InputCostPerMillion != 1.1 {
				t.Fatalf("got %v (%v, ok=%v), want the priced twin 1.1, rows %v", got.InputCostPerMillion, match, ok, order)
			}
		})
	})
}

// An admin override of 0 makes the model free. It is a price, so it must not
// fall through to a paid base row, in the runtime path and in the admin catalog.
func TestZeroOverrideIsAPriceNotAMissingOne(t *testing.T) {
	t.Parallel()
	zero := 0.0
	entries := []PricingCatalogEntry{
		{Provider: ProviderBedrock, ModelID: "anthropic.claude-haiku-4-5", SyncedInput: 1, SyncedOutput: 5},
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-haiku-4-5", SyncedInput: 1.1, SyncedOutput: 5.5, OverrideInput: &zero, OverrideOutput: &zero},
	}

	cache := NewDatabasePricingCache(&mockPricingCatalogRepository{loadAllResult: entries}, time.Minute, logging.NewLoggerWithOutput(&bytes.Buffer{}))
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, ok := cache.Price("bedrock:us", "us.anthropic.claude-haiku-4-5"); !ok || got.InputCostPerMillion != 0 || got.OutputCostPerMillion != 0 {
		t.Errorf("runtime Price = %v/%v (ok=%v), want the free override 0/0, not the paid base 1/5",
			got.InputCostPerMillion, got.OutputCostPerMillion, ok)
	}

	rows := []*pricingEntryJSON{toPricingEntryJSON(&entries[0]), toPricingEntryJSON(&entries[1])}
	if got := lookupCatalogPricing(newCatalogPricingIndex(rows), "bedrock:us", "us.anthropic.claude-haiku-4-5"); got == nil || got.ModelID != "us.anthropic.claude-haiku-4-5" {
		t.Errorf("admin catalog resolved %+v, want the regional row with its free override", got)
	}
}

// The admin catalog and /v1/models (and so billing) price one route from the
// issue's prod rows identically, whatever order the table loads in.
func TestCatalogAndRuntimePriceTheSameRoute(t *testing.T) {
	t.Parallel()
	entries := []PricingCatalogEntry{
		{Provider: ProviderBedrock, ModelID: "anthropic.claude-sonnet-5", SyncedInput: 2, SyncedOutput: 10},
		{Provider: ProviderBedrock, ModelID: "global.anthropic.claude-sonnet-5", SyncedInput: 2, SyncedOutput: 10},
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-5", SyncedInput: 2.2, SyncedOutput: 11},
		{Provider: ProviderBedrock, ModelID: "openai.gpt-6-sol", SyncedInput: 2.2, SyncedOutput: 11},
		{Provider: ProviderBedrock, ModelID: "us.openai.gpt-6-sol", SyncedInput: 2.2, SyncedOutput: 11},
		{Provider: ProviderBedrock, ModelID: "global.openai.gpt-6-sol", SyncedInput: 2, SyncedOutput: 10},
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-haiku-4-5", SyncedInput: 1.1, SyncedOutput: 5.5},
	}

	for _, tc := range []struct {
		route   VirtualModelRoute
		wantIn  float64
		wantOut float64
	}{
		{VirtualModelRoute{Provider: "bedrock:us", ModelID: "us.anthropic.claude-sonnet-5", Enabled: true}, 2.2, 11},
		{VirtualModelRoute{Provider: "bedrock:us", ModelID: "global.anthropic.claude-sonnet-5", Enabled: true}, 2, 10},
		{VirtualModelRoute{Provider: "bedrock:us", ModelID: "us.openai.gpt-6-sol", Enabled: true}, 2.2, 11},
		{VirtualModelRoute{Provider: "bedrock:us", ModelID: "global.openai.gpt-6-sol", Enabled: true}, 2, 10},
		// Routes with no row of their own, so each resolves through a fallback.
		{VirtualModelRoute{Provider: "bedrock:aus", ModelID: "au.anthropic.claude-sonnet-5", Enabled: true}, 2, 10},     // base
		{VirtualModelRoute{Provider: "bedrock:us", ModelID: "eu.openai.gpt-6-sol", Enabled: true}, 2.2, 11},             // base
		{VirtualModelRoute{Provider: "bedrock:aus", ModelID: "au.anthropic.claude-haiku-4-5", Enabled: true}, 1.1, 5.5}, // another region
	} {
		vm := VirtualModel{Routes: []VirtualModelRoute{tc.route}}
		connectors := map[ProviderName]ProviderConnector{"bedrock:us": nil, "bedrock:aus": nil}

		rotations(entries, func(order []PricingCatalogEntry) {
			cache := NewDatabasePricingCache(&mockPricingCatalogRepository{loadAllResult: order}, time.Minute, logging.NewLoggerWithOutput(&bytes.Buffer{}))
			if err := cache.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			rows := make([]*pricingEntryJSON, len(order))
			for i := range order {
				rows[i] = toPricingEntryJSON(&order[i])
			}

			runtime := vmEffectivePricing(vm, cache.Price, nil, connectors)
			admin := vmCatalogPricing(vm, newCatalogPricingIndex(rows))
			if runtime == nil || admin == nil {
				t.Fatalf("%s: runtime=%v admin=%v, want both priced", tc.route.ModelID, runtime, admin)
			}
			if *runtime.EffectiveInputCostPer1M != tc.wantIn || *runtime.EffectiveOutputCostPer1M != tc.wantOut {
				t.Fatalf("%s: runtime %v/%v, want %v/%v", tc.route.ModelID,
					*runtime.EffectiveInputCostPer1M, *runtime.EffectiveOutputCostPer1M, tc.wantIn, tc.wantOut)
			}
			if *admin.EffectiveInputCostPer1M != tc.wantIn || *admin.EffectiveOutputCostPer1M != tc.wantOut {
				t.Fatalf("%s: admin catalog %v/%v, want %v/%v", tc.route.ModelID,
					*admin.EffectiveInputCostPer1M, *admin.EffectiveOutputCostPer1M, tc.wantIn, tc.wantOut)
			}
		})
	}
}

// An override on the regional row prices that profile only. It is not copied to
// the base row, the global profile, or the profiles that fall back to base.
func TestOverrideAppliesToTheExactRowOnly(t *testing.T) {
	t.Parallel()
	in, out := 5.0, 25.0
	repo := &mockPricingCatalogRepository{loadAllResult: []PricingCatalogEntry{
		{Provider: ProviderBedrock, ModelID: "anthropic.claude-sonnet-5", SyncedInput: 2, SyncedOutput: 10},
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-5", SyncedInput: 2.2, SyncedOutput: 11, OverrideInput: &in, OverrideOutput: &out},
	}}
	cache := NewDatabasePricingCache(repo, time.Minute, logging.NewLoggerWithOutput(&bytes.Buffer{}))
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		modelID string
		wantIn  float64
	}{
		{"us.anthropic.claude-sonnet-5", 5},
		{"anthropic.claude-sonnet-5", 2},
		{"global.anthropic.claude-sonnet-5", 2}, // no row of its own: priced by the base row
	} {
		got, ok := cache.Price(ProviderBedrock, tc.modelID)
		if !ok || got.InputCostPerMillion != tc.wantIn {
			t.Errorf("Price(%q) = %v (ok=%v), want input %v", tc.modelID, got.InputCostPerMillion, ok, tc.wantIn)
		}
	}
}

// A regional route priced by its base row is billed below the real regional
// price, so it is logged. A hot route logs once, not once per request.
func TestRegionalProfileFallbackIsLoggedOnce(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	repo := &mockPricingCatalogRepository{loadAllResult: []PricingCatalogEntry{
		{Provider: ProviderBedrock, ModelID: "anthropic.claude-sonnet-5", SyncedInput: 2, SyncedOutput: 10},
		{Provider: ProviderBedrock, ModelID: "eu.anthropic.claude-sonnet-5", SyncedInput: 2.4, SyncedOutput: 12},
	}}
	cache := NewDatabasePricingCache(repo, time.Minute, logging.NewLoggerWithOutput(&out))
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	const event = "pricing.route_priced_by_fallback"

	for range 3 {
		cache.Price("bedrock:us", "us.anthropic.claude-sonnet-5") // no us. row: priced by base
	}
	if n := strings.Count(out.String(), event); n != 1 {
		t.Fatalf("fallback logged %d times for one route called 3 times, want 1:\n%s", n, out.String())
	}

	out.Reset()
	cache.Price("bedrock:us", "eu.anthropic.claude-sonnet-5") // exact row
	cache.Price("bedrock:us", "anthropic.claude-sonnet-5")    // base ID, not a regional profile
	if strings.Contains(out.String(), event) {
		t.Fatalf("exact and base-ID prices must not log a fallback:\n%s", out.String())
	}
}

// Price is called by every billed request, concurrently. A route priced by
// fallback is still logged exactly once.
func TestRegionalProfileFallbackLogsOnceUnderConcurrency(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	repo := &mockPricingCatalogRepository{loadAllResult: []PricingCatalogEntry{
		{Provider: ProviderBedrock, ModelID: "anthropic.claude-sonnet-5", SyncedInput: 2, SyncedOutput: 10},
	}}
	cache := NewDatabasePricingCache(repo, time.Minute, logging.NewLoggerWithOutput(&out))
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { cache.Price("bedrock:us", "us.anthropic.claude-sonnet-5") })
	}
	wg.Wait()

	if n := strings.Count(out.String(), "pricing.route_priced_by_fallback"); n != 1 {
		t.Fatalf("fallback logged %d times across 50 concurrent calls, want 1", n)
	}
}
