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

	got := vmEffectivePricing(vm, idx, nil, nil, connectors)
	if got == nil {
		t.Fatal("expected effective pricing for an aliased bedrock route, got nil (renders as \"Unknown\")")
	}
	if got.EffectiveInputCostPer1M != 2 || got.EffectiveOutputCostPer1M != 10 {
		t.Errorf("effective pricing = %+v, want 2/10", got)
	}
}

func TestPricingBareKeyStripsVendorAndRegion(t *testing.T) {
	// A Bedrock route and the Azure catalog row for the same model must collapse
	// to the same bare key so the cross-provider fallback can match them.
	want := "gpt-5.4"
	for _, tc := range []struct {
		provider ProviderName
		modelID  string
	}{
		{"bedrock:us", "openai.gpt-5.4"}, // route: vendor-namespaced
		{"azure", "gpt-5.4"},             // catalog: bare
		{"azure", "us/gpt-5.4"},          // catalog: regional
	} {
		if got := pricingBareKey(tc.provider, tc.modelID); got != want {
			t.Errorf("pricingBareKey(%q, %q) = %q, want %q", tc.provider, tc.modelID, got, want)
		}
	}
	// Non-Bedrock provider: the "." inside "gpt-5.4" must NOT be treated as a
	// vendor separator.
	if got := pricingBareKey("azure", "gpt-5.4"); got != "gpt-5.4" {
		t.Errorf("pricingBareKey(azure, gpt-5.4) = %q, want gpt-5.4", got)
	}
}

// The headline case: gpt-5.x is served on Bedrock but LiteLLM prices it only
// under Azure. With no bedrock/openai.gpt-5.4 row, the exact lookup misses and
// the model showed "Unknown"; the bare-key fallback borrows the Azure price.
func TestVMEffectivePricingCrossProviderFallback(t *testing.T) {
	vm := VirtualModel{
		Family:  "openai",
		Name:    "gpt-5-4",
		Enabled: true,
		Routes: []VirtualModelRoute{{
			Provider: "bedrock:us",
			ModelID:  "openai.gpt-5.4",
			Priority: 1,
			Enabled:  true,
		}},
	}

	// Only an Azure row exists — no bedrock row for gpt-5.4.
	pricing := []ModelPricing{
		{Provider: "azure", ModelID: "gpt-5.4", InputCostPerMillion: 2.5, OutputCostPerMillion: 15},
		{Provider: "azure", ModelID: "us/gpt-5.4", InputCostPerMillion: 2.75, OutputCostPerMillion: 16.5},
	}
	idx := buildPricingIndex(pricing)
	bareIdx := bareFallbackIndex(pricing,
		func(p ModelPricing) ProviderName { return p.Provider },
		func(p ModelPricing) string { return p.ModelID })

	connectors := map[ProviderName]ProviderConnector{"bedrock:us": &stubConnector{name: "bedrock:us"}}

	got := vmEffectivePricing(vm, idx, bareIdx, nil, connectors)
	if got == nil {
		t.Fatal("expected cross-provider fallback pricing for gpt-5.4 on bedrock, got nil (Unknown)")
	}
	// Plainest (shortest) model ID wins: bare "gpt-5.4" over regional "us/gpt-5.4".
	if got.EffectiveInputCostPer1M != 2.5 || got.EffectiveOutputCostPer1M != 15 {
		t.Errorf("fallback pricing = %+v, want 2.5/15 (the bare azure row)", got)
	}

	// An exact same-provider row must still win over the fallback.
	withExact := append(pricing, ModelPricing{
		Provider: ProviderBedrock, ModelID: "openai.gpt-5.4", InputCostPerMillion: 3, OutputCostPerMillion: 20,
	})
	got = vmEffectivePricing(vm,
		buildPricingIndex(withExact),
		bareFallbackIndex(withExact,
			func(p ModelPricing) ProviderName { return p.Provider },
			func(p ModelPricing) string { return p.ModelID }),
		nil, connectors)
	if got == nil || got.EffectiveInputCostPer1M != 3 {
		t.Errorf("exact bedrock row should win over fallback; got %+v, want 3/20", got)
	}
}
