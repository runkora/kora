package inferencegw

import (
	"context"
	"errors"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go"
)

// ─── stub connector ───────────────────────────────────────────────────────────

// stubConnector is a minimal ProviderConnector for routing tests.
type stubConnector struct {
	name   ProviderName
	models []ProviderModel
}

func (s *stubConnector) Name() ProviderName { return s.name }
func (s *stubConnector) ListModels(_ context.Context) ([]ProviderModel, error) {
	return s.models, nil
}
func (s *stubConnector) ChatCompletion(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return nil, errors.New("stub")
}
func (s *stubConnector) ChatCompletionStream(_ context.Context, _ openai.ChatCompletionNewParams) (<-chan OpenAIStreamResult, error) {
	return nil, errors.New("stub")
}
func (s *stubConnector) AnthropicMessages(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
	return nil, errors.New("stub")
}
func (s *stubConnector) AnthropicMessagesStream(_ context.Context, _ anthropic.MessageNewParams) (<-chan AnthropicStreamEvent, error) {
	return nil, errors.New("stub")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// newTestSnap builds a virtualCatalogSnapshot from a slice of virtual models.
// All models are added to byID, byRouteKey, and all; only enabled ones appear in list.
func newTestSnap(vms []VirtualModel, disabledRoutes map[string]bool) *virtualCatalogSnapshot {
	byID := make(map[string]*VirtualModel, len(vms))
	byRouteKey := make(map[string]*VirtualModel)
	list := make([]VirtualModel, 0, len(vms))
	for i := range vms {
		vm := &vms[i]
		byID[vm.FullID()] = vm
		for j := range vm.Routes {
			key := string(vm.Routes[j].Provider) + "/" + vm.Routes[j].ModelID
			byRouteKey[key] = vm
		}
		if vm.Enabled {
			list = append(list, *vm)
		}
	}
	if disabledRoutes == nil {
		disabledRoutes = make(map[string]bool)
	}
	return &virtualCatalogSnapshot{byID: byID, byRouteKey: byRouteKey, list: list, all: vms, disabledRoutes: disabledRoutes}
}

// dualConnectors returns Bedrock + VertexAI stub connectors.
func dualConnectors() map[ProviderName]ProviderConnector {
	return map[ProviderName]ProviderConnector{
		ProviderBedrock:  &stubConnector{name: ProviderBedrock},
		ProviderVertexAI: &stubConnector{name: ProviderVertexAI},
	}
}

// ─── RouteRequest ─────────────────────────────────────────────────────────────

func TestRouteRequest_NilCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(dualConnectors())
	_, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-sonnet-4-6"})
	if !errors.Is(err, ErrNoCatalog) {
		t.Errorf("expected ErrNoCatalog with nil catalog, got: %v", err)
	}
}

func TestRouteRequest_ModelNotFound(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(nil, nil))
	_, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/unknown"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest for unknown model, got: %v", err)
	}
}

func TestRouteRequest_DisabledVirtualModel(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6",
			Strategy: VirtualStrategyPriority, Enabled: false,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "m1", Priority: 1, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	_, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-sonnet-4-6"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest for disabled virtual model, got: %v", err)
	}
}

func TestRouteRequest_PriorityStrategy_OrderByPriority(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6",
			Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "bedrock-model", Priority: 1, Enabled: true},
				{ID: "r2", Provider: ProviderVertexAI, ModelID: "vertex-model", Priority: 2, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyPriority {
		t.Errorf("strategy = %q, want priority", strategy)
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(targets))
	}
	if targets[0].Provider != ProviderBedrock {
		t.Errorf("first target = %q, want bedrock (priority 1)", targets[0].Provider)
	}
}

func TestRouteRequest_LowestCostStrategy_SortsByCost(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "google", Name: "gemini-flash",
			Strategy: VirtualStrategyLowestCost, Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "expensive", Priority: 1,
					InputCostPer1M: 10.0, OutputCostPer1M: 10.0, Enabled: true},
				{ID: "r2", Provider: ProviderVertexAI, ModelID: "cheap", Priority: 2,
					InputCostPer1M: 1.0, OutputCostPer1M: 1.0, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "google/gemini-flash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyLowestCost {
		t.Errorf("strategy = %q, want lowest_cost", strategy)
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(targets))
	}
	if targets[0].ModelID != "cheap" {
		t.Errorf("first target = %q, want cheap (lowest cost route first)", targets[0].ModelID)
	}
}

