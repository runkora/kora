package bundle_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/clear-route/orbit/internal/policyservice/bundle"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	opabundle "github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
)

// ---------------------------------------------------------------------------
// orbit.budget Rego rule tests
// ---------------------------------------------------------------------------

// compileAndPrepareBudgetQuery compiles a real bundle (identical to production)
// and prepares it for evaluating the orbit.budget rules.
func compileAndPrepareBudgetQuery(t *testing.T, grantList []*grants.Grant, roleList []*roles.RoleEntry) rego.PreparedEvalQuery {
	t.Helper()

	cfg := bundle.Config{
		JWKSURL:  "http://127.0.0.1:1/jwks",
		Issuer:   "orbit",
		Audience: "orbit-platform",
	}
	compiler := bundle.NewCompiler(cfg, routes.NewStore())
	cached, err := compiler.Compile(testTenantID, grantList, nil, roleList)
	if err != nil {
		t.Fatalf("compile bundle: %v", err)
	}

	b, err := opabundle.NewReader(bytes.NewReader(cached.Data)).Read()
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}

	pq, err := rego.New(
		rego.Query(`x := {
			"budget_allow":        data.orbit.budget.budget_allow,
			"spend_limit_reached": data.orbit.budget.spend_limit_reached,
			"has_limit":           data.orbit.budget.has_limit,
			"effective_budget_usd": data.orbit.budget.effective_budget_usd
		}`),
		rego.ParsedBundle(testTenantID, &b),
	).PrepareForEval(context.Background())
	if err != nil {
		t.Fatalf("prepare rego: %v", err)
	}
	return pq
}

func evalBudget(t *testing.T, pq rego.PreparedEvalQuery, principalID string, currentSpendUSD float64) (allow bool, limitReached bool, hasLimit bool, effectiveBudget float64) {
	t.Helper()
	return evalBudgetWithRoles(t, pq, principalID, nil, currentSpendUSD)
}

func evalBudgetWithRoles(t *testing.T, pq rego.PreparedEvalQuery, principalID string, principalRoles []string, currentSpendUSD float64) (allow bool, limitReached bool, hasLimit bool, effectiveBudget float64) {
	t.Helper()
	if principalRoles == nil {
		principalRoles = []string{}
	}
	input := map[string]any{
		"principal":         map[string]any{"id": principalID, "roles": principalRoles},
		"current_spend_usd": currentSpendUSD,
	}
	rs, err := pq.Eval(context.Background(), rego.EvalInput(input))
	if err != nil {
		t.Fatalf("rego eval: %v", err)
	}
	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		t.Fatal("rego eval: empty result set — check query and bundle compilation")
	}
	d, ok := rs[0].Bindings["x"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected binding shape: %T", rs[0].Bindings["x"])
	}
	allow, _ = d["budget_allow"].(bool)
	limitReached, _ = d["spend_limit_reached"].(bool)
	hasLimit, _ = d["has_limit"].(bool)
	// OPA returns numbers as json.Number in object bindings — extract via float64.
	if n, ok := d["effective_budget_usd"].(json.Number); ok {
		effectiveBudget, _ = n.Float64()
	}
	return allow, limitReached, hasLimit, effectiveBudget
}

// humanGrantWithBudget builds a human grant carrying a budget assignment.
func humanGrantWithBudget(principalID, grantID string, monthlyLimitUSD float64) *grants.Grant {
	return &grants.Grant{
		ID:            grantID,
		TenantID:      testTenantID,
		PrincipalType: grants.PrincipalTypeHuman,
		PrincipalID:   principalID,
		Scopes:        []string{"inference:invoke"},
		Budget: &grants.BudgetAssignment{
			MonthlyLimitUSD: monthlyLimitUSD,
			ResetCadence:    "monthly",
		},
	}
}

func TestBudgetAllow_DefaultFailOpen(t *testing.T) {
	// Principal has no budget configured — should always allow regardless of spend.
	// has_limit must be false and effective_budget_usd must be 0.
	pq := compileAndPrepareBudgetQuery(t, nil, nil)

	allow, limitReached, hasLimit, effectiveBudget := evalBudget(t, pq, "unknown-principal", 9999.99)
	if !allow {
		t.Error("budget_allow = false, want true (no budget configured → fail-open)")
	}
	if limitReached {
		t.Error("spend_limit_reached = true, want false (no budget configured)")
	}
	if hasLimit {
		t.Error("has_limit = true, want false (no budget entry in bundle)")
	}
	if effectiveBudget != 0 {
		t.Errorf("effective_budget_usd = %v, want 0 (no budget configured)", effectiveBudget)
	}
}

