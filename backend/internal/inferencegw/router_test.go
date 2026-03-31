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
// It always returns an error for actual inference calls; only ListModels matters here.
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

// ─── parseExplicitModel ───────────────────────────────────────────────────────

func TestParseExplicitModel_Valid(t *testing.T) {
	t.Parallel()
	provider, modelID, err := parseExplicitModel("bedrock/claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "bedrock" {
		t.Errorf("provider = %q, want bedrock", provider)
	}
	if modelID != "claude-sonnet-4-6" {
		t.Errorf("modelID = %q, want claude-sonnet-4-6", modelID)
	}
}

func TestParseExplicitModel_WithSlashInModelID(t *testing.T) {
	t.Parallel()
	// The first "/" is the separator; subsequent "/" belong to the model ID.
	provider, modelID, err := parseExplicitModel("bedrock/us.anthropic.claude-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "bedrock" {
		t.Errorf("provider = %q, want bedrock", provider)
	}
	if modelID != "us.anthropic.claude-3" {
		t.Errorf("modelID = %q, want us.anthropic.claude-3", modelID)
	}
}

func TestParseExplicitModel_NoSlash(t *testing.T) {
	t.Parallel()
	_, _, err := parseExplicitModel("claude-sonnet")
	if err == nil {
		t.Fatal("expected error for model without '/' separator")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("error should wrap ErrInvalidRequest, got: %v", err)
	}
}

func TestParseExplicitModel_EmptyProvider(t *testing.T) {
	t.Parallel()
	_, _, err := parseExplicitModel("/claude-sonnet")
	if err == nil {
		t.Fatal("expected error for empty provider")
	}
}

func TestParseExplicitModel_EmptyModelID(t *testing.T) {
	t.Parallel()
	_, _, err := parseExplicitModel("bedrock/")
	if err == nil {
		t.Fatal("expected error for empty model ID")
	}
}

// ─── resolveAlias ─────────────────────────────────────────────────────────────

func TestResolveAlias_UnknownReturnedUnchanged(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil, NewNopPricingCache())
	got := r.resolveAlias("bedrock/unknown-model")
	if got != "bedrock/unknown-model" {
		t.Errorf("resolveAlias = %q, want unchanged %q", got, "bedrock/unknown-model")
	}
}

func TestResolveAlias_WithPopulatedTable(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil, NewNopPricingCache())
	table := map[string]string{
		"bedrock/claude-sonnet-4-6": "bedrock/us.anthropic.claude-sonnet-4-6",
	}
	r.aliasTable.Store(&table)

	got := r.resolveAlias("bedrock/claude-sonnet-4-6")
	if got != "bedrock/us.anthropic.claude-sonnet-4-6" {
		t.Errorf("resolveAlias = %q, want canonical form", got)
	}
}

func TestResolveAlias_AlreadyCanonical(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil, NewNopPricingCache())
	table := map[string]string{
		"bedrock/claude-sonnet-4-6": "bedrock/us.anthropic.claude-sonnet-4-6",
	}
	r.aliasTable.Store(&table)

	canonical := "bedrock/us.anthropic.claude-sonnet-4-6"
	got := r.resolveAlias(canonical)
	if got != canonical {
		t.Errorf("resolveAlias = %q, want unchanged canonical %q", got, canonical)
	}
}

// ─── RouteRequest ─────────────────────────────────────────────────────────────

func TestRouteRequest_WildcardRequiresCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock: &stubConnector{name: ProviderBedrock},
	}, NewNopPricingCache())
	// Catalog is nil on a fresh engine — lowestCostTargets should fail.
	_, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "*"})
	if err == nil {
		t.Fatal("expected error when catalog is empty and model is '*'")
	}
}

func TestRouteRequest_Failover_RequiresCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock: &stubConnector{name: ProviderBedrock},
	}, NewNopPricingCache())
	_, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "failover"})
	if err == nil {
		t.Fatal("expected error when catalog is empty and model is 'failover'")
	}
}

func TestRouteRequest_ExplicitTarget_UnknownProvider(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock: &stubConnector{name: ProviderBedrock},
	}, NewNopPricingCache())
	_, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "bogus/my-model"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("error should wrap ErrInvalidRequest (→ 400), got: %v", err)
	}
	if strategy != StrategyExplicit {
		t.Errorf("strategy = %q, want explicit", strategy)
	}
}

func TestRouteRequest_ExplicitTarget_KnownProvider(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock: &stubConnector{name: ProviderBedrock},
	}, NewNopPricingCache())
	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "bedrock/claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyExplicit {
		t.Errorf("strategy = %q, want explicit", strategy)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
	if targets[0].Provider != ProviderBedrock {
		t.Errorf("target provider = %q, want bedrock", targets[0].Provider)
	}
	if targets[0].ModelID != "claude-sonnet-4-6" {
		t.Errorf("target modelID = %q, want claude-sonnet-4-6", targets[0].ModelID)
	}
}