func TestRouteRequest_WeightedStrategy_SingleRoute(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-haiku",
			Strategy: VirtualStrategyWeighted, Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Weight: 100, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-haiku"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyWeighted {
		t.Errorf("strategy = %q, want weighted", strategy)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
}

func TestRouteRequest_WeightedStrategy_ReturnsAllRoutes(t *testing.T) {
	t.Parallel()
	// Weighted strategy selects one route probabilistically, then appends
	// the remaining routes as fallback — all routes must appear in the result.
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-haiku",
			Strategy: VirtualStrategyWeighted, Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Weight: 50, Enabled: true},
				{ID: "r2", Provider: ProviderVertexAI, ModelID: "model-b", Priority: 2, Weight: 50, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-haiku"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyWeighted {
		t.Errorf("strategy = %q, want weighted", strategy)
	}
	if len(targets) != 2 {
		t.Errorf("len(targets) = %d, want 2 (selected + fallback)", len(targets))
	}
}

func TestRouteRequest_DisabledRouteExcluded(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6",
			Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: false},
				{ID: "r2", Provider: ProviderVertexAI, ModelID: "model-b", Priority: 2, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	targets, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1 (disabled route excluded)", len(targets))
	}
	if targets[0].ModelID != "model-b" {
		t.Errorf("target = %q, want model-b", targets[0].ModelID)
	}
}

func TestRouteRequest_ProviderOverrideExcluded(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6",
			Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: true},
				{ID: "r2", Provider: ProviderVertexAI, ModelID: "model-b", Priority: 2, Enabled: true},
			},
		},
	}
	// Platform admin disabled bedrock/model-a via provider override.
	disabled := map[string]bool{"bedrock/model-a": true}
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(vms, disabled))
	targets, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1 (overridden route excluded)", len(targets))
	}
	if targets[0].ModelID != "model-b" {
		t.Errorf("target = %q, want model-b", targets[0].ModelID)
	}
}

func TestRouteRequest_ProviderNotConfigured_Excluded(t *testing.T) {
	t.Parallel()
	// Only Bedrock is wired up; VertexAI has a route but no connector.
	connectors := map[ProviderName]ProviderConnector{
		ProviderBedrock: &stubConnector{name: ProviderBedrock},
	}
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6",
			Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: true},
				{ID: "r2", Provider: ProviderVertexAI, ModelID: "model-b", Priority: 2, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(connectors)
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	targets, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 || targets[0].Provider != ProviderBedrock {
		t.Errorf("expected 1 bedrock target only, got %+v", targets)
	}
}

func TestRouteRequest_NoEnabledRoutes(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6",
			Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: false},
			},
		},
	}
	r := NewRoutingEngine(dualConnectors())
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	_, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "anthropic/claude-sonnet-4-6"})
	if !errors.Is(err, ErrNoProvidersEnabled) {
		t.Errorf("expected ErrNoProvidersEnabled, got: %v", err)
	}
}

// ─── IsWildcardModel ─────────────────────────────────────────────────────────

func TestIsWildcardModel_AlwaysFalse(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil)
	for _, m := range []string{"", "*", "failover", "anthropic/claude-sonnet-4-6", "bare-name"} {
		if r.IsWildcardModel(m) {
			t.Errorf("IsWildcardModel(%q) = true, want false — virtual model catalog has no wildcard routing", m)
		}
	}
}

// ─── ResolveModelForPolicy ────────────────────────────────────────────────────

func TestResolveModelForPolicy_Found(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true},
	}
	r := NewRoutingEngine(nil)
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	info, ok := r.ResolveModelForPolicy("anthropic/claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected ok=true for known virtual model")
	}
	if info.Family != "anthropic" {
		t.Errorf("Family = %q, want anthropic", info.Family)
	}
	if info.Alias != "claude-sonnet-4-6" {
		t.Errorf("Alias = %q, want claude-sonnet-4-6", info.Alias)
	}
}

