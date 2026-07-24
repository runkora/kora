package inferencegw

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Multi-account Bedrock (#72) introduced per-account provider aliases
// (bedrock:aus, bedrock:us, bedrock:usw) on virtual-model routes and usage
// records, while the pricing catalog is still keyed on the base "bedrock"
// provider. These tests lock in that every pricing key collapses the alias, so
// writers and readers land on the same key.

func TestPricingKeyCollapsesBedrockAliases(t *testing.T) {
	base := pricingKey(ProviderBedrock, "anthropic.claude-sonnet-5")

	for _, tc := range []struct {
		provider ProviderName
		modelID  string
	}{
		{"bedrock:aus", "au.anthropic.claude-sonnet-5"},
		{"bedrock:us", "us.anthropic.claude-sonnet-5"},
		{"bedrock:usw", "us.anthropic.claude-sonnet-5"},
		{ProviderBedrock, "anthropic.claude-sonnet-5"},
	} {
		if got := pricingKey(tc.provider, tc.modelID); got != base {
			t.Errorf("pricingKey(%q, %q) = %q, want %q", tc.provider, tc.modelID, got, base)
		}
	}

	// Non-Bedrock providers keep their own identity.
	if got := pricingKey(ProviderVertexAI, "gemini-2.0"); got != "vertexai/gemini-2.0" {
		t.Errorf("pricingKey(vertexai) = %q, want vertexai/gemini-2.0", got)
	}
}

// A pricing row is stored once under the base provider. Cost estimation looks it
// up with the route's aliased provider and a cross-region model ID; before the
// fix that missed entirely and the request was costed at zero.
func TestPricingCacheResolvesAliasedBedrockProvider(t *testing.T) {
	cache := NewStaticPricingCache([]ModelPricing{{
		Provider:             ProviderBedrock,
		ModelID:              "anthropic.claude-sonnet-5",
		InputCostPerMillion:  2,
		OutputCostPerMillion: 10,
	}})

	p, ok := cache.Price("bedrock:usw", "us.anthropic.claude-sonnet-5")
	if !ok {
		t.Fatal("aliased bedrock provider did not resolve pricing; usage would be costed at 0")
	}
	if p.InputCostPerMillion != 2 || p.OutputCostPerMillion != 10 {
		t.Errorf("pricing = %v/%v, want 2/10", p.InputCostPerMillion, p.OutputCostPerMillion)
	}
}

// Round-trip guard: DatabasePricingCache must WRITE (Refresh) and READ (Price)
// through the same key builder. The catalog row is stored under the base
// provider; the read arrives with a per-account alias and a cross-region model
// ID. Any asymmetry between the two silently costs usage at zero.
func TestDatabasePricingCacheKeyRoundTrip(t *testing.T) {
	repo := &mockPricingCatalogRepository{
		loadAllResult: []PricingCatalogEntry{{
			Provider:     ProviderBedrock,
			ModelID:      "anthropic.claude-sonnet-5",
			SyncedInput:  2,
			SyncedOutput: 10,
		}},
	}

	cache := NewDatabasePricingCache(repo, time.Minute, logging.NewLoggerWithOutput(io.Discard))
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	for _, tc := range []struct {
		provider ProviderName
		modelID  string
	}{
		{ProviderBedrock, "anthropic.claude-sonnet-5"},
		{"bedrock:aus", "au.anthropic.claude-sonnet-5"},
		{"bedrock:usw", "us.anthropic.claude-sonnet-5"},
	} {
		p, ok := cache.Price(tc.provider, tc.modelID)
		if !ok {
			t.Errorf("Price(%q, %q) missed; write/read keys are asymmetric", tc.provider, tc.modelID)
			continue
		}
		if p.InputCostPerMillion != 2 || p.OutputCostPerMillion != 10 {
			t.Errorf("Price(%q, %q) = %v/%v, want 2/10",
				tc.provider, tc.modelID, p.InputCostPerMillion, p.OutputCostPerMillion)
		}
	}
}

// GET /v1/models embeds effective pricing per virtual model. With an aliased
// route this returned nil, which the UI renders as "Unknown" for every model.
func TestVMEffectivePricingResolvesAliasedRoute(t *testing.T) {
	vm := VirtualModel{
		Family:  "anthropic",
		Name:    "claude-sonnet-5",
		Enabled: true,
		Routes: []VirtualModelRoute{{
			Provider: "bedrock:usw",
			ModelID:  "us.anthropic.claude-sonnet-5",
			Priority: 1,
			Enabled:  true,
		}},
	}

	idx := buildPricingIndex([]ModelPricing{{
		Provider:             ProviderBedrock,
		ModelID:              "anthropic.claude-sonnet-5",
		InputCostPerMillion:  2,
		OutputCostPerMillion: 10,
	}})

	connectors := map[ProviderName]ProviderConnector{
		"bedrock:usw": &stubConnector{name: "bedrock:usw"},
	}

	got := vmEffectivePricing(vm, idx, nil, connectors)
	if got == nil {
		t.Fatal("expected effective pricing for an aliased bedrock route, got nil (renders as \"Unknown\")")
	}
	if got.EffectiveInputCostPer1M != 2 || got.EffectiveOutputCostPer1M != 10 {
		t.Errorf("effective pricing = %+v, want 2/10", got)
	}
}
