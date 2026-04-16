package inferencegw

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/golang-jwt/jwt/v5"
)

// ─── stub evaluator ─────────────────────────────────────────────────────────

// stubEvaluator implements policy.Evaluator for service-level tests.
// allowedScopes controls which candidate IDs are allowed by Query,
// and which (resourceType:pathSegments:action) scopes pass Check.
type stubEvaluator struct {
	// allowedCandidateIDs is the set of candidate IDs that Query returns.
	allowedCandidateIDs map[string]bool
	// checkAllow controls the result of Check. When nil, Check always allows.
	checkAllow *bool
}

func (s *stubEvaluator) Check(_ context.Context, input policy.CheckInput) (policy.Result, error) {
	if s.checkAllow != nil {
		return policy.Result{Allow: *s.checkAllow, Reason: "stub"}, nil
	}
	// Default: allow if the derived scope is in allowedCandidateIDs.
	var scope strings.Builder
	scope.WriteString(input.ResourceType)
	for _, seg := range input.PathSegments {
		scope.WriteString(":" + seg)
	}
	scope.WriteString(":" + input.Action)
	if s.allowedCandidateIDs[scope.String()] {
		return policy.Result{Allow: true, Reason: "allow"}, nil
	}
	return policy.Result{Allow: false, Reason: "deny"}, nil
}

func (s *stubEvaluator) Query(_ context.Context, input policy.QueryInput) ([]string, error) {
	var allowed []string
	for _, c := range input.Candidates {
		if s.allowedCandidateIDs[c.ID] {
			allowed = append(allowed, c.ID)
		}
	}
	return allowed, nil
}

func (s *stubEvaluator) Scopes(_ context.Context, _ policy.ScopesInput) ([]string, error) {
	return nil, nil
}

func (s *stubEvaluator) ScopesExplain(_ context.Context, _ policy.ScopesInput) (*policy.ScopesExplainResult, error) {
	return &policy.ScopesExplainResult{}, nil
}

func (s *stubEvaluator) Start(_ context.Context) {}

// ─── helpers ────────────────────────────────────────────────────────────────

func testClaims(tenantID string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
		Orbit: token.OrbitClaims{
			TenantID:      tenantID,
			PrincipalType: token.PrincipalTypeHuman,
		},
	}
}

func ctxWithClaims(t *testing.T) context.Context {
	t.Helper()
	return token.WithClaims(context.Background(), testClaims("tenant-1"))
}

func buildTestRouter(models []ProviderModel) *RoutingEngine {
	connectors := map[ProviderName]ProviderConnector{
		ProviderBedrock:  &stubConnector{name: ProviderBedrock},
		ProviderVertexAI: &stubConnector{name: ProviderVertexAI},
	}
	r := NewRoutingEngine(connectors, NewNopPricingCache())
	if models != nil {
		r.modelCatalog.Store(&models)
	}
	return r
}

// ─── checkModelPolicy ────────────────────────────────────────────────────────

func TestCheckModelPolicy_FailsClosed_UnknownModel(t *testing.T) {
	t.Parallel()
	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "known-model", Family: "anthropic", Aliases: []string{"claude-sonnet"}},
	}
	svc := NewService(
		&stubEvaluator{},
		buildTestRouter(catalog),
		NewNopPricingCache(),
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	err := svc.checkModelPolicy(ctx, "totally-unknown-model")
	if !errors.Is(err, ErrPolicyDenied) {
		t.Errorf("expected ErrPolicyDenied for unknown model, got: %v", err)
	}
}

