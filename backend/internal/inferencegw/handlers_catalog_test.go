package inferencegw

import (
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/policy"
)

// ─── validateVirtualModelRequest ─────────────────────────────────────────────

func TestValidateVirtualModelRequest_Valid(t *testing.T) {
	t.Parallel()
	req := virtualModelRequest{
		Family:      "anthropic",
		Name:        "claude-sonnet-4-6",
		DisplayName: "Claude Sonnet 4.6",
		Strategy:    "priority",
		Enabled:     true,
		Routes: []routeInput{
			{Provider: "bedrock:test", ModelID: "us.anthropic.claude-sonnet-4-6-v1:0", Priority: 1, Enabled: true},
		},
	}
	if err := validateVirtualModelRequest(req); err != nil {
		t.Errorf("expected no error for valid request, got: %v", err)
	}
}

func TestValidateVirtualModelRequest_MissingFields(t *testing.T) {
	t.Parallel()
	base := virtualModelRequest{
		Family:      "anthropic",
		Name:        "claude-sonnet-4-6",
		DisplayName: "Claude Sonnet 4.6",
		Strategy:    "priority",
	}

	cases := []struct {
		name    string
		mutate  func(*virtualModelRequest)
		wantMsg string
	}{
		{
			name:    "missing family",
			mutate:  func(r *virtualModelRequest) { r.Family = "" },
			wantMsg: "family is required",
		},
		{
			name:    "missing name",
			mutate:  func(r *virtualModelRequest) { r.Name = "" },
			wantMsg: "name is required",
		},
		{
			name:    "missing display_name",
			mutate:  func(r *virtualModelRequest) { r.DisplayName = "" },
			wantMsg: "display_name is required",
		},
		{
			name:    "invalid strategy",
			mutate:  func(r *virtualModelRequest) { r.Strategy = "round_robin" },
			wantMsg: "strategy must be one of",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := base
			tc.mutate(&req)
			err := validateVirtualModelRequest(req)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

func TestValidateVirtualModelRequest_InvalidScopeChars(t *testing.T) {
	t.Parallel()
	base := virtualModelRequest{
		Family:      "anthropic",
		Name:        "claude-sonnet-4-6",
		DisplayName: "Claude Sonnet 4.6",
		Strategy:    "priority",
	}

	cases := []struct {
		name     string
		mutate   func(*virtualModelRequest)
		wantMsgs []string
	}{
		{
			name:     "dot in name (issue 207 repro)",
			mutate:   func(r *virtualModelRequest) { r.Name = "uk.claude-sonnet-4-6" },
			wantMsgs: []string{`name "uk.claude-sonnet-4-6"`, `"."`, "must match"},
		},
		{
			name:     "slash in name",
			mutate:   func(r *virtualModelRequest) { r.Name = "foo/bar" },
			wantMsgs: []string{`"/"`, "must match"},
		},
		{
			name:     "uppercase in name",
			mutate:   func(r *virtualModelRequest) { r.Name = "Claude" },
			wantMsgs: []string{`"C"`, "must match"},
		},
		{
			name:     "space in name",
			mutate:   func(r *virtualModelRequest) { r.Name = "foo bar" },
			wantMsgs: []string{`" "`, "must match"},
		},
		{
			// Locks in that regex-based rejection reports whole runes, not
			// bytes, for multi-byte UTF-8 input.
			name:     "multi-byte rune in name",
			mutate:   func(r *virtualModelRequest) { r.Name = "café" },
			wantMsgs: []string{`name "café"`, `"é"`, "must match"},
		},
		{
			name:     "dot in family",
			mutate:   func(r *virtualModelRequest) { r.Family = "an.thropic" },
			wantMsgs: []string{`family "an.thropic"`, `"."`, "must match"},
		},
		{
			name:     "colon in family",
			mutate:   func(r *virtualModelRequest) { r.Family = "an:thropic" },
			wantMsgs: []string{`family "an:thropic"`, `":"`, "must match"},
		},
		{
			name:     "colon in name",
			mutate:   func(r *virtualModelRequest) { r.Name = "claude:sonnet" },
			wantMsgs: []string{`name "claude:sonnet"`, `":"`, "must match"},
		},
		{
			name:     "name over 255 chars",
			mutate:   func(r *virtualModelRequest) { r.Name = strings.Repeat("a", 256) },
			wantMsgs: []string{`name "`, "must be 1–255 characters", policy.ScopeSegmentGrammar},
		},
		{
			name:     "family over 255 chars",
			mutate:   func(r *virtualModelRequest) { r.Family = strings.Repeat("a", 256) },
			wantMsgs: []string{`family "`, "must be 1–255 characters", policy.ScopeSegmentGrammar},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := base
			tc.mutate(&req)
			err := validateVirtualModelRequest(req)
			if err == nil {
				t.Fatalf("expected error containing %v, got nil", tc.wantMsgs)
			}
			for _, want := range tc.wantMsgs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should contain %q", err.Error(), want)
				}
			}
		})
	}
}

