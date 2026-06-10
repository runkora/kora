package inferencegw

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
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
	// budgetHasLimit controls whether CheckBudget reports a configured limit.
	budgetHasLimit bool
	// budgetEffectiveUSD is the effective_limit_usd returned by CheckBudget.
	budgetEffectiveUSD float64
	// budgetErr, when non-nil, is returned by CheckBudget.
	budgetErr error
	// lastBudgetSpend captures the currentSpendUSD passed to CheckBudget.
	lastBudgetSpend float64
	// budgetCategories is returned by GetBudgetCategories. nil means no limit.
	budgetCategories []policy.BudgetCategory
	// budgetCategoriesErr, when non-nil, is returned by GetBudgetCategories.
	budgetCategoriesErr error
	// roleBudgets is returned by GetRoleBudgets. nil means no role budgets configured.
	roleBudgets []policy.BudgetCategory
	// lastRoleBudgetRoles captures the roleNames passed to GetRoleBudgets.
	lastRoleBudgetRoles []string
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

func (s *stubEvaluator) CheckBudget(_ context.Context, _, _ string, spend float64) (policy.BudgetResult, error) {
	s.lastBudgetSpend = spend
	if s.budgetErr != nil {
		return policy.BudgetResult{}, s.budgetErr
	}
	allow := true
	if s.checkAllow != nil {
		allow = *s.checkAllow
	}
	return policy.BudgetResult{
		Allow:              allow,
		HasLimit:           s.budgetHasLimit,
		EffectiveBudgetUSD: s.budgetEffectiveUSD,
	}, nil
}

func (s *stubEvaluator) GetBudgetCategories(_ context.Context, _, _ string) ([]policy.BudgetCategory, error) {
	return s.budgetCategories, s.budgetCategoriesErr
}

func (s *stubEvaluator) GetRoleBudgets(_ context.Context, _ string, roleNames []string) ([]policy.BudgetCategory, error) {
	s.lastRoleBudgetRoles = roleNames
	return s.roleBudgets, nil
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

func ctxWithRoles(t *testing.T, roles ...string) context.Context {
	t.Helper()
	c := testClaims("tenant-1")
	c.Orbit.Claims = map[string]string{"roles": strings.Join(roles, ",")}
	return token.WithClaims(context.Background(), c)
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

// ─── GetPersonalBudget — service-level ──────────────────────────────────────

func newBudgetService(eval *stubEvaluator, counter SpendCounter) *Service {
	return NewService(eval, buildTestRouter(nil), NewNopPricingCache(), nil, counter, logging.NewLoggerWithOutput(io.Discard))
}

func TestGetPersonalBudget_SpendThreadedIntoCheckBudget(t *testing.T) {
	t.Parallel()
	// Verify the spend returned by the counter is the exact value passed to CheckBudget.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spend: 42.5}
	svc := newBudgetService(eval, counter)

	status, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetSpend != 42.5 {
		t.Errorf("CheckBudget received spend = %v, want 42.5 (from counter)", eval.lastBudgetSpend)
	}
	if status.CurrentSpendUSD != 42.5 {
		t.Errorf("CurrentSpendUSD = %v, want 42.5", status.CurrentSpendUSD)
	}
}

func TestGetPersonalBudget_CounterError_FailsOpen(t *testing.T) {
	t.Parallel()
	// Counter error → fail-open: empty BudgetStatus, no error returned.
	eval := &stubEvaluator{}
	counter := &mockSpendCounter{getSpendErr: errors.New("redis down")}
	svc := newBudgetService(eval, counter)

	status, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("counter error should not propagate, got: %v", err)
	}
	if status.HasLimit || status.SpendLimitReached || status.CurrentSpendUSD != 0 {
		t.Errorf("fail-open should return empty status, got: %+v", status)
	}
	// CheckBudget must not have been called when GetSpend failed.
	if eval.lastBudgetSpend != 0 {
		t.Error("CheckBudget should not be called when GetSpend errors")
	}
}

func TestGetPersonalBudget_OPAError_FailsOpen(t *testing.T) {
	t.Parallel()
	// OPA error → fail-open: empty BudgetStatus, no error returned.
	eval := &stubEvaluator{budgetErr: errors.New("OPA unavailable")}
	counter := &mockSpendCounter{spend: 10.0}
	svc := newBudgetService(eval, counter)

	status, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("OPA error should not propagate, got: %v", err)
	}
	if status.HasLimit || status.SpendLimitReached || status.CurrentSpendUSD != 0 {
		t.Errorf("fail-open should return empty status, got: %+v", status)
	}
}

