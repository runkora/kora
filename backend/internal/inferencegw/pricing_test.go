package inferencegw

import (
	"testing"
)

func mkConnectors(providers ...ProviderName) map[ProviderName]ProviderConnector {
	m := make(map[ProviderName]ProviderConnector, len(providers))
	for _, p := range providers {
		m[p] = nil // presence-only; vmEffectivePricing only checks key existence
	}
	return m
}

// mkPricingIdx returns the price lookup of a cache holding entries.
func mkPricingIdx(entries ...ModelPricing) func(ProviderName, string) (ModelPricing, bool) {
	return NewStaticPricingCache(entries).Price
}

// ─── vmEffectivePricing ───────────────────────────────────────────────────────

func TestVmEffectivePricing(t *testing.T) {
	t.Parallel()

	bedrockConn := mkConnectors(ProviderBedrock)
	noop := map[string]bool{}

	tests := []struct {
		name           string
		vm             VirtualModel
		pricingIdx     func(ProviderName, string) (ModelPricing, bool)
		disabledRoutes map[string]bool
		connectors     map[ProviderName]ProviderConnector
		wantInput      float64
		wantOutput     float64
		wantNil        bool
	}{
		{
			name: "single available route returns its price",
			vm: VirtualModel{Routes: []VirtualModelRoute{
				{Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: true},
			}},
			pricingIdx:     mkPricingIdx(ModelPricing{Provider: ProviderBedrock, ModelID: "model-a", InputCostPerMillion: 3.0, OutputCostPerMillion: 15.0}),
			disabledRoutes: noop,
			connectors:     bedrockConn,
			wantInput:      3.0,
			wantOutput:     15.0,
		},
		{
			name: "lowest combined token cost wins, not lowest priority",
			vm: VirtualModel{Routes: []VirtualModelRoute{
				{Provider: ProviderBedrock, ModelID: "cheap", Priority: 2, Enabled: true},
				{Provider: ProviderBedrock, ModelID: "expensive", Priority: 1, Enabled: true},
			}},
			pricingIdx: mkPricingIdx(
				ModelPricing{Provider: ProviderBedrock, ModelID: "cheap", InputCostPerMillion: 0.5, OutputCostPerMillion: 0.5},
				ModelPricing{Provider: ProviderBedrock, ModelID: "expensive", InputCostPerMillion: 5.0, OutputCostPerMillion: 20.0},
			),
			disabledRoutes: noop,
			connectors:     bedrockConn,
			wantInput:      0.5,
			wantOutput:     0.5,
		},
		{
			name: "disabled admin toggle skips route; next by priority wins",
			vm: VirtualModel{Routes: []VirtualModelRoute{
				{Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: false},
				{Provider: ProviderBedrock, ModelID: "model-b", Priority: 2, Enabled: true},
			}},
			pricingIdx: mkPricingIdx(
				ModelPricing{Provider: ProviderBedrock, ModelID: "model-a", InputCostPerMillion: 1.0, OutputCostPerMillion: 1.0},
				ModelPricing{Provider: ProviderBedrock, ModelID: "model-b", InputCostPerMillion: 4.0, OutputCostPerMillion: 8.0},
			),
			disabledRoutes: noop,
			connectors:     bedrockConn,
			wantInput:      4.0,
			wantOutput:     8.0,
		},
		{
			name: "provider_model_override disables route; next by priority wins",
			vm: VirtualModel{Routes: []VirtualModelRoute{
				{Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: true},
				{Provider: ProviderBedrock, ModelID: "model-b", Priority: 2, Enabled: true},
			}},
			pricingIdx: mkPricingIdx(
				ModelPricing{Provider: ProviderBedrock, ModelID: "model-a", InputCostPerMillion: 1.0, OutputCostPerMillion: 1.0},
				ModelPricing{Provider: ProviderBedrock, ModelID: "model-b", InputCostPerMillion: 4.0, OutputCostPerMillion: 8.0},
			),
			disabledRoutes: map[string]bool{"bedrock/model-a": true},
			connectors:     bedrockConn,
			wantInput:      4.0,
			wantOutput:     8.0,
		},
		{
			name: "unconfigured connector skips route; next by priority wins",
			vm: VirtualModel{Routes: []VirtualModelRoute{
				{Provider: ProviderVertexAI, ModelID: "model-a", Priority: 1, Enabled: true},
				{Provider: ProviderBedrock, ModelID: "model-b", Priority: 2, Enabled: true},
			}},
			pricingIdx: mkPricingIdx(
				ModelPricing{Provider: ProviderVertexAI, ModelID: "model-a", InputCostPerMillion: 1.0, OutputCostPerMillion: 1.0},
				ModelPricing{Provider: ProviderBedrock, ModelID: "model-b", InputCostPerMillion: 4.0, OutputCostPerMillion: 8.0},
			),
			disabledRoutes: noop,
			connectors:     bedrockConn, // VertexAI not configured
			wantInput:      4.0,
			wantOutput:     8.0,
		},
		{
			name: "no matching pricing entry returns nil",
			vm: VirtualModel{Routes: []VirtualModelRoute{
				{Provider: ProviderBedrock, ModelID: "unknown", Priority: 1, Enabled: true},
			}},
			pricingIdx:     mkPricingIdx(),
			disabledRoutes: noop,
			connectors:     bedrockConn,
			wantNil:        true,
		},
		{
			name: "no enabled routes returns nil",
			vm: VirtualModel{Routes: []VirtualModelRoute{
				{Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: false},
			}},
			pricingIdx:     mkPricingIdx(ModelPricing{Provider: ProviderBedrock, ModelID: "model-a", InputCostPerMillion: 1.0}),
			disabledRoutes: noop,
			connectors:     bedrockConn,
			wantNil:        true,
		},
		{
			name:           "empty routes returns nil",
			vm:             VirtualModel{},
			pricingIdx:     mkPricingIdx(),
			disabledRoutes: noop,
			connectors:     bedrockConn,
			wantNil:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := vmEffectivePricing(tc.vm, tc.pricingIdx, tc.disabledRoutes, tc.connectors)
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.EffectiveInputCostPer1M == nil || *got.EffectiveInputCostPer1M != tc.wantInput {
				t.Errorf("input: want %v, got %v", tc.wantInput, got.EffectiveInputCostPer1M)
			}
			if got.EffectiveOutputCostPer1M == nil || *got.EffectiveOutputCostPer1M != tc.wantOutput {
				t.Errorf("output: want %v, got %v", tc.wantOutput, got.EffectiveOutputCostPer1M)
			}
		})
	}
}

