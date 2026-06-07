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

func buildTestRouter(vms []VirtualModel) *RoutingEngine {
	connectors := map[ProviderName]ProviderConnector{
		ProviderBedrock:  &stubConnector{name: ProviderBedrock},
		ProviderVertexAI: &stubConnector{name: ProviderVertexAI},
	}
	r := NewRoutingEngine(connectors)
	if vms != nil {
		r.LoadVirtualCatalog(newTestSnap(vms, nil))
	}
	return r
}

// ─── checkModelPolicy ────────────────────────────────────────────────────────

func TestCheckModelPolicy_FailsClosed_UnknownModel(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true},
	}
	svc := NewService(
		&stubEvaluator{},
		buildTestRouter(vms),
		NewNopPricingCache(),
		nil,
		nil,
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	err := svc.checkModelPolicy(ctx, "totally-unknown-model")
	if !errors.Is(err, ErrPolicyDenied) {
		t.Errorf("expected ErrPolicyDenied for unknown model, got: %v", err)
	}
}

func TestCheckModelPolicy_VirtualModelAllowed(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true},
	}
	allow := true
	svc := NewService(
		&stubEvaluator{checkAllow: &allow},
		buildTestRouter(vms),
		NewNopPricingCache(),
		nil,
		nil,
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	err := svc.checkModelPolicy(ctx, "anthropic/claude-sonnet-4-6")
	if err != nil {
		t.Errorf("expected nil for allowed virtual model, got: %v", err)
	}
}

func TestCheckModelPolicy_VirtualModelDenied(t *testing.T) {
	t.Parallel()
	vms := []VirtualModel{
		{Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true},
	}
	deny := false
	svc := NewService(
		&stubEvaluator{checkAllow: &deny},
		buildTestRouter(vms),
		NewNopPricingCache(),
		nil,
		nil,
		nil,
		nil,
	)
	ctx := ctxWithClaims(t)
	err := svc.checkModelPolicy(ctx, "anthropic/claude-sonnet-4-6")
	if !errors.Is(err, ErrPolicyDenied) {
		t.Errorf("expected ErrPolicyDenied for denied virtual model, got: %v", err)
	}
}

// ─── filterTargetsByPolicy ──────────────────────────────────────────────────

func TestFilterTargetsByPolicy_FiltersCorrectly(t *testing.T) {
	t.Parallel()
	// Virtual models with routes to realistic provider model IDs.
	// ResolveTargetForPolicy does a reverse lookup through virtual catalog routes.
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-4-6-v1:0", Priority: 1, Enabled: true},
				{ID: "r2", Provider: ProviderVertexAI, ModelID: "claude-sonnet-4-6@20250514", Priority: 2, Enabled: true},
			},
		},
		{
			Family: "meta", Name: "llama-3-70b", Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r3", Provider: ProviderBedrock, ModelID: "us.meta.llama-3-70b-instruct-v1:0", Priority: 1, Enabled: true},
			},
		},
	}
	svc := NewService(
		&stubEvaluator{allowedCandidateIDs: map[string]bool{
			"anthropic:claude-sonnet-4-6": true,
			// meta:llama-3-70b NOT allowed
		}},
		buildTestRouter(vms),
		NewNopPricingCache(),
		nil,
		nil,
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
	// Both anthropic targets survive; meta is filtered out.
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
	vms := []VirtualModel{
		{
			Family: "anthropic", Name: "claude-sonnet-4-6", Enabled: true,
			Routes: []VirtualModelRoute{
				{ID: "r1", Provider: ProviderBedrock, ModelID: "us.anthropic.claude-sonnet-4-6-v1:0", Priority: 1, Enabled: true},
			},
		},
	}
	svc := NewService(
		&stubEvaluator{allowedCandidateIDs: map[string]bool{}}, // nothing allowed
		buildTestRouter(vms),
		NewNopPricingCache(),
		nil,
		nil,
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

// ─── extractFailureDetails ────────────────────────────────────────────────────

func TestExtractFailureDetails_NilError(t *testing.T) {
	t.Parallel()
	if got := extractFailureDetails(nil); got != nil {
		t.Errorf("expected nil for nil error, got %+v", got)
	}
}

func TestExtractFailureDetails_NonConnectorError(t *testing.T) {
	t.Parallel()
	if got := extractFailureDetails(errors.New("generic error")); got != nil {
		t.Errorf("expected nil for non-ConnectorError, got %+v", got)
	}
}

func TestExtractFailureDetails_AllFieldsPopulated(t *testing.T) {
	t.Parallel()
	err := &ConnectorError{
		Provider:        ProviderBedrock,
		Code:            "THROTTLED",
		HTTPStatus:      429,
		ProviderMessage: "Rate exceeded",
	}
	got := extractFailureDetails(err)
	if got == nil {
		t.Fatal("expected non-nil FailureDetails")
	}
	if got.ProviderHTTPStatus == nil || *got.ProviderHTTPStatus != 429 {
		t.Errorf("ProviderHTTPStatus = %v, want 429", got.ProviderHTTPStatus)
	}
	if got.ProviderErrorType == nil || *got.ProviderErrorType != "THROTTLED" {
		t.Errorf("ProviderErrorType = %v, want THROTTLED", got.ProviderErrorType)
	}
	if got.ProviderErrorMessage == nil || *got.ProviderErrorMessage != "Rate exceeded" {
		t.Errorf("ProviderErrorMessage = %v, want 'Rate exceeded'", got.ProviderErrorMessage)
	}
}

func TestExtractFailureDetails_ZeroHTTPStatus(t *testing.T) {
	t.Parallel()
	err := &ConnectorError{Code: "NETWORK_ERROR", HTTPStatus: 0}
	got := extractFailureDetails(err)
	if got == nil {
		t.Fatal("expected non-nil FailureDetails")
	}
	if got.ProviderHTTPStatus != nil {
		t.Errorf("expected nil ProviderHTTPStatus for HTTPStatus=0, got %d", *got.ProviderHTTPStatus)
	}
}

func TestExtractFailureDetails_MessageTruncatedAt500(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 600)
	err := &ConnectorError{Code: "CONTEXT_WINDOW_EXCEEDED", ProviderMessage: long}
	got := extractFailureDetails(err)
	if got == nil || got.ProviderErrorMessage == nil {
		t.Fatal("expected non-nil FailureDetails with message")
	}
	if len(*got.ProviderErrorMessage) != 500 {
		t.Errorf("message length = %d, want 500", len(*got.ProviderErrorMessage))
	}
}

func TestExtractFailureDetails_EmptyConnectorError(t *testing.T) {
	t.Parallel()
	err := &ConnectorError{Provider: ProviderBedrock}
	if got := extractFailureDetails(err); got != nil {
		t.Errorf("expected nil when all fields empty, got %+v", got)
	}
}