func TestGetPersonalBudget_CategoriesPassedToCounter(t *testing.T) {
	t.Parallel()
	// GetBudgetCategories returns two categories; verify GetSpend receives them
	// (indirectly: the mock counter sums correctly regardless of categories,
	// so we verify that the spend value flows through unchanged).
	cats := []policy.BudgetCategory{
		{Source: "role", ID: "eng", ResetCadence: "monthly"},
		{Source: "grant", ID: "g1", ResetCadence: "monthly"},
	}
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 80, budgetCategories: cats}
	counter := &mockSpendCounter{spend: 30.0}
	svc := newBudgetService(eval, counter)

	status, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetSpend != 30.0 {
		t.Errorf("CheckBudget received spend = %v, want 30.0", eval.lastBudgetSpend)
	}
	if status.CurrentSpendUSD != 30.0 {
		t.Errorf("CurrentSpendUSD = %v, want 30.0", status.CurrentSpendUSD)
	}
	if !status.HasLimit {
		t.Error("HasLimit should be true")
	}
}

func TestGetPersonalBudget_GetBudgetCategoriesError_FailsOpen(t *testing.T) {
	t.Parallel()
	// GetBudgetCategories errors → categories treated as nil, spend still read via legacy path.
	eval := &stubEvaluator{
		budgetHasLimit:      true,
		budgetEffectiveUSD:  50,
		budgetCategoriesErr: errors.New("OPA unavailable"),
	}
	counter := &mockSpendCounter{spend: 20.0}
	svc := newBudgetService(eval, counter)

	// Must not error: categories error is fail-open.
	status, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("categories error should not propagate, got: %v", err)
	}
	// Spend still flows through (legacy path, nil categories).
	if eval.lastBudgetSpend != 20.0 {
		t.Errorf("CheckBudget received spend = %v, want 20.0", eval.lastBudgetSpend)
	}
	_ = status
}

func TestGetPersonalBudget_RoleCategoriesMergedFromJWT(t *testing.T) {
	t.Parallel()
	// JWT carries "eng" role. GetRoleBudgets returns one role category.
	// GetBudgetCategories returns one grant category.
	// mergeRoleCategories should append the role category so the counter
	// receives both. The mock counter sums correctly regardless of categories,
	// so we verify that GetRoleBudgets was called with the extracted role names.
	roleCat := policy.BudgetCategory{Source: "role", ID: "eng", ResetCadence: "monthly"}
	eval := &stubEvaluator{
		budgetHasLimit:     true,
		budgetEffectiveUSD: 100,
		budgetCategories: []policy.BudgetCategory{
			{Source: "grant", ID: "g1", ResetCadence: "monthly"},
		},
		roleBudgets: []policy.BudgetCategory{roleCat},
	}
	counter := &mockSpendCounter{spend: 30.0}
	svc := newBudgetService(eval, counter)

	// Context carries claims with role "eng".
	ctx := ctxWithRoles(t, "eng")
	status, err := svc.GetPersonalBudget(ctx, "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(eval.lastRoleBudgetRoles) != 1 || eval.lastRoleBudgetRoles[0] != "eng" {
		t.Errorf("GetRoleBudgets called with roles = %v, want [eng]", eval.lastRoleBudgetRoles)
	}
	if status.CurrentSpendUSD != 30.0 {
		t.Errorf("CurrentSpendUSD = %v, want 30.0", status.CurrentSpendUSD)
	}
}

func TestGetPersonalBudget_NoRolesInJWT_SkipsGetRoleBudgets(t *testing.T) {
	t.Parallel()
	// JWT has no roles claim. GetRoleBudgets must not be called.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spend: 10.0}
	svc := newBudgetService(eval, counter)

	// ctxWithClaims produces claims with no roles.
	_, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastRoleBudgetRoles != nil {
		t.Errorf("GetRoleBudgets should not be called when no roles in JWT, got roles = %v", eval.lastRoleBudgetRoles)
	}
}