func TestRouteRequest_BareModelName_BroadcastsAllProviders(t *testing.T) {
	t.Parallel()
	// Bare model name with empty catalog broadcasts to all providers.
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock:  &stubConnector{name: ProviderBedrock},
		ProviderVertexAI: &stubConnector{name: ProviderVertexAI},
	}, NewNopPricingCache())
	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyFailoverChain {
		t.Errorf("strategy = %q, want failover_chain", strategy)
	}
	if len(targets) != 2 {
		t.Errorf("len(targets) = %d, want 2 (broadcast to all)", len(targets))
	}
}

func TestRouteRequest_BareModelName_FiltersWithWarmCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock:  &stubConnector{name: ProviderBedrock},
		ProviderVertexAI: &stubConnector{name: ProviderVertexAI},
	}, NewNopPricingCache())

	// Warm the catalog with only Bedrock carrying the model.
	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "claude-sonnet-4-6", Capabilities: []ModelCapability{CapabilityChat}},
	}
	r.modelCatalog.Store(&catalog)

	targets, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1 (only bedrock carries this model)", len(targets))
	}
	if targets[0].Provider != ProviderBedrock {
		t.Errorf("target provider = %q, want bedrock", targets[0].Provider)
	}
}

func TestRouteRequest_LowestCost_WithCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock:  &stubConnector{name: ProviderBedrock},
		ProviderVertexAI: &stubConnector{name: ProviderVertexAI},
	}, NewNopPricingCache())

	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "model-a", Capabilities: []ModelCapability{CapabilityChat}},
		{Provider: ProviderVertexAI, ModelID: "model-b", Capabilities: []ModelCapability{CapabilityChat}},
	}
	r.modelCatalog.Store(&catalog)

	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "*"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyLowestCost {
		t.Errorf("strategy = %q, want lowest_cost", strategy)
	}
	if len(targets) != 2 {
		t.Errorf("len(targets) = %d, want 2", len(targets))
	}
}

func TestRouteRequest_LowestCost_SkipsNonChatModels(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock: &stubConnector{name: ProviderBedrock},
	}, NewNopPricingCache())

	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "embed-model", Capabilities: []ModelCapability{CapabilityEmbedding}},
		{Provider: ProviderBedrock, ModelID: "chat-model", Capabilities: []ModelCapability{CapabilityChat}},
	}
	r.modelCatalog.Store(&catalog)

	targets, _, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "*"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tgt := range targets {
		if tgt.ModelID == "embed-model" {
			t.Error("embedding model should not appear in lowestCost targets for chat routing")
		}
	}
}

func TestRouteRequest_ExplicitTarget_AliasResolved(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock: &stubConnector{name: ProviderBedrock},
	}, NewNopPricingCache())

	// Pre-populate the alias table.
	table := map[string]string{
		"bedrock/claude-sonnet": "bedrock/us.anthropic.claude-sonnet-4-6",
	}
	r.aliasTable.Store(&table)

	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "bedrock/claude-sonnet"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyExplicit {
		t.Errorf("strategy = %q, want explicit", strategy)
	}
	if targets[0].ModelID != "us.anthropic.claude-sonnet-4-6" {
		t.Errorf("modelID = %q, want resolved canonical form", targets[0].ModelID)
	}
}

func TestRouteRequest_Failover_WithCatalog(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock:  &stubConnector{name: ProviderBedrock},
		ProviderVertexAI: &stubConnector{name: ProviderVertexAI},
	}, NewNopPricingCache())

	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "model-a", Capabilities: []ModelCapability{CapabilityChat}},
		{Provider: ProviderVertexAI, ModelID: "model-b", Capabilities: []ModelCapability{CapabilityChat}},
	}
	r.modelCatalog.Store(&catalog)

	targets, strategy, err := r.RouteRequest(context.Background(), ChatCompletionRequest{Model: "failover"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != StrategyFailoverChain {
		t.Errorf("strategy = %q, want failover_chain", strategy)
	}
	// Bedrock has priority over Vertex AI.
	if targets[0].Provider != ProviderBedrock {
		t.Errorf("first target = %q, want bedrock (highest priority)", targets[0].Provider)
	}
}

// ─── ListModels + alias table ─────────────────────────────────────────────────

func TestListModels_PopulatesAliasTable(t *testing.T) {
	t.Parallel()
	models := []ProviderModel{
		{
			Provider:     ProviderBedrock,
			ModelID:      "us.anthropic.claude-sonnet-4-6",
			Capabilities: []ModelCapability{CapabilityChat},
			Aliases:      []string{"claude-sonnet-4-6", "anthropic.claude-sonnet-4-6"},
		},
	}
	connector := &stubConnector{name: ProviderBedrock, models: models}
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{
		ProviderBedrock: connector,
	}, NewNopPricingCache())

	_, err := r.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}

	// Alias should resolve to the canonical ID.
	got := r.resolveAlias("bedrock/claude-sonnet-4-6")
	if got != "bedrock/us.anthropic.claude-sonnet-4-6" {
		t.Errorf("alias resolved to %q, want bedrock/us.anthropic.claude-sonnet-4-6", got)
	}
}

func TestListModels_NoProviders(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil, NewNopPricingCache())
	_, err := r.ListModels(context.Background())
	if !errors.Is(err, ErrNoProvidersEnabled) {
		t.Errorf("expected ErrNoProvidersEnabled, got: %v", err)
	}
}