// OpenAI reports prompt_tokens inclusive of prompt_tokens_details.cached_tokens;
// Anthropic reports input_tokens with the cache buckets held separately. The cost
// function prices disjoint buckets, so the OpenAI side has to subtract first or
// the cached tokens are charged at both the full and the cached rate.
// See https://github.com/clear-route/orbit/issues/262.
func TestDisjointPromptTokens(t *testing.T) {
	tests := []struct {
		name         string
		promptTokens int
		cacheRead    int
		cacheWrite   int
		want         int
	}{
		{"no cache", 1000, 0, 0, 1000},
		{"partly cached", 1000, 400, 0, 600},
		{"entirely cached", 1000, 1000, 0, 0},
		{"cache write turn", 1000, 0, 900, 100},
		{"both buckets", 1000, 400, 300, 300},
		// A dialect that cannot report writes leaves them in the prompt.
		{"write count unavailable", 1000, 400, 0, 600},
		// Nonsense from a provider must not become a negative charge.
		{"more cached than prompt", 400, 1000, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := disjointPromptTokens(tc.promptTokens, tc.cacheRead, tc.cacheWrite); got != tc.want {
				t.Errorf("disjointPromptTokens(%d, %d, %d) = %d, want %d", tc.promptTokens, tc.cacheRead, tc.cacheWrite, got, tc.want)
			}
		})
	}
}

