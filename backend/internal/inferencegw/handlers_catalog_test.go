package inferencegw

import (
	"strings"
	"testing"
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
			{Provider: "bedrock", ModelID: "us.anthropic.claude-sonnet-4-6-v1:0", Priority: 1, Enabled: true},
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
			route:   routeInput{Provider: "openai", ModelID: "gpt-4", Priority: 1},
			wantMsg: `route[0].provider "openai" is not valid`,
		},
		{
			name:    "missing model_id",
			route:   routeInput{Provider: "bedrock", ModelID: "", Priority: 1},
			wantMsg: "route[0].model_id is required",
		},
		{
			name:    "priority zero",
			route:   routeInput{Provider: "bedrock", ModelID: "some-model", Priority: 0},
			wantMsg: "route[0].priority must be ≥ 1",
		},
		{
			name:    "priority negative",
			route:   routeInput{Provider: "vertexai", ModelID: "some-model", Priority: -1},
			wantMsg: "route[0].priority must be ≥ 1",
		},
		{
			name:    "weight above 100",
			route:   routeInput{Provider: "bedrock", ModelID: "some-model", Priority: 1, Weight: 101},
			wantMsg: "route[0].weight must be between 0 and 100",
		},
		{
			name:    "weight negative",
			route:   routeInput{Provider: "bedrock", ModelID: "some-model", Priority: 1, Weight: -1},
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
			{Provider: "bedrock", ModelID: "model-a", Priority: 1, Weight: 60, Enabled: true},
			{Provider: "vertexai", ModelID: "model-b", Priority: 2, Weight: 40, Enabled: true},
		}
		if err := validateVirtualModelRequest(req); err != nil {
			t.Errorf("expected no error when weights sum to 100, got: %v", err)
		}
	})

	t.Run("weights do not sum to 100", func(t *testing.T) {
		t.Parallel()
		req := base
		req.Routes = []routeInput{
			{Provider: "bedrock", ModelID: "model-a", Priority: 1, Weight: 60, Enabled: true},
			{Provider: "vertexai", ModelID: "model-b", Priority: 2, Weight: 30, Enabled: true},
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
			{Provider: "bedrock", ModelID: "model-a", Priority: 1, Weight: 100, Enabled: true},
			{Provider: "vertexai", ModelID: "model-b", Priority: 2, Weight: 50, Enabled: false},
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

// ─── isValidProvider ─────────────────────────────────────────────────────────

func TestIsValidProvider(t *testing.T) {
	t.Parallel()
	valid := []ProviderName{ProviderBedrock, ProviderVertexAI, ProviderAzure}
	for _, p := range valid {
		if !isValidProvider(p) {
			t.Errorf("isValidProvider(%q) = false, want true", p)
		}
	}
	invalid := []ProviderName{"openai", "gcp", "", "BEDROCK"}
	for _, p := range invalid {
		if isValidProvider(p) {
			t.Errorf("isValidProvider(%q) = true, want false", p)
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