func TestCheckModelPolicy_AllowsWildcards(t *testing.T) {
	t.Parallel()
	svc := NewService(
		&stubEvaluator{},
		buildTestRouter(nil),
		NewNopPricingCache(),
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	for _, model := range []string{"", "*", "failover"} {
		if err := svc.checkModelPolicy(ctx, model); err != nil {
			t.Errorf("checkModelPolicy(%q) = %v, want nil (wildcards deferred)", model, err)
		}
	}
}

func TestCheckModelPolicy_ExplicitModel_Allowed(t *testing.T) {
	t.Parallel()
	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet", Family: "anthropic", Aliases: []string{"claude-sonnet"}},
	}
	allow := true
	svc := NewService(
		&stubEvaluator{checkAllow: &allow},
		buildTestRouter(catalog),
		NewNopPricingCache(),
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	// Use the alias form that ResolveModelForPolicy can match.
	err := svc.checkModelPolicy(ctx, "claude-sonnet")
	if err != nil {
		t.Errorf("expected nil for allowed model, got: %v", err)
	}
}

func TestCheckModelPolicy_ExplicitModel_Denied(t *testing.T) {
	t.Parallel()
	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet", Family: "anthropic", Aliases: []string{"claude-sonnet"}},
	}
	deny := false
	svc := NewService(
		&stubEvaluator{checkAllow: &deny},
		buildTestRouter(catalog),
		NewNopPricingCache(),
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	err := svc.checkModelPolicy(ctx, "claude-sonnet")
	if !errors.Is(err, ErrPolicyDenied) {
		t.Errorf("expected ErrPolicyDenied for denied model, got: %v", err)
	}
}

// ─── filterTargetsByPolicy ──────────────────────────────────────────────────

func TestFilterTargetsByPolicy_FiltersCorrectly(t *testing.T) {
	t.Parallel()
	// Use realistic ModelIDs (longer than aliases) so PolicyAlias() picks the alias.
	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-4-6-v1:0", Family: "anthropic", Aliases: []string{"claude-sonnet"}},
		{Provider: ProviderBedrock, ModelID: "us.meta.llama-3-70b-instruct-v1:0", Family: "meta", Aliases: []string{"llama-3-70b"}},
		{Provider: ProviderVertexAI, ModelID: "claude-sonnet-4-6@20250514", Family: "anthropic", Aliases: []string{"claude-sonnet"}},
	}
	svc := NewService(
		&stubEvaluator{allowedCandidateIDs: map[string]bool{
			"anthropic:claude-sonnet": true,
			// meta:llama-3-70b NOT allowed
		}},
		buildTestRouter(catalog),
		NewNopPricingCache(),
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	targets := []RouteTarget{
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-4-6-v1:0"},
		{Provider: ProviderBedrock, ModelID: "us.meta.llama-3-70b-instruct-v1:0"},
		{Provider: ProviderVertexAI, ModelID: "claude-sonnet-4-6@20250514"},
	}

	filtered, err := svc.filterTargetsByPolicy(ctx, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both anthropic targets should survive. Meta should be filtered out.
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
	for _, f := range filtered {
		if f.ModelID == "us.meta.llama-3-70b-instruct-v1:0" {
			t.Error("meta target should have been filtered out")
		}
	}
}

func TestFilterTargetsByPolicy_DeniesWhenAllFiltered(t *testing.T) {
	t.Parallel()
	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-4-6-v1:0", Family: "anthropic", Aliases: []string{"claude-sonnet"}},
	}
	svc := NewService(
		&stubEvaluator{allowedCandidateIDs: map[string]bool{}}, // nothing allowed
		buildTestRouter(catalog),
		NewNopPricingCache(),
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	targets := []RouteTarget{
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-4-6-v1:0"},
	}

	_, err := svc.filterTargetsByPolicy(ctx, targets)
	if !errors.Is(err, ErrPolicyDenied) {
		t.Errorf("expected ErrPolicyDenied when all targets filtered, got: %v", err)
	}
}

// ─── filterModelsByPolicy ───────────────────────────────────────────────────

func TestFilterModelsByPolicy_DeduplicatesAndFilters(t *testing.T) {
	t.Parallel()
	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-4-6-v1:0", Family: "anthropic", Aliases: []string{"claude-sonnet"}},
		{Provider: ProviderVertexAI, ModelID: "claude-sonnet-4-6@20250514", Family: "anthropic", Aliases: []string{"claude-sonnet"}},
		{Provider: ProviderBedrock, ModelID: "us.meta.llama-3-70b-instruct-v1:0", Family: "meta", Aliases: []string{"llama-3-70b"}},
	}
	svc := NewService(
		&stubEvaluator{allowedCandidateIDs: map[string]bool{
			"anthropic:claude-sonnet": true,
		}},
		buildTestRouter(catalog),
		NewNopPricingCache(),
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)

	filtered, err := svc.filterModelsByPolicy(ctx, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both anthropic variants should survive, meta should be filtered.
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
	for _, m := range filtered {
		if m.Family != "anthropic" {
			t.Errorf("unexpected family %q in filtered results", m.Family)
		}
	}
}

func TestFilterModelsByPolicy_ExcludesUnknownFamily(t *testing.T) {
	t.Parallel()
	catalog := []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "mystery", Family: "unknown"},
		{Provider: ProviderBedrock, ModelID: "no-family", Family: ""},
	}
	svc := NewService(
		&stubEvaluator{allowedCandidateIDs: map[string]bool{
			"unknown:mystery": true,
			":no-family":      true,
		}},
		buildTestRouter(catalog),
		NewNopPricingCache(),
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)

	filtered, err := svc.filterModelsByPolicy(ctx, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 0 {
		t.Errorf("len(filtered) = %d, want 0 (unknown/empty families excluded)", len(filtered))
	}
}