func TestBudgetAllow_PrincipalAbsentFromMap(t *testing.T) {
	// Bundle has a budget for principal A; principal B should still fail-open.
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{humanGrantWithBudget("pid-a", "grant-1", 100)},
		nil,
	)

	allow, limitReached, hasLimit, effectiveBudget := evalBudget(t, pq, "pid-b", 50)
	if !allow {
		t.Error("budget_allow = false for principal absent from budget map, want true")
	}
	if limitReached {
		t.Error("spend_limit_reached = true for principal absent from budget map, want false")
	}
	if hasLimit {
		t.Error("has_limit = true for principal absent from budget map, want false")
	}
	if effectiveBudget != 0 {
		t.Errorf("effective_budget_usd = %v, want 0 for absent principal", effectiveBudget)
	}
}

func TestBudgetAllow_UnderLimit(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{humanGrantWithBudget("pid-1", "grant-1", 100)},
		nil,
	)

	allow, limitReached, hasLimit, effectiveBudget := evalBudget(t, pq, "pid-1", 49.99)
	if !allow {
		t.Errorf("budget_allow = false at spend 49.99 / limit 100, want true")
	}
	if limitReached {
		t.Error("spend_limit_reached = true, want false")
	}
	if !hasLimit {
		t.Error("has_limit = false, want true (principal has a budget entry)")
	}
	if effectiveBudget != 100 {
		t.Errorf("effective_budget_usd = %v, want 100", effectiveBudget)
	}
}

func TestBudgetAllow_AtExactLimit(t *testing.T) {
	// The rule is >=, so spend == limit must deny.
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{humanGrantWithBudget("pid-1", "grant-1", 50)},
		nil,
	)

	allow, limitReached, hasLimit, _ := evalBudget(t, pq, "pid-1", 50)
	if allow {
		t.Error("budget_allow = true at spend == limit (50), want false (>= semantics)")
	}
	if !limitReached {
		t.Error("spend_limit_reached = false at spend == limit, want true")
	}
	if !hasLimit {
		t.Error("has_limit = false, want true")
	}
}

func TestBudgetAllow_OverLimit(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{humanGrantWithBudget("pid-1", "grant-1", 50)},
		nil,
	)

	allow, limitReached, hasLimit, _ := evalBudget(t, pq, "pid-1", 75)
	if allow {
		t.Error("budget_allow = true at spend 75 > limit 50, want false")
	}
	if !limitReached {
		t.Error("spend_limit_reached = false, want true")
	}
	if !hasLimit {
		t.Error("has_limit = false, want true")
	}
}

func TestBudgetAllow_MAXEffectiveLimitApplied(t *testing.T) {
	// Two grants for the same principal: $50 and $200. Effective limit = $200.
	// Spend at $150 should allow (under max) even though it exceeds the lower grant.
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{
			humanGrantWithBudget("pid-1", "grant-a", 50),
			humanGrantWithBudget("pid-1", "grant-b", 200),
		},
		nil,
	)

	allow, _, hasLimit, effectiveBudget := evalBudget(t, pq, "pid-1", 150)
	if !allow {
		t.Errorf("budget_allow = false at spend 150 with effective limit 200, want true")
	}
	if !hasLimit {
		t.Error("has_limit = false, want true")
	}
	if effectiveBudget != 200 {
		t.Errorf("effective_budget_usd = %v, want 200 (MAX of 50, 200)", effectiveBudget)
	}
}

func TestBudgetAllow_EffectiveBudgetUSDPopulated(t *testing.T) {
	// Verify effective_budget_usd is populated correctly for a principal with a limit.
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{humanGrantWithBudget("pid-1", "grant-1", 75.50)},
		nil,
	)

	_, _, hasLimit, effectiveBudget := evalBudget(t, pq, "pid-1", 0)
	if !hasLimit {
		t.Error("has_limit = false, want true")
	}
	if effectiveBudget != 75.50 {
		t.Errorf("effective_budget_usd = %v, want 75.50", effectiveBudget)
	}
}

// roleEntryWithBudget builds a role entry carrying a budget assignment.
func roleEntryWithBudget(role string, monthlyLimitUSD float64) *roles.RoleEntry {
	return &roles.RoleEntry{
		TenantID: testTenantID,
		Role:     role,
		Scopes:   []string{"inference:invoke"},
		Budget: &roles.BudgetAssignment{
			MonthlyLimitUSD: monthlyLimitUSD,
			ResetCadence:    "monthly",
		},
	}
}

