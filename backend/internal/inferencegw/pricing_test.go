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

func mkPricingIdx(entries ...ModelPricing) map[string]ModelPricing {
	m := make(map[string]ModelPricing, len(entries))
	for _, e := range entries {
		m[string(e.Provider)+"/"+e.ModelID] = e
	}
	return m
}

// ─── vmEffectivePricing ───────────────────────────────────────────────────────

func TestVmEffectivePricing(t *testing.T) {
	t.Parallel()

	bedrockConn := mkConnectors(ProviderBedrock)
	noop := map[string]bool{}

	tests := []struct {
		name           string
		vm             VirtualModel
		pricingIdx     map[string]ModelPricing
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
			name: "lowest Priority route wins, not cheapest",
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
			wantInput:      5.0,
			wantOutput:     20.0,
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
			if got.EffectiveInputCostPer1M != tc.wantInput {
				t.Errorf("input: want %v, got %v", tc.wantInput, got.EffectiveInputCostPer1M)
			}
			if got.EffectiveOutputCostPer1M != tc.wantOutput {
				t.Errorf("output: want %v, got %v", tc.wantOutput, got.EffectiveOutputCostPer1M)
			}
		})
	}
}
