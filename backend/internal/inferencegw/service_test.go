package inferencegw

import (
	"context"
	"errors"
	"fmt"
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
	// budgetEffectiveUSD is the monthly principal ceiling limit reported by CheckBudget.
	budgetEffectiveUSD float64
	// budgetErr, when non-nil, is returned by CheckBudget.
	budgetErr error
	// lastBudgetReq captures the request passed to CheckBudget (nil = not called).
	lastBudgetReq *policy.BudgetCheckRequest
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
	// tenantBudget is returned by GetTenantBudget.
	tenantBudget *policy.TenantBudget
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

func (s *stubEvaluator) CheckBudget(_ context.Context, _ string, req policy.BudgetCheckRequest) (policy.BudgetResult, error) {
	s.lastBudgetReq = &req
	if s.budgetErr != nil {
		return policy.BudgetResult{}, s.budgetErr
	}
	allow := true
	if s.checkAllow != nil {
		allow = *s.checkAllow
	}
	res := policy.BudgetResult{Allow: allow, HasLimit: s.budgetHasLimit}
	if s.budgetHasLimit {
		res.Ceilings = []policy.BudgetCeiling{{
			Scope:    "principal",
			ID:       req.PrincipalID,
			Window:   "monthly",
			LimitUSD: s.budgetEffectiveUSD,
			SpendUSD: req.Spends.Principal["monthly"],
		}}
	}
	return res, nil
}

func (s *stubEvaluator) GetTenantBudget(_ context.Context, _ string) (*policy.TenantBudget, error) {
	return s.tenantBudget, nil
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

func TestCheckModelPolicy_UnknownModel_ReturnsModelNotFound(t *testing.T) {
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
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound for unknown model, got: %v", err)
	}
	// Must NOT look like a policy denial to the caller.
	if errors.Is(err, ErrPolicyDenied) {
		t.Error("unknown model must not return ErrPolicyDenied")
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
	// Regression guard: a real authz denial for a known model must still be
	// policy_denied / 403, not leaked as a model-not-found 404.
	if !errors.Is(err, ErrPolicyDenied) {
		t.Errorf("expected ErrPolicyDenied for denied virtual model, got: %v", err)
	}
	if errors.Is(err, ErrModelNotFound) {
		t.Error("authz denial for a known model must not return ErrModelNotFound")
	}
}

// modelPolicyFailure is what each request handler uses to decide the usage
// status/code recorded when checkModelPolicy fails. An unknown model must be
// recorded as model_not_found, not policy_denied, so telemetry and billing do
// not misattribute a client typo as an authorization failure.
func TestModelPolicyFailure(t *testing.T) {
	t.Parallel()
	t.Run("unknown model", func(t *testing.T) {
		err := fmt.Errorf("%w: %q", ErrModelNotFound, "bogus-model")
		status, code, msg := modelPolicyFailure(err)
		if status != UsageStatusModelNotFound || code != "MODEL_NOT_FOUND" {
			t.Errorf("unknown model: got (%s, %s), want (model_not_found, MODEL_NOT_FOUND)", status, code)
		}
		if !strings.Contains(msg, "bogus-model") {
			t.Errorf("span message should name the model, got: %q", msg)
		}
	})
	t.Run("policy denial", func(t *testing.T) {
		status, code, msg := modelPolicyFailure(ErrPolicyDenied)
		if status != UsageStatusPolicyDenied || code != "POLICY_DENIED" {
			t.Errorf("denial: got (%s, %s), want (policy_denied, POLICY_DENIED)", status, code)
		}
		if msg != ErrPolicyDenied.Error() {
			t.Errorf("span message: got %q, want %q", msg, ErrPolicyDenied.Error())
		}
	})
	t.Run("wrapped policy-engine error stays policy_denied", func(t *testing.T) {
		status, code, _ := modelPolicyFailure(fmt.Errorf("policy check: %w", errors.New("boom")))
		if status != UsageStatusPolicyDenied || code != "POLICY_DENIED" {
			t.Errorf("engine error: got (%s, %s), want (policy_denied, POLICY_DENIED)", status, code)
		}
	})
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

func monthlySpends(usd float64) policy.BudgetSpends {
	return policy.BudgetSpends{Principal: map[string]float64{"monthly": usd}}
}

func TestGetPersonalBudget_SpendThreadedIntoCheckBudget(t *testing.T) {
	t.Parallel()
	// Verify the spends read from the counter are the exact values passed to CheckBudget.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spends: monthlySpends(42.5)}
	svc := newBudgetService(eval, counter)

	status, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetReq == nil || eval.lastBudgetReq.Spends.Principal["monthly"] != 42.5 {
		t.Errorf("CheckBudget received spends = %+v, want monthly 42.5 (from counter)", eval.lastBudgetReq)
	}
	if status.CurrentSpendUSD != 42.5 {
		t.Errorf("CurrentSpendUSD = %v, want 42.5", status.CurrentSpendUSD)
	}
	if len(status.Ceilings) != 1 || status.Ceilings[0].Window != "monthly" || status.Ceilings[0].LimitUSD != 50 {
		t.Errorf("Ceilings = %+v, want one monthly ceiling of 50", status.Ceilings)
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
	// CheckBudget must not have been called when GetSpends failed.
	if eval.lastBudgetReq != nil {
		t.Error("CheckBudget should not be called when GetSpends errors")
	}
}

func TestGetPersonalBudget_OPAError_FailsOpen(t *testing.T) {
	t.Parallel()
	// OPA error → fail-open: empty BudgetStatus, no error returned.
	eval := &stubEvaluator{budgetErr: errors.New("OPA unavailable")}
	counter := &mockSpendCounter{spends: monthlySpends(10)}
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
	counter := &mockSpendCounter{spends: monthlySpends(30)}
	svc := newBudgetService(eval, counter)

	status, err := svc.GetPersonalBudget(ctxWithRoles(t, "eng", "power-user"), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetReq == nil {
		t.Fatal("CheckBudget was not called")
	}
	got := eval.lastBudgetReq.Roles
	want := []string{"eng", "power-user"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("CheckBudget received roles = %v, want %v", got, want)
	}
	if status.CurrentSpendUSD != 30.0 {
		t.Errorf("CurrentSpendUSD = %v, want 30.0", status.CurrentSpendUSD)
	}
	if !status.HasLimit {
		t.Error("HasLimit should be true")
	}
}

func TestGetPersonalBudget_ResolvesRolesFromSnapshotWhenCredentialHasNone(t *testing.T) {
	t.Parallel()
	// A PAT-attributed caller carries no roles claim. Roles must be resolved
	// from the user_profiles snapshot so role budgets attribute and enforce.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spends: monthlySpends(30)}
	usage := &mockInferenceUsageRepository{principalRoles: map[string][]string{"tenant-1\x00user-1": {"engineering"}}}
	svc := NewService(eval, buildTestRouter(nil), NewNopPricingCache(), usage, counter, logging.NewLoggerWithOutput(io.Discard))

	if _, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetReq == nil {
		t.Fatal("CheckBudget was not called")
	}
	if got := eval.lastBudgetReq.Roles; len(got) != 1 || got[0] != "engineering" {
		t.Errorf("CheckBudget roles = %v, want [engineering] resolved from snapshot", got)
	}
}

func TestGetPersonalBudget_DeduplicatesResolvedRoles(t *testing.T) {
	t.Parallel()
	// The login flow merges OIDC roles + groups, so the snapshot can contain a
	// duplicate. It must be collapsed before reaching budget evaluation, or a
	// pooled role budget would double-count.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spends: monthlySpends(30)}
	usage := &mockInferenceUsageRepository{principalRoles: map[string][]string{"tenant-1\x00user-1": {"engineering", "engineering", "users"}}}
	svc := NewService(eval, buildTestRouter(nil), NewNopPricingCache(), usage, counter, logging.NewLoggerWithOutput(io.Discard))

	if _, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetReq == nil {
		t.Fatal("CheckBudget was not called")
	}
	got := eval.lastBudgetReq.Roles
	if len(got) != 2 || got[0] != "engineering" || got[1] != "users" {
		t.Errorf("CheckBudget roles = %v, want de-duplicated [engineering users]", got)
	}
}

func TestGetPersonalBudget_SkipsRoleLookupForAgentPrincipals(t *testing.T) {
	t.Parallel()
	// SPIFFE (agent) principals have no user_profiles row; the snapshot lookup
	// must be skipped, not issued as a wasted query, and no roles resolved.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spends: monthlySpends(30)}
	usage := &mockInferenceUsageRepository{principalRoles: map[string][]string{"tenant-1\x00spiffe://td/agent/x": {"should-not-be-read"}}}
	svc := NewService(eval, buildTestRouter(nil), NewNopPricingCache(), usage, counter, logging.NewLoggerWithOutput(io.Discard))

	if _, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "spiffe://td/agent/x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetReq == nil {
		t.Fatal("CheckBudget was not called")
	}
	if got := eval.lastBudgetReq.Roles; len(got) != 0 {
		t.Errorf("CheckBudget roles = %v, want none (agent lookup skipped)", got)
	}
}

func TestGetPersonalBudget_LiveJWTRolesWinOverSnapshot(t *testing.T) {
	t.Parallel()
	// An interactive session carries live roles. Those win — the snapshot is a
	// fallback only for credentials that carry none.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spends: monthlySpends(30)}
	usage := &mockInferenceUsageRepository{principalRoles: map[string][]string{"tenant-1\x00user-1": {"stale-snapshot-role"}}}
	svc := NewService(eval, buildTestRouter(nil), NewNopPricingCache(), usage, counter, logging.NewLoggerWithOutput(io.Discard))

	if _, err := svc.GetPersonalBudget(ctxWithRoles(t, "live-role"), "tenant-1", "user-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetReq == nil {
		t.Fatal("CheckBudget was not called")
	}
	if got := eval.lastBudgetReq.Roles; len(got) != 1 || got[0] != "live-role" {
		t.Errorf("CheckBudget roles = %v, want [live-role] (live JWT roles win over snapshot)", got)
	}
}

func TestGetPersonalBudget_NoRolesInJWT_EmptyRolesToCheckBudget(t *testing.T) {
	t.Parallel()
	// JWT has no roles claim — CheckBudget receives no roles (grant budgets only).
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spends: monthlySpends(10)}
	svc := newBudgetService(eval, counter)

	_, err := svc.GetPersonalBudget(ctxWithClaims(t), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.lastBudgetReq == nil || len(eval.lastBudgetReq.Roles) != 0 {
		t.Errorf("CheckBudget should receive no roles when JWT has none, got %+v", eval.lastBudgetReq)
	}
}

func TestCheckBudget_ScopesDerivedFromConfiguration(t *testing.T) {
	t.Parallel()
	// Grant budgets (weekly + monthly + per-run), a pooled role, a per-member
	// role, and a tenant cap — verify the counter is asked for exactly the
	// right scopes.
	eval := &stubEvaluator{
		budgetCategories: []policy.BudgetCategory{
			{Source: "grant", ID: "g-week", ResetCadence: "weekly", LimitUSD: 20},
			{Source: "grant", ID: "g-run", ResetCadence: "monthly", PerRunLimitUSD: 5},
		},
		roleBudgets: []policy.BudgetCategory{
			{Source: "role", ID: "engineering", ResetCadence: "monthly", LimitUSD: 500, Pooled: true},
			{Source: "role", ID: "engineer", ResetCadence: "monthly", LimitUSD: 50},
		},
		tenantBudget: &policy.TenantBudget{LimitUSD: 2000, ResetCadence: "monthly"},
	}
	counter := &mockSpendCounter{}
	svc := newBudgetService(eval, counter)

	_, err := svc.checkBudget(ctxWithRoles(t, "engineering", "engineer"), principal{
		TenantID: "tenant-1", PrincipalID: "user-1", PrincipalType: "HUMAN",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc := counter.lastScopes
	if len(sc.PrincipalWindows) != 2 || sc.PrincipalWindows[0] != "monthly" || sc.PrincipalWindows[1] != "weekly" {
		t.Errorf("PrincipalWindows = %v, want [monthly weekly]", sc.PrincipalWindows)
	}
	if sc.TenantWindow != "monthly" {
		t.Errorf("TenantWindow = %q, want monthly", sc.TenantWindow)
	}
	if len(sc.PoolWindows) != 1 || sc.PoolWindows["engineering"] != "monthly" {
		t.Errorf("PoolWindows = %v, want engineering→monthly", sc.PoolWindows)
	}
	if sc.RunID != "" {
		t.Errorf("RunID = %q, want empty (no run context on this request)", sc.RunID)
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
					{Source: "grant", ID: "g1", ResetCadence: "monthly", LimitUSD: 80},
				},
				EffectiveLimits: map[string]float64{"monthly": 80},
			},
		},
		allRoleBudgets: map[string]policy.RoleBudget{
			"engineer": {LimitUSD: 50, ResetCadence: "monthly"},
		},
	}
	counter := &mockSpendCounter{}
	usage := &mockInferenceUsageRepository{
		periodSpends: []PrincipalPeriodSpend{
			{PrincipalID: "pid-grant", PrincipalType: "HUMAN", SpendUSD: 40},
			{PrincipalID: "pid-role", PrincipalType: "HUMAN", SpendUSD: 50},
			{PrincipalID: "pid-free", PrincipalType: "AGENT", SpendUSD: 10},
		},
		activeRoleHolders: map[string][]string{"pid-role": {"engineer"}},
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

func TestListBudgetPrincipals_WeeklyBlockedReportedAndTypeInferred(t *testing.T) {
	t.Parallel()
	// An agent with only a WEEKLY grant budget ($20), exhausted per the usage
	// log's ISO-week spend, and no usage row this month (so principal_type is
	// unrecorded). It must be reported blocked with a non-zero effective limit
	// and inferred type AGENT. The attribution counter is deliberately stale
	// (zero) — weekly spend must come from the usage log, matching enforcement,
	// so a freshly re-windowed assignment never reads as zero usage.
	pid := "spiffe://orbit.test/tenant/t/agent/coder"
	eval := &stubEvaluator{
		allBudgets: map[string]policy.PrincipalBudget{
			pid: {
				Categories:      []policy.BudgetCategory{{Source: "grant", ID: "g1", ResetCadence: "weekly", LimitUSD: 20}},
				EffectiveLimits: map[string]float64{"weekly": 20},
			},
		},
	}
	counter := &mockSpendCounter{categorySpends: []float64{0}} // stale attribution counter
	usage := &mockInferenceUsageRepository{
		windowSpends: map[string]float64{pid: 20}, // true ISO-week spend
	}
	svc := NewService(eval, buildTestRouter(nil), NewNopPricingCache(), usage, counter, logging.NewLoggerWithOutput(io.Discard))

	resp, err := svc.ListBudgetPrincipals(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(resp.Data), resp.Data)
	}
	row := resp.Data[0]
	if row.EffectiveLimitUSD != 20 {
		t.Errorf("effective limit = %v, want 20 (weekly limit, not monthly-only zero)", row.EffectiveLimitUSD)
	}
	if !row.SpendLimitReached {
		t.Error("weekly ceiling exhausted must report SpendLimitReached=true")
	}
	if len(row.Categories) != 1 || row.Categories[0].SpendUSD != 20 {
		t.Errorf("weekly category = %+v, want spend_usd 20 from the usage log (not the stale counter)", row.Categories)
	}
	if row.PrincipalType != "AGENT" {
		t.Errorf("principal type = %q, want AGENT (inferred from SPIFFE id)", row.PrincipalType)
	}
	if row.EffectiveResetCadence != "weekly" {
		t.Errorf("effective_reset_cadence = %q, want \"weekly\"", row.EffectiveResetCadence)
	}
	if row.EffectiveSpendUSD != 20 {
		t.Errorf("effective_spend_usd = %v, want 20 (ISO-week spend, not monthly)", row.EffectiveSpendUSD)
	}
}

func TestListBudgetPrincipals_RoleCounterForRemovedBudgetIgnored(t *testing.T) {
	t.Parallel()
	// An attribution counter exists for a role whose budget has since been
	// removed — the principal must not be reported as budgeted.
	eval := &stubEvaluator{allRoleBudgets: map[string]policy.RoleBudget{}}
	counter := &mockSpendCounter{}
	usage := &mockInferenceUsageRepository{
		activeRoleHolders: map[string][]string{"pid-1": {"engineer"}},
	}
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
	if counter.incremented != 1.25 {
		t.Errorf("counter incremented = %v, want 1.25", counter.incremented)
	}
}