func TestBudgetAllow_RoleOnlyBudget_Enforced(t *testing.T) {
	// Principal has no grant budget — only an OIDC role budget, resolved
	// against input.principal.roles. has_limit must be true and the limit
	// must enforce with >= semantics.
	pq := compileAndPrepareBudgetQuery(t, nil, []*roles.RoleEntry{roleEntryWithBudget("engineer", 50)})

	allow, _, hasLimit, effectiveBudget := evalBudgetWithRoles(t, pq, "pid-1", []string{"engineer"}, 49.99)
	if !allow {
		t.Error("budget_allow = false under role limit, want true")
	}
	if !hasLimit {
		t.Error("has_limit = false for role-only budget, want true")
	}
	if effectiveBudget != 50 {
		t.Errorf("effective_budget_usd = %v, want 50 (role budget)", effectiveBudget)
	}

	allow, limitReached, _, _ := evalBudgetWithRoles(t, pq, "pid-1", []string{"engineer"}, 50)
	if allow {
		t.Error("budget_allow = true at spend == role limit, want false (>= semantics)")
	}
	if !limitReached {
		t.Error("spend_limit_reached = false at spend == role limit, want true")
	}
}

func TestBudgetAllow_RoleAndGrant_MAXAcrossSources(t *testing.T) {
	// Role budget $50, grant budget $80 → effective = MAX = 80.
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{humanGrantWithBudget("pid-1", "grant-1", 80)},
		[]*roles.RoleEntry{roleEntryWithBudget("engineer", 50)},
	)

	allow, _, hasLimit, effectiveBudget := evalBudgetWithRoles(t, pq, "pid-1", []string{"engineer"}, 65)
	if !allow {
		t.Error("budget_allow = false at spend 65 with effective limit 80, want true")
	}
	if !hasLimit {
		t.Error("has_limit = false, want true")
	}
	if effectiveBudget != 80 {
		t.Errorf("effective_budget_usd = %v, want 80 (MAX of role 50, grant 80)", effectiveBudget)
	}

	allow, _, _, _ = evalBudgetWithRoles(t, pq, "pid-1", []string{"engineer"}, 80)
	if allow {
		t.Error("budget_allow = true at spend == effective limit 80, want false")
	}
}

func TestBudgetAllow_RoleWithoutBudget_FailsOpen(t *testing.T) {
	// Caller holds a role that has no budget configured — fail-open.
	pq := compileAndPrepareBudgetQuery(t, nil, []*roles.RoleEntry{
		{TenantID: testTenantID, Role: "analyst", Scopes: []string{"inference:invoke"}},
	})

	allow, _, hasLimit, effectiveBudget := evalBudgetWithRoles(t, pq, "pid-1", []string{"analyst"}, 9999)
	if !allow {
		t.Error("budget_allow = false for role without budget, want true (fail-open)")
	}
	if hasLimit {
		t.Error("has_limit = true for role without budget, want false")
	}
	if effectiveBudget != 0 {
		t.Errorf("effective_budget_usd = %v, want 0", effectiveBudget)
	}
}

func TestBudgetAllow_RoleBudget_OnlyHeldRolesApply(t *testing.T) {
	// A role budget exists for "engineer", but the caller holds only "analyst" —
	// the engineer budget must not apply.
	pq := compileAndPrepareBudgetQuery(t, nil, []*roles.RoleEntry{roleEntryWithBudget("engineer", 50)})

	allow, _, hasLimit, _ := evalBudgetWithRoles(t, pq, "pid-1", []string{"analyst"}, 9999)
	if !allow {
		t.Error("budget_allow = false for non-held role budget, want true")
	}
	if hasLimit {
		t.Error("has_limit = true for non-held role budget, want false")
	}
}

func TestBudgetAllow_SpendLimitReachedInverseOfAllow(t *testing.T) {
	// spend_limit_reached must always be the logical inverse of budget_allow.
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{humanGrantWithBudget("pid-1", "grant-1", 100)},
		nil,
	)

	cases := []struct {
		spend    float64
		wantDeny bool
	}{
		{0, false},
		{99.99, false},
		{100, true},
		{200, true},
	}
	for _, tc := range cases {
		allow, limitReached, _, _ := evalBudget(t, pq, "pid-1", tc.spend)
		if allow == limitReached {
			t.Errorf("spend %.2f: budget_allow=%v and spend_limit_reached=%v must differ", tc.spend, allow, limitReached)
		}
		if tc.wantDeny && allow {
			t.Errorf("spend %.2f: expected deny, got allow", tc.spend)
		}
		if !tc.wantDeny && !allow {
			t.Errorf("spend %.2f: expected allow, got deny", tc.spend)
		}
	}
}