func TestValidateVirtualModelRequest_RouteValidation(t *testing.T) {
	t.Parallel()
	base := virtualModelRequest{
		Family:      "anthropic",
		Name:        "claude-sonnet-4-6",
		DisplayName: "Claude Sonnet 4.6",
		Strategy:    "priority",
	}

	cases := []struct {
		name    string
		route   routeInput
		wantMsg string
	}{
		{
			name:    "invalid provider",
			route:   routeInput{Provider: "mistral", ModelID: "gpt-4", Priority: 1},
			wantMsg: `route[0].provider "mistral" is not valid`,
		},
		{
			name:    "missing model_id",
			route:   routeInput{Provider: "bedrock:test", ModelID: "", Priority: 1},
			wantMsg: "route[0].model_id is required",
		},
		{
			name:    "priority zero",
			route:   routeInput{Provider: "bedrock:test", ModelID: "some-model", Priority: 0},
			wantMsg: "route[0].priority must be ≥ 1",
		},
		{
			name:    "priority negative",
			route:   routeInput{Provider: "vertexai:test", ModelID: "some-model", Priority: -1},
			wantMsg: "route[0].priority must be ≥ 1",
		},
		{
			name:    "weight above 100",
			route:   routeInput{Provider: "bedrock:test", ModelID: "some-model", Priority: 1, Weight: 101},
			wantMsg: "route[0].weight must be between 0 and 100",
		},
		{
			name:    "weight negative",
			route:   routeInput{Provider: "bedrock:test", ModelID: "some-model", Priority: 1, Weight: -1},
			wantMsg: "route[0].weight must be between 0 and 100",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := base
			req.Routes = []routeInput{tc.route}
			err := validateVirtualModelRequest(req)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

func TestValidateVirtualModelRequest_WeightedStrategy(t *testing.T) {
	t.Parallel()
	base := virtualModelRequest{
		Family:      "anthropic",
		Name:        "claude-sonnet-4-6",
		DisplayName: "Claude Sonnet 4.6",
		Strategy:    "weighted",
	}

	t.Run("weights sum to 100", func(t *testing.T) {
		t.Parallel()
		req := base
		req.Routes = []routeInput{
			{Provider: "bedrock:test", ModelID: "model-a", Priority: 1, Weight: 60, Enabled: true},
			{Provider: "vertexai:test", ModelID: "model-b", Priority: 2, Weight: 40, Enabled: true},
		}
		if err := validateVirtualModelRequest(req); err != nil {
			t.Errorf("expected no error when weights sum to 100, got: %v", err)
		}
	})

	t.Run("weights do not sum to 100", func(t *testing.T) {
		t.Parallel()
		req := base
		req.Routes = []routeInput{
			{Provider: "bedrock:test", ModelID: "model-a", Priority: 1, Weight: 60, Enabled: true},
			{Provider: "vertexai:test", ModelID: "model-b", Priority: 2, Weight: 30, Enabled: true},
		}
		err := validateVirtualModelRequest(req)
		if err == nil {
			t.Fatal("expected error for weights summing to 90, got nil")
		}
		if !strings.Contains(err.Error(), "weights must sum to 100") {
			t.Errorf("error %q should mention weights summing to 100", err.Error())
		}
		if !strings.Contains(err.Error(), "90") {
			t.Errorf("error %q should include the actual total (90)", err.Error())
		}
	})

	t.Run("no routes skips weight check", func(t *testing.T) {
		t.Parallel()
		req := base
		req.Routes = nil
		if err := validateVirtualModelRequest(req); err != nil {
			t.Errorf("expected no error for weighted strategy with no routes, got: %v", err)
		}
	})

	t.Run("disabled routes excluded from weight sum", func(t *testing.T) {
		t.Parallel()
		req := base
		// Enabled routes sum to 100; disabled route's weight is irrelevant.
		req.Routes = []routeInput{
			{Provider: "bedrock:test", ModelID: "model-a", Priority: 1, Weight: 100, Enabled: true},
			{Provider: "vertexai:test", ModelID: "model-b", Priority: 2, Weight: 50, Enabled: false},
		}
		if err := validateVirtualModelRequest(req); err != nil {
			t.Errorf("expected no error when enabled weights sum to 100 (disabled routes ignored), got: %v", err)
		}
	})
}

func TestValidateVirtualModelRequest_AllStrategiesAccepted(t *testing.T) {
	t.Parallel()
	for _, strategy := range []string{"priority", "weighted", "lowest_cost"} {
		t.Run(strategy, func(t *testing.T) {
			t.Parallel()
			req := virtualModelRequest{
				Family:      "anthropic",
				Name:        "claude-sonnet-4-6",
				DisplayName: "Claude Sonnet 4.6",
				Strategy:    strategy,
			}
			if err := validateVirtualModelRequest(req); err != nil {
				t.Errorf("strategy %q should be valid, got: %v", strategy, err)
			}
		})
	}
}

// ─── ProviderName.Valid ─────────────────────────────────────────────────────────

func TestProviderNameValid(t *testing.T) {
	t.Parallel()
	valid := []ProviderName{"bedrock:us", "bedrock:prod_east",
		"compatible:openrouter", "vertexai:gcp-prod", "azure:eastus"}
	for _, p := range valid {
		if !p.Valid() {
			t.Errorf("Valid(%q) = false, want true", p)
		}
	}
	invalid := []ProviderName{"mistral", "gcp:x", "", "BEDROCK", "compatible:", "compatible:My-Router", "compatible:a/b", "openai:x", ProviderBedrock, ProviderVertexAI, ProviderAzure, ProviderCompatible, "bedrock:", "bedrock:-east", "bedrock:Prod"}
	for _, p := range invalid {
		if p.Valid() {
			t.Errorf("Valid(%q) = true, want false", p)
		}
	}
}

// ─── toVirtualModelRoutes ────────────────────────────────────────────────────

func TestToVirtualModelRoutes_MapsFields(t *testing.T) {
	t.Parallel()
	vmID := "vm-123"
	inputs := []routeInput{
		{
			ID:       "route-1",
			Provider: "bedrock",
			ModelID:  "us.anthropic.claude-sonnet-4-6-v1:0",
			Priority: 1,
			Weight:   60,
			Enabled:  true,
		},
		{
			ID:       "",
			Provider: "vertexai",
			ModelID:  "claude-sonnet-4-6@20250514",
			Priority: 2,
			Weight:   40,
			Enabled:  false,
		},
	}

	routes := toVirtualModelRoutes(vmID, inputs)

	if len(routes) != 2 {
		t.Fatalf("len(routes) = %d, want 2", len(routes))
	}

	r0 := routes[0]
	if r0.ID != "route-1" {
		t.Errorf("ID = %q, want route-1 (existing ID preserved)", r0.ID)
	}
	if r0.VirtualModelID != vmID {
		t.Errorf("VirtualModelID = %q, want %q", r0.VirtualModelID, vmID)
	}
	if r0.Provider != ProviderBedrock {
		t.Errorf("Provider = %q, want bedrock", r0.Provider)
	}
	if !r0.Enabled {
		t.Error("Enabled should be true for route 0")
	}

	r1 := routes[1]
	if r1.ID != "" {
		t.Errorf("ID = %q, want empty (caller assigns UUID for new routes)", r1.ID)
	}
	if r1.VirtualModelID != vmID {
		t.Errorf("VirtualModelID = %q, want %q", r1.VirtualModelID, vmID)
	}
	if r1.Enabled {
		t.Error("Enabled should be false for route 1")
	}
}

func TestToVirtualModelRoutes_Empty(t *testing.T) {
	t.Parallel()
	routes := toVirtualModelRoutes("vm-1", nil)
	if len(routes) != 0 {
		t.Errorf("len(routes) = %d, want 0 for nil input", len(routes))
	}
}

// ─── toPricingEntryJSON ───────────────────────────────────────────────────────

func TestToPricingEntryJSON_TokenFields(t *testing.T) {
	t.Parallel()
	v := 2.0
	input := &v
	e := &PricingCatalogEntry{
		Provider:       ProviderBedrock,
		ModelID:        "anthropic.claude-sonnet-4-6",
		SyncedInput:    3.0,
		SyncedOutput:   15.0,
		OverrideInput:  input,
		OverrideOutput: nil,
	}
	got := toPricingEntryJSON(e)
	if got.EffectiveInput != 2.0 {
		t.Errorf("EffectiveInput = %v, want 2.0 (override applied)", got.EffectiveInput)
	}
	if got.EffectiveOutput != 15.0 {
		t.Errorf("EffectiveOutput = %v, want 15.0 (synced fallback)", got.EffectiveOutput)
	}
	if !got.HasOverride {
		t.Errorf("HasOverride = false, want true (input override set)")
	}
}

func TestToPricingEntryJSON_NonTokenFields(t *testing.T) {
	t.Parallel()
	e := &PricingCatalogEntry{
		Provider:                      ProviderBedrock,
		ModelID:                       "image-model",
		SyncedImageInputCostPerImage:  0.001,
		SyncedImageOutputCostPerImage: 0.002,
		SyncedRequestInputCost:        0.005,
		SyncedRequestOutputCost:       0.006,
		SyncedSecondInputCost:         0.0001,
		SyncedSecondOutputCost:        0.0002,
		SyncedAudioInputCostPer1M:     1.5,
		SyncedAudioOutputCostPer1M:    3.0,
	}
	got := toPricingEntryJSON(e)
	if got.SyncedImageInputCostPerImage != 0.001 {
		t.Errorf("SyncedImageInputCostPerImage = %v, want 0.001", got.SyncedImageInputCostPerImage)
	}
	if got.SyncedAudioInputCostPer1M != 1.5 {
		t.Errorf("SyncedAudioInputCostPer1M = %v, want 1.5", got.SyncedAudioInputCostPer1M)
	}
	if got.SyncedAudioOutputCostPer1M != 3.0 {
		t.Errorf("SyncedAudioOutputCostPer1M = %v, want 3.0", got.SyncedAudioOutputCostPer1M)
	}
	if got.HasOverride {
		t.Errorf("HasOverride = true, want false (no overrides set)")
	}
}

func TestToPricingEntryJSON_NoOverride_EffectiveEqualsSynced(t *testing.T) {
	t.Parallel()
	e := &PricingCatalogEntry{
		Provider:     ProviderVertexAI,
		ModelID:      "gemini-2.0-flash",
		SyncedInput:  5.0,
		SyncedOutput: 20.0,
	}
	got := toPricingEntryJSON(e)
	if got.EffectiveInput != 5.0 {
		t.Errorf("EffectiveInput = %v, want 5.0", got.EffectiveInput)
	}
	if got.EffectiveOutput != 20.0 {
		t.Errorf("EffectiveOutput = %v, want 20.0", got.EffectiveOutput)
	}
	if got.HasOverride {
		t.Errorf("HasOverride = true, want false")
	}
}

// ─── vmCatalogPricing ─────────────────────────────────────────────────────────

func TestVmCatalogPricing_NoRoutes(t *testing.T) {
	t.Parallel()
	if got := vmCatalogPricing(VirtualModel{}, nil); got != nil {
		t.Errorf("expected nil for empty VM, got %+v", got)
	}
}

func TestVmCatalogPricing_NoPricingData(t *testing.T) {
	t.Parallel()
	vm := VirtualModel{
		Routes: []VirtualModelRoute{
			{Provider: ProviderBedrock, ModelID: "anthropic.claude-sonnet-4-6", Enabled: true},
		},
	}
	if got := vmCatalogPricing(vm, map[string]*pricingEntryJSON{}); got != nil {
		t.Errorf("expected nil when pricing map is empty, got %+v", got)
	}
}

func TestVmCatalogPricing_NonTokenFallback(t *testing.T) {
	t.Parallel()
	vm := VirtualModel{
		Routes: []VirtualModelRoute{
			{Provider: ProviderBedrock, ModelID: "image-model", Enabled: true},
		},
	}
	pricingMap := map[string]*pricingEntryJSON{
		"bedrock/image-model": {
			Provider:                     "bedrock",
			ModelID:                      "image-model",
			SyncedImageInputCostPerImage: 0.001, // non-token only — no effective token costs
		},
	}
	got := vmCatalogPricing(vm, pricingMap)
	if got == nil {
		t.Fatal("expected non-nil: non-token fallback should fire when no token-priced route exists")
	}
	if !got.NonTokenOnly {
		t.Errorf("NonTokenOnly = false, want true")
	}

	// A client reading the wire response, not the Go struct, must not see the
	// two token fields at all: the model has no token price, and 0 reads as
	// "free" rather than "not applicable".
	if got.EffectiveInputCostPer1M != nil {
		t.Errorf("EffectiveInputCostPer1M = %v, want nil (omitted from the wire)", *got.EffectiveInputCostPer1M)
	}
	if got.EffectiveOutputCostPer1M != nil {
		t.Errorf("EffectiveOutputCostPer1M = %v, want nil (omitted from the wire)", *got.EffectiveOutputCostPer1M)
	}

	// The whole point of the fallback: the route's own non-token dimensions
	// reach the client, or "see model details" in the UI has nothing to show.
	if got.ImageInputCost != 0.001 {
		t.Errorf("ImageInputCost = %v, want 0.001 (propagated from the route)", got.ImageInputCost)
	}
}

// The best-branch (token-priced) response must also omit the two fields via a
// pointer, matching modelObjectPricing's contract on /v1/models: present and
// nullable, not present-and-zero.
func TestVmCatalogPricing_TokenPricedRouteUsesPointers(t *testing.T) {
	t.Parallel()
	vm := VirtualModel{
		Routes: []VirtualModelRoute{
			{Provider: ProviderBedrock, ModelID: "claude", Enabled: true},
		},
	}
	pricingMap := map[string]*pricingEntryJSON{
		"bedrock/claude": {
			Provider:        "bedrock",
			ModelID:         "claude",
			EffectiveInput:  3,
			EffectiveOutput: 15,
		},
	}
	got := vmCatalogPricing(vm, pricingMap)
	if got == nil {
		t.Fatal("expected non-nil pricing")
	}
	if got.EffectiveInputCostPer1M == nil || *got.EffectiveInputCostPer1M != 3 {
		t.Errorf("EffectiveInputCostPer1M = %v, want *3", got.EffectiveInputCostPer1M)
	}
	if got.EffectiveOutputCostPer1M == nil || *got.EffectiveOutputCostPer1M != 15 {
		t.Errorf("EffectiveOutputCostPer1M = %v, want *15", got.EffectiveOutputCostPer1M)
	}
}

func TestVmCatalogPricing_NilWhenNeitherPricing(t *testing.T) {
	t.Parallel()
	vm := VirtualModel{
		Routes: []VirtualModelRoute{
			{Provider: ProviderBedrock, ModelID: "free-model", Enabled: true},
		},
	}
	// Model exists but has no token or non-token costs.
	pricingMap := map[string]*pricingEntryJSON{
		"bedrock/free-model": {Provider: "bedrock:test", ModelID: "free-model"},
	}
	if got := vmCatalogPricing(vm, pricingMap); got != nil {
		t.Errorf("expected nil when no token or non-token pricing, got %+v", got)
	}
}

// ─── pricingEntryHasNonToken ──────────────────────────────────────────────────

func TestPricingEntryHasNonToken(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		p    *pricingEntryJSON
		want bool
	}{
		{"all zero", &pricingEntryJSON{}, false},
		{"image input", &pricingEntryJSON{SyncedImageInputCostPerImage: 0.001}, true},
		{"image output", &pricingEntryJSON{SyncedImageOutputCostPerImage: 0.002}, true},
		{"request input", &pricingEntryJSON{SyncedRequestInputCost: 0.005}, true},
		{"second input", &pricingEntryJSON{SyncedSecondInputCost: 0.0001}, true},
		{"audio input", &pricingEntryJSON{SyncedAudioInputCostPer1M: 1.0}, true},
		{"token only — no non-token", &pricingEntryJSON{EffectiveInput: 3.0, EffectiveOutput: 15.0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pricingEntryHasNonToken(tc.p); got != tc.want {
				t.Errorf("pricingEntryHasNonToken = %v, want %v", got, tc.want)
			}
		})
	}
}

// ─── lookupPricing ────────────────────────────────────────────────────────────

func TestLookupPricing(t *testing.T) {
	t.Parallel()
	entry := &pricingEntryJSON{}
	aliasEntry := &pricingEntryJSON{}

	pricingMap := map[string]*pricingEntryJSON{
		"bedrock/claude-sonnet":         entry,
		"bedrock:us/claude-haiku":       aliasEntry,
		"bedrock/claude-3-instruct-pre": {}, // for instruct strip
	}

	cases := []struct {
		name     string
		provider ProviderName
		modelID  string
		want     *pricingEntryJSON
	}{
		{
			name:     "direct hit on canonical key",
			provider: ProviderBedrock,
			modelID:  "claude-sonnet",
			want:     entry,
		},
		{
			name:     "alias falls back to canonical bedrock key",
			provider: "bedrock:aus",
			modelID:  "claude-sonnet",
			want:     entry,
		},
		{
			name:     "alias key present — returns alias entry",
			provider: "bedrock:us",
			modelID:  "claude-haiku",
			want:     aliasEntry,
		},
		{
			name:     "both keys absent — returns nil",
			provider: "bedrock:us",
			modelID:  "claude-opus",
			want:     nil,
		},
		{
			name:     "instruct suffix stripped on bedrock alias",
			provider: "bedrock:aus",
			modelID:  "claude-3-instruct-pre-instruct",
			want:     pricingMap["bedrock/claude-3-instruct-pre"],
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lookupPricing(pricingMap, tc.provider, tc.modelID)
			if got != tc.want {
				t.Errorf("lookupPricing(%q, %q) = %v, want %v", tc.provider, tc.modelID, got, tc.want)
			}
		})
	}
}

// TestLookupPricingCrossProviderFallback covers the best-effort fallback that
// borrows a price from another provider by bare model name when no row exists
// for the request's provider (e.g. gpt-5.x on Bedrock, priced only under Azure).
func TestLookupPricingCrossProviderFallback(t *testing.T) {
	t.Parallel()
	azureBare := &pricingEntryJSON{Provider: "azure", ModelID: "gpt-5.4", EffectiveInput: 2.5, EffectiveOutput: 15}
	azureRegional := &pricingEntryJSON{Provider: "azure", ModelID: "us/gpt-5.4", EffectiveInput: 2.75, EffectiveOutput: 16.5}
	bedrockExact := &pricingEntryJSON{Provider: "bedrock:test", ModelID: "openai.gpt-5.4", EffectiveInput: 3, EffectiveOutput: 20}

	t.Run("no bedrock row borrows azure price, plainest model_id wins", func(t *testing.T) {
		t.Parallel()
		m := map[string]*pricingEntryJSON{
			"azure/gpt-5.4":    azureBare,
			"azure/us/gpt-5.4": azureRegional,
		}
		if got := lookupPricing(m, "bedrock:us", "openai.gpt-5.4"); got != azureBare {
			t.Errorf("got %v, want the bare azure row (2.5/15)", got)
		}
	})

	t.Run("exact provider row wins over the cross-provider fallback", func(t *testing.T) {
		t.Parallel()
		m := map[string]*pricingEntryJSON{
			"bedrock/openai.gpt-5.4": bedrockExact,
			"azure/gpt-5.4":          azureBare,
		}
		if got := lookupPricing(m, "bedrock:us", "openai.gpt-5.4"); got != bedrockExact {
			t.Errorf("got %v, want the exact bedrock row (3/20)", got)
		}
	})

	t.Run("unrelated model still misses", func(t *testing.T) {
		t.Parallel()
		m := map[string]*pricingEntryJSON{"azure/gpt-5.4": azureBare}
		if got := lookupPricing(m, "bedrock:us", "openai.nonexistent-model"); got != nil {
			t.Errorf("got %v, want nil (no unrelated borrow)", got)
		}
	})
}

// ─── normalisePricingProvider ─────────────────────────────────────────────────

func TestNormalisePricingProvider(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   ProviderName
		want ProviderName
	}{
		{ProviderBedrock, ProviderBedrock},
		{"bedrock:us", ProviderBedrock},
		{"bedrock:prod_east", ProviderBedrock},
		{ProviderVertexAI, ProviderVertexAI},
		{ProviderAzure, ProviderAzure},
	}
	for _, tc := range cases {
		t.Run(string(tc.in), func(t *testing.T) {
			t.Parallel()
			if got := normalisePricingProvider(tc.in); got != tc.want {
				t.Errorf("normalisePricingProvider(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestVmCatalogPricing_PicksLowestEffectiveCost(t *testing.T) {
	t.Parallel()
	vm := VirtualModel{
		Routes: []VirtualModelRoute{
			{Provider: ProviderBedrock, ModelID: "cheap-model", Enabled: true},
			{Provider: ProviderVertexAI, ModelID: "expensive-model", Enabled: true},
		},
	}
	pricingMap := map[string]*pricingEntryJSON{
		"bedrock/cheap-model": {
			Provider:        "bedrock",
			ModelID:         "cheap-model",
			EffectiveInput:  1.0,
			EffectiveOutput: 5.0,
		},
		"vertexai/expensive-model": {
			Provider:        "vertexai",
			ModelID:         "expensive-model",
			EffectiveInput:  3.0,
			EffectiveOutput: 15.0,
		},
	}
	got := vmCatalogPricing(vm, pricingMap)
	if got == nil {
		t.Fatal("expected non-nil pricing")
	}
	if got.EffectiveInputCostPer1M == nil || *got.EffectiveInputCostPer1M != 1.0 {
		t.Errorf("EffectiveInputCostPer1M = %v, want *1.0 (cheap-model)", got.EffectiveInputCostPer1M)
	}
}

func TestVmCatalogPricing_SkipsDisabledRoutes(t *testing.T) {
	t.Parallel()
	vm := VirtualModel{
		Routes: []VirtualModelRoute{
			{Provider: ProviderBedrock, ModelID: "cheap-model", Enabled: false},
			{Provider: ProviderVertexAI, ModelID: "expensive-model", Enabled: true},
		},
	}
	pricingMap := map[string]*pricingEntryJSON{
		"bedrock/cheap-model": {
			Provider:        "bedrock",
			ModelID:         "cheap-model",
			EffectiveInput:  1.0,
			EffectiveOutput: 5.0,
		},
		"vertexai/expensive-model": {
			Provider:        "vertexai",
			ModelID:         "expensive-model",
			EffectiveInput:  3.0,
			EffectiveOutput: 15.0,
		},
	}
	got := vmCatalogPricing(vm, pricingMap)
	if got == nil {
		t.Fatal("expected non-nil pricing")
	}
	if got.EffectiveInputCostPer1M == nil || *got.EffectiveInputCostPer1M != 3.0 {
		t.Errorf("EffectiveInputCostPer1M = %v, want *3.0 (disabled cheap route ignored)", got.EffectiveInputCostPer1M)
	}
}