func TestResolveModelForPolicy_NotFound(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil)
	r.LoadVirtualCatalog(newTestSnap(nil, nil))
	_, ok := r.ResolveModelForPolicy("anthropic/unknown")
	if ok {
		t.Error("expected ok=false for unknown virtual model")
	}
}

func TestResolveModelForPolicy_NilCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil)
	_, ok := r.ResolveModelForPolicy("anthropic/claude-sonnet-4-6")
	if ok {
		t.Error("expected ok=false with nil catalog")
	}
}

// ─── ResolveTargetForPolicy ──────────────────────────────────────────────────

func TestResolveTargetForPolicy_Found(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-4-6", Priority: 1, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(nil)
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	info, ok := r.ResolveTargetForPolicy(ProviderBedrock, "us.anthropic.claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected ok=true for known route target")
	}
	if info.Family != "anthropic" {
		t.Errorf("Family = %q, want anthropic", info.Family)
	}
	if info.Alias != "claude-sonnet-4-6" {
		t.Errorf("Alias = %q, want claude-sonnet-4-6", info.Alias)
	}
}

func TestResolveTargetForPolicy_NotFound(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "model-a", Priority: 1, Enabled: true},
			},
		},
	}
	r := NewRoutingEngine(nil)
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	_, ok := r.ResolveTargetForPolicy(ProviderBedrock, "nonexistent-model")
	if ok {
		t.Error("expected ok=false for unknown model")
	}
}

func TestResolveTargetForPolicy_NilCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil)
	_, ok := r.ResolveTargetForPolicy(ProviderBedrock, "model-a")
	if ok {
		t.Error("expected ok=false with nil catalog")
	}
}

// ─── CatalogScopes ───────────────────────────────────────────────────────────

func TestCatalogScopes_DerivesFromVirtualModels(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true},
		{Family: "anthropic", Name: "claude-haiku-4-5", Enabled: true},
		{Family: "google", Name: "gemini-flash", Enabled: true},
	}
	r := NewRoutingEngine(nil)
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
	scopes := r.CatalogScopes()
	// 1 family scope for anthropic + 2 model scopes for anthropic +
	// 1 family scope for google + 1 model scope for google = 5 total.
	if len(scopes) != 5 {
		t.Errorf("len(scopes) = %d, want 5", len(scopes))
	}

	seen := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		seen[s.Scope] = true
	}
	expected := []string{
		"inference:anthropic:use",
		"inference:anthropic:claude-sonnet-4-6:use",
		"inference:anthropic:claude-haiku-4-5:use",
		"inference:google:use",
		"inference:google:gemini-flash:use",
	}
	for _, e := range expected {
		if !seen[e] {
			t.Errorf("expected scope %q not present in catalog scopes", e)
		}
	}
}

func TestCatalogScopes_NilCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil)
	if scopes := r.CatalogScopes(); scopes != nil {
		t.Errorf("expected nil scopes with nil catalog, got %v", scopes)
	}
}

// ─── ListModels ───────────────────────────────────────────────────────────────

func TestListModels_PopulatesProviderModelCache(t *testing.T) {
	t.Parallel()
	models := []ProviderModel{
		{
			Provider:     ProviderBedrock,
			ModelID:      "us.anthropic.claude-sonnet-4-6",
			Capabilities: []ModelCapability{CapabilityChat},
		},
	}
	connector := &stubConnector{name: ProviderBedrock, models: models}
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock: connector,
	})

	result, err := r.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	// ProviderModels() should return the cached result.
	cached := r.ProviderModels()
	if len(cached) != 1 {
		t.Errorf("ProviderModels() = %d models, want 1", len(cached))
	}
}

func TestListModels_NoProviders(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil)
	_, err := r.ListModels(context.Background())
	if !errors.Is(err, ErrNoProvidersEnabled) {
		t.Errorf("expected ErrNoProvidersEnabled, got: %v", err)
	}
}
