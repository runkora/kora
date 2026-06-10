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
	// lastBudgetRoles captures the roles passed to CheckBudget.
	lastBudgetRoles []string
	// budgetCategories is returned by GetBudgetCategories. nil means no limit.
	budgetCategories []policy.BudgetCategory
	// budgetCategoriesErr, when non-nil, is returned by GetBudgetCategories.
	budgetCategoriesErr error
	// roleBudgets is returned by GetRoleBudgets. nil means no role budgets configured.
	roleBudgets []policy.BudgetCategory
	// lastRoleBudgetRoles captures the roleNames passed to GetRoleBudgets.
	lastRoleBudgetRoles []string
	// allBudgets and allRoleBudgets are returned by ListBudgets.
	allBudgets     map[string]policy.PrincipalBudget
	allRoleBudgets map[string]policy.RoleBudget
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

func (s *stubEvaluator) CheckBudget(_ context.Context, _, _ string, roles []string, spend float64) (policy.BudgetResult, error) {
	s.lastBudgetSpend = spend
	s.lastBudgetRoles = roles
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

func (s *stubEvaluator) ListBudgets(_ context.Context, _ string) (map[string]policy.PrincipalBudget, map[string]policy.RoleBudget, error) {
	return s.allBudgets, s.allRoleBudgets, nil
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

func TestGetPersonalBudget_JWTRolesPassedToCheckBudget(t *testing.T) {
	t.Parallel()
	// Role budgets are resolved inside OPA against the caller's JWT roles —
	// verify the roles extracted from claims reach CheckBudget.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spend: 30.0}
	svc := newBudgetService(eval, counter)

	status, err := svc.GetPersonalBudget(ctxWithRoles(t, "eng", "power-user"), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"eng", "power-user"}
	if len(eval.lastBudgetRoles) != 2 || eval.lastBudgetRoles[0] != want[0] || eval.lastBudgetRoles[1] != want[1] {
		t.Errorf("CheckBudget received roles = %v, want %v", eval.lastBudgetRoles, want)
	}
	if status.CurrentSpendUSD != 30.0 {
		t.Errorf("CurrentSpendUSD = %v, want 30.0", status.CurrentSpendUSD)
	}
	if !status.HasLimit {
		t.Error("HasLimit should be true")
	}
}

func TestGetPersonalBudget_NoRolesInJWT_EmptyRolesToCheckBudget(t *testing.T) {
	t.Parallel()
	// JWT has no roles claim — CheckBudget receives no roles (grant budgets only).
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spend: 10.0}
	svc := newBudgetService(eval, counter)

	_, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(eval.lastBudgetRoles) != 0 {
		t.Errorf("CheckBudget should receive no roles when JWT has none, got %v", eval.lastBudgetRoles)
	}
}

func TestListBudgetPrincipals_MergesSourcesAndSorts(t *testing.T) {
	t.Parallel()
	// Three principals:
	//   pid-role  — role budget $50 (via attribution counter), spend $50 → blocked, utilisation 1.0
	//   pid-grant — grant budget $80 from the bundle, spend $40 → utilisation 0.5
	//   pid-free  — no budget, spend $10 → has_limit=false, listed after budgeted principals
	eval := &stubEvaluator{
		allBudgets: map[string]policy.PrincipalBudget{
			"pid-grant": {
				Categories: []policy.BudgetCategory{
					{Source: "grant", ID: "g1", ResetCadence: "monthly", MonthlyLimitUSD: 80},
				},
				EffectiveLimitUSD: 80,
			},
		},
		allRoleBudgets: map[string]policy.RoleBudget{
			"engineer": {MonthlyLimitUSD: 50, ResetCadence: "monthly"},
		},
	}
	counter := &mockSpendCounter{
		roleAttributions: map[string][]string{"pid-role": {"engineer"}},
	}
	usage := &mockInferenceUsageRepository{
		periodSpends: []PrincipalPeriodSpend{
			{PrincipalID: "pid-grant", PrincipalType: "HUMAN", SpendUSD: 40},
			{PrincipalID: "pid-role", PrincipalType: "HUMAN", SpendUSD: 50},
			{PrincipalID: "pid-free", PrincipalType: "AGENT", SpendUSD: 10},
		},
	}
	svc := NewService(eval, buildTestRouter(nil), NewNopPricingCache(), usage, counter, logging.NewLoggerWithOutput(io.Discard))

	resp, err := svc.ListBudgetPrincipals(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("got %d principals, want 3: %+v", len(resp.Data), resp.Data)
	}

	first := resp.Data[0]
	if first.PrincipalID != "pid-role" || !first.SpendLimitReached || first.EffectiveLimitUSD != 50 {
		t.Errorf("first row = %+v, want pid-role blocked at limit 50", first)
	}
	if len(first.Categories) != 1 || first.Categories[0].Source != "role" || first.Categories[0].ID != "engineer" {
		t.Errorf("pid-role categories = %+v, want one role:engineer category", first.Categories)
	}

	second := resp.Data[1]
	if second.PrincipalID != "pid-grant" || second.SpendLimitReached || second.EffectiveLimitUSD != 80 {
		t.Errorf("second row = %+v, want pid-grant under limit 80", second)
	}

	third := resp.Data[2]
	if third.PrincipalID != "pid-free" || third.HasLimit || third.CurrentSpendUSD != 10 {
		t.Errorf("third row = %+v, want pid-free without limit, spend 10", third)
	}
}

func TestListBudgetPrincipals_RoleCounterForRemovedBudgetIgnored(t *testing.T) {
	t.Parallel()
	// An attribution counter exists for a role whose budget has since been
	// removed — the principal must not be reported as budgeted.
	eval := &stubEvaluator{allRoleBudgets: map[string]policy.RoleBudget{}}
	counter := &mockSpendCounter{
		roleAttributions: map[string][]string{"pid-1": {"engineer"}},
	}
	usage := &mockInferenceUsageRepository{}
	svc := NewService(eval, buildTestRouter(nil), NewNopPricingCache(), usage, counter, logging.NewLoggerWithOutput(io.Discard))

	resp, err := svc.ListBudgetPrincipals(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Errorf("got %d principals, want 0 (stale role counter ignored): %+v", len(resp.Data), resp.Data)
	}
}

func TestIncrementSpend_RoleCategoriesMergedForAttribution(t *testing.T) {
	t.Parallel()
	// Attribution counters: grant categories from the bundle plus role
	// categories resolved from the JWT roles must all reach IncrementSpend.
	roleCat := policy.BudgetCategory{Source: "role", ID: "eng", ResetCadence: "monthly"}
	grantCat := policy.BudgetCategory{Source: "grant", ID: "g1", ResetCadence: "monthly"}
	eval := &stubEvaluator{
		budgetCategories: []policy.BudgetCategory{grantCat},
		roleBudgets:      []policy.BudgetCategory{roleCat},
	}
	counter := &mockSpendCounter{}
	svc := newBudgetService(eval, counter)

	svc.incrementSpend(ctxWithRoles(t, "eng"), "tenant-1", "user-1", 1.25)

	if len(eval.lastRoleBudgetRoles) != 1 || eval.lastRoleBudgetRoles[0] != "eng" {
		t.Errorf("GetRoleBudgets called with roles = %v, want [eng]", eval.lastRoleBudgetRoles)
	}
	if len(counter.lastCategories) != 2 {
		t.Fatalf("IncrementSpend received %d categories, want 2 (grant + role)", len(counter.lastCategories))
	}
	if counter.lastCategories[0] != grantCat || counter.lastCategories[1] != roleCat {
		t.Errorf("IncrementSpend categories = %v, want [%v %v]", counter.lastCategories, grantCat, roleCat)
	}
	if counter.spend != 1.25 {
		t.Errorf("counter spend = %v, want 1.25", counter.spend)
	}
}
