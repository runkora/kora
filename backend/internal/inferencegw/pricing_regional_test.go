package inferencegw

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func regionalRows() []PricingCatalogEntry {
	row := func(id string, in, out float64) PricingCatalogEntry {
		return PricingCatalogEntry{Provider: ProviderBedrock, ModelID: id, SyncedInput: in, SyncedOutput: out}
	}
	return []PricingCatalogEntry{
		row("anthropic.claude-sonnet-5", 2, 10),
		row("global.anthropic.claude-sonnet-5", 2, 10),
		row("us.anthropic.claude-sonnet-5", 2.2, 11),
		row("au.anthropic.claude-sonnet-5", 2.3, 12),
	}
}

func permutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, p := range permutations(n - 1) {
		for i := 0; i <= len(p); i++ {
			q := append(append(append([]int{}, p[:i]...), n-1), p[i:]...)
			out = append(out, q)
		}
	}
	return out
}

func TestPricing_RegionalRouteResolvesOwnRowRegardlessOfLoadOrder(t *testing.T) {
	t.Parallel()
	rows := regionalRows()
	want := map[string]float64{
		"anthropic.claude-sonnet-5":        2,
		"global.anthropic.claude-sonnet-5": 2,
		"us.anthropic.claude-sonnet-5":     2.2,
		"au.anthropic.claude-sonnet-5":     2.3,
		// No row of its own: falls back to the base row.
		"eu.anthropic.claude-sonnet-5": 2,
	}
	for _, perm := range permutations(len(rows)) {
		ordered := make([]PricingCatalogEntry, len(rows))
		for i, j := range perm {
			ordered[i] = rows[j]
		}
		cache := NewDatabasePricingCache(&mockPricingCatalogRepository{loadAllResult: ordered}, time.Minute, logging.NewLoggerWithOutput(io.Discard))
		if err := cache.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		pm := make(map[string]*pricingEntryJSON, len(ordered))
		for i := range ordered {
			pm[string(ordered[i].Provider)+"/"+ordered[i].ModelID] = toPricingEntryJSON(&ordered[i])
		}
		for id, in := range want {
			got, ok := cache.Price(ProviderBedrock, id)
			if !ok || got.InputCostPerMillion != in {
				t.Errorf("order %v: runtime %s = %v (ok=%v), want %v", perm, id, got.InputCostPerMillion, ok, in)
			}
			if a := lookupPricing(pm, ProviderBedrock, id); a == nil || a.EffectiveInput != in {
				t.Errorf("order %v: admin %s differs from runtime price %v", perm, id, in)
			}
		}
		if n := len(cache.All()); n != len(rows) {
			t.Errorf("All() = %d rows, want %d", n, len(rows))
		}
	}
}