// The same prompt costs the same whichever dialect reported it.
func TestEstimatedCost_DialectsAgree(t *testing.T) {
	pricing := resolvedPricing{inputCostPer1M: 3, outputCostPer1M: 15, cacheReadCostPer1M: 0.3}

	// 1000 prompt tokens, 400 of them served from cache, 500 completion.
	// OpenAI reports prompt=1000 (inclusive); Anthropic reports input=600 + cache=400.
	openAI := pricing.estimatedCostUSD(disjointPromptTokens(1000, 400, 0), 500, 400, 0)
	anthropic := pricing.estimatedCostUSD(600, 500, 400, 0)

	if openAI != anthropic {
		t.Errorf("OpenAI dialect billed %v, Anthropic billed %v for the same prompt", openAI, anthropic)
	}

	// Priced from the disjoint buckets: 600 full + 400 cached + 500 output.
	want := (600.0/1_000_000)*3 + (400.0/1_000_000)*0.3 + (500.0/1_000_000)*15
	if openAI != want {
		t.Errorf("cost = %v, want %v", openAI, want)
	}
}

// A Claude stream served over the OpenAI dialect must record the same totals as
// the same traffic served natively, whichever cache buckets it used.
func TestDialectsAgreeOnTotals(t *testing.T) {
	tests := []struct {
		name                            string
		uncached, cacheRead, cacheWrite int
	}{
		{"no cache", 1000, 0, 0},
		{"cache read turn", 100, 900, 0},
		{"cache write turn", 100, 0, 900},
		{"both buckets", 100, 500, 400},
	}

	const completion = 50
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// What the OpenAI-dialect conversion puts on the wire.
			promptTokens := tc.uncached + tc.cacheRead + tc.cacheWrite
			wireTotal := promptTokens + completion

			anthropicTotal := tc.uncached + tc.cacheRead + tc.cacheWrite + completion
			if wireTotal != anthropicTotal {
				t.Errorf("OpenAI-dialect total %d disagrees with the Anthropic-dialect total %d", wireTotal, anthropicTotal)
			}

			// And the buckets handed to pricing must still add back up to the prompt.
			billed := disjointPromptTokens(promptTokens, tc.cacheRead, tc.cacheWrite)
			if billed+tc.cacheRead+tc.cacheWrite != promptTokens {
				t.Errorf("billed buckets %d+%d+%d do not reconstruct prompt_tokens %d", billed, tc.cacheRead, tc.cacheWrite, promptTokens)
			}
		})
	}
}

// A bridged dialect reports the prompt inclusive of both cache buckets and hands
// the counts over separately. Billed from those, it must cost exactly what the
// same traffic costs on the native Anthropic path, where the buckets arrive
// already disjoint. Passing 0 for the write count bills it as plain input and
// this test fails by the 0.25x difference.
func TestEstimatedCost_CacheWriteMatchesNativeDialect(t *testing.T) {
	pricing := resolvedPricing{
		inputCostPer1M:      3,
		outputCostPer1M:     15,
		cacheReadCostPer1M:  0.3,
		cacheWriteCostPer1M: 3.75,
	}

	tests := []struct {
		name                            string
		uncached, cacheRead, cacheWrite int
	}{
		{"cache write turn", 2, 0, 11246},
		{"cache read turn", 2, 11246, 0},
		{"both buckets", 100, 500, 400},
	}

	const completion = 4
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			promptTokens := tc.uncached + tc.cacheRead + tc.cacheWrite

			bridged := pricing.estimatedCostUSD(
				disjointPromptTokens(promptTokens, tc.cacheRead, tc.cacheWrite),
				completion, tc.cacheRead, tc.cacheWrite)
			native := pricing.estimatedCostUSD(tc.uncached, completion, tc.cacheRead, tc.cacheWrite)

			if bridged != native {
				t.Errorf("bridged dialect billed %v, native Anthropic billed %v", bridged, native)
			}
		})
	}
}
