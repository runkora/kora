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
	"github.com/clear-route/orbit/internal/policyservice/tenantbudget"
	opabundle "github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
)

// ---------------------------------------------------------------------------
// orbit.budget Rego rule tests (hierarchical ceilings)
// ---------------------------------------------------------------------------

// compileAndPrepareBudgetQuery compiles a real bundle (identical to production)
// and prepares it for evaluating the orbit.budget rules.
func compileAndPrepareBudgetQuery(t *testing.T, grantList []*grants.Grant, roleList []*roles.RoleEntry, tb *tenantbudget.TenantBudget) rego.PreparedEvalQuery {
	t.Helper()

	cfg := bundle.Config{
		JWKSURL:  "http://127.0.0.1:1/jwks",
		Issuer:   "orbit",
		Audience: "orbit-platform",
	}
	compiler := bundle.NewCompiler(cfg, routes.NewStore())
	cached, err := compiler.Compile(testTenantID, grantList, nil, roleList, tb, nil)
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
			"ceilings":            data.orbit.budget.ceilings
		}`),
		rego.ParsedBundle(testTenantID, &b),
	).PrepareForEval(context.Background())
	if err != nil {
		t.Fatalf("prepare rego: %v", err)
	}
	return pq
}

// budgetEvalInput mirrors the OPA input document the gateway constructs.
type budgetEvalInput struct {
	principalID   string
	principalType string
	roles         []string
	runID         string
	// delegatorID mirrors input.delegation.delegator_id — the VERIFIED
	// delegation context ("" = not delegated).
	delegatorID string
	// spends, keyed exactly as the gateway sends them.
	principalSpends map[string]float64 // window → usd
	delegatorSpends map[string]float64 // window → usd (delegator's counters)
	rolePoolSpends  map[string]float64 // role → usd
	tenantSpends    map[string]float64 // window → usd
	runSpend        float64
}

type budgetVerdict struct {
	allow        bool
	limitReached bool
	hasLimit     bool
	ceilings     []map[string]any
}

// num extracts a float from an OPA result value (float64 or json.Number).
func num(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			t.Fatalf("non-numeric json.Number %q", n)
		}
		return f
	default:
		t.Fatalf("unexpected numeric type %T", v)
		return 0
	}
}

func evalBudget(t *testing.T, pq rego.PreparedEvalQuery, in budgetEvalInput) budgetVerdict {
	t.Helper()
	if in.principalType == "" {
		in.principalType = "HUMAN"
	}
	if in.roles == nil {
		in.roles = []string{}
	}
	spends := map[string]any{
		"principal":  in.principalSpends,
		"role_pools": in.rolePoolSpends,
		"tenant":     in.tenantSpends,
		"run":        in.runSpend,
	}
	if in.delegatorSpends != nil {
		spends["delegator"] = in.delegatorSpends
	}
	input := map[string]any{
		"principal": map[string]any{
			"id":        in.principalID,
			"tenant_id": testTenantID,
			"type":      in.principalType,
			"roles":     in.roles,
			"run_id":    in.runID,
		},
		"spends": spends,
	}
	if in.delegatorID != "" {
		input["delegation"] = map[string]any{"delegator_id": in.delegatorID}
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
	v := budgetVerdict{}
	v.allow, _ = d["budget_allow"].(bool)
	v.limitReached, _ = d["spend_limit_reached"].(bool)
	v.hasLimit, _ = d["has_limit"].(bool)
	if raw, ok := d["ceilings"].([]any); ok {
		for _, c := range raw {
			if m, ok := c.(map[string]any); ok {
				v.ceilings = append(v.ceilings, m)
			}
		}
	}
	return v
}

// ── fixtures ─────────────────────────────────────────────────────────────────

func grantBudget(principalID, grantID string, limitUSD float64, cadence string) *grants.Grant {
	return &grants.Grant{
		ID:            grantID,
		TenantID:      testTenantID,
		PrincipalType: grants.PrincipalTypeHuman,
		PrincipalID:   principalID,
		Scopes:        []string{"inference:invoke"},
		Budget:        &grants.BudgetAssignment{LimitUSD: limitUSD, ResetCadence: cadence},
	}
}

func grantPerRunBudget(principalID, grantID string, perRunUSD float64) *grants.Grant {
	g := grantBudget(principalID, grantID, 0, "monthly")
	g.PrincipalType = grants.PrincipalTypeAgent
	g.Budget = &grants.BudgetAssignment{PerRunLimitUSD: perRunUSD, ResetCadence: "monthly"}
	return g
}

func roleBudget(role string, limitUSD float64, cadence string, pooled bool) *roles.RoleEntry {
	return &roles.RoleEntry{
		TenantID: testTenantID,
		Role:     role,
		Scopes:   []string{"inference:invoke"},
		Budget:   &roles.BudgetAssignment{LimitUSD: limitUSD, ResetCadence: cadence, Pooled: pooled},
	}
}

func tenantCap(limitUSD float64, cadence string) *tenantbudget.TenantBudget {
	return &tenantbudget.TenantBudget{TenantID: testTenantID, LimitUSD: limitUSD, ResetCadence: cadence}
}

// ── fail-open defaults ───────────────────────────────────────────────────────

func TestBudget_NoConfiguration_FailsOpen(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, nil, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{principalID: "anyone", principalSpends: map[string]float64{"monthly": 9999}})
	if !v.allow || v.limitReached || v.hasLimit || len(v.ceilings) != 0 {
		t.Errorf("no configuration must fail open, got %+v", v)
	}
}

func TestBudget_PrincipalAbsentFromMap_FailsOpen(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{grantBudget("pid-a", "g1", 100, "monthly")}, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{principalID: "pid-b", principalSpends: map[string]float64{"monthly": 9999}})
	if !v.allow || v.hasLimit {
		t.Errorf("absent principal must fail open, got %+v", v)
	}
}

// ── principal window ceilings ────────────────────────────────────────────────

func TestBudget_MonthlyGrant_GreaterEqualSemantics(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{grantBudget("pid-1", "g1", 50, "monthly")}, nil, nil)

	under := evalBudget(t, pq, budgetEvalInput{principalID: "pid-1", principalSpends: map[string]float64{"monthly": 49.99}})
	if !under.allow || !under.hasLimit {
		t.Errorf("under limit must allow with has_limit, got %+v", under)
	}
	at := evalBudget(t, pq, budgetEvalInput{principalID: "pid-1", principalSpends: map[string]float64{"monthly": 50}})
	if at.allow || !at.limitReached {
		t.Errorf("spend == limit must deny (>= semantics), got %+v", at)
	}
}

func TestBudget_MAXWithinWindow(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{
		grantBudget("pid-1", "g-low", 50, "monthly"),
		grantBudget("pid-1", "g-high", 200, "monthly"),
	}, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{principalID: "pid-1", principalSpends: map[string]float64{"monthly": 150}})
	if !v.allow {
		t.Errorf("spend 150 under MAX(50,200) must allow, got %+v", v)
	}
	if len(v.ceilings) != 1 {
		t.Fatalf("expected one monthly ceiling, got %v", v.ceilings)
	}
	if num(t, v.ceilings[0]["limit_usd"]) != 200 {
		t.Errorf("effective monthly limit = %v, want 200", v.ceilings[0]["limit_usd"])
	}
}

func TestBudget_WeeklyCeiling_Enforced(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{grantBudget("pid-1", "g1", 20, "weekly")}, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{principalID: "pid-1", principalSpends: map[string]float64{"weekly": 20, "monthly": 5}})
	if v.allow {
		t.Errorf("weekly spend at weekly limit must deny, got %+v", v)
	}
}

func TestBudget_WeeklyAndMonthly_AreIndependentCeilings(t *testing.T) {
	// Weekly $20 from one grant, monthly $100 from another: breaching EITHER denies.
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{
		grantBudget("pid-1", "g-week", 20, "weekly"),
		grantBudget("pid-1", "g-month", 100, "monthly"),
	}, nil, nil)

	weeklyBreach := evalBudget(t, pq, budgetEvalInput{principalID: "pid-1", principalSpends: map[string]float64{"weekly": 20, "monthly": 30}})
	if weeklyBreach.allow {
		t.Errorf("weekly breach must deny even when monthly is fine, got %+v", weeklyBreach)
	}
	bothUnder := evalBudget(t, pq, budgetEvalInput{principalID: "pid-1", principalSpends: map[string]float64{"weekly": 10, "monthly": 30}})
	if !bothUnder.allow {
		t.Errorf("both windows under their limits must allow, got %+v", bothUnder)
	}
	if len(bothUnder.ceilings) != 2 {
		t.Errorf("expected two ceilings (weekly + monthly), got %v", bothUnder.ceilings)
	}
}

// ── role budgets: per-member and pooled ─────────────────────────────────────

func TestBudget_PerMemberRoleBudget_Enforced(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, nil, []*roles.RoleEntry{roleBudget("engineer", 50, "monthly", false)}, nil)

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "pid-1", roles: []string{"engineer"},
		principalSpends: map[string]float64{"monthly": 50},
	})
	if v.allow || !v.hasLimit {
		t.Errorf("member at per-member role limit must deny, got %+v", v)
	}

	nonMember := evalBudget(t, pq, budgetEvalInput{
		principalID: "pid-1", roles: []string{"analyst"},
		principalSpends: map[string]float64{"monthly": 9999},
	})
	if !nonMember.allow || nonMember.hasLimit {
		t.Errorf("non-member must fail open, got %+v", nonMember)
	}
}

func TestBudget_RoleAndGrant_MAXAcrossSourcesSameWindow(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t,
		[]*grants.Grant{grantBudget("pid-1", "g1", 80, "monthly")},
		[]*roles.RoleEntry{roleBudget("engineer", 50, "monthly", false)},
		nil,
	)

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "pid-1", roles: []string{"engineer"},
		principalSpends: map[string]float64{"monthly": 65},
	})
	if !v.allow {
		t.Errorf("spend 65 under MAX(role 50, grant 80) must allow, got %+v", v)
	}
	at := evalBudget(t, pq, budgetEvalInput{
		principalID: "pid-1", roles: []string{"engineer"},
		principalSpends: map[string]float64{"monthly": 80},
	})
	if at.allow {
		t.Errorf("spend at effective limit 80 must deny, got %+v", at)
	}
}

func TestBudget_PooledRole_SharedCeilingNotMergedIntoMAX(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, nil, []*roles.RoleEntry{roleBudget("engineering", 500, "monthly", true)}, nil)

	// Pool exhausted by the team: any member is blocked regardless of own spend.
	blocked := evalBudget(t, pq, budgetEvalInput{
		principalID: "pid-1", roles: []string{"engineering"},
		principalSpends: map[string]float64{"monthly": 1},
		rolePoolSpends:  map[string]float64{"engineering": 500},
	})
	if blocked.allow {
		t.Errorf("member must be blocked when the pool is exhausted, got %+v", blocked)
	}
	if len(blocked.ceilings) != 1 || blocked.ceilings[0]["scope"] != "role_pool" {
		t.Errorf("expected a single role_pool ceiling (not a principal window), got %v", blocked.ceilings)
	}

	// Pool has room: allowed.
	open := evalBudget(t, pq, budgetEvalInput{
		principalID: "pid-1", roles: []string{"engineering"},
		rolePoolSpends: map[string]float64{"engineering": 499},
	})
	if !open.allow || !open.hasLimit {
		t.Errorf("member with pool headroom must allow with has_limit, got %+v", open)
	}
}

// ── tenant cap ───────────────────────────────────────────────────────────────

func TestBudget_TenantCap_BlocksAgentsNotHumans(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, nil, nil, tenantCap(2000, "monthly"))

	agent := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		tenantSpends: map[string]float64{"monthly": 2000},
	})
	if agent.allow || !agent.limitReached {
		t.Errorf("AGENT at tenant cap must deny, got %+v", agent)
	}

	svc := evalBudget(t, pq, budgetEvalInput{
		principalID: "svc-1", principalType: "SERVICE",
		tenantSpends: map[string]float64{"monthly": 2000},
	})
	if svc.allow {
		t.Errorf("SERVICE at tenant cap must deny, got %+v", svc)
	}

	human := evalBudget(t, pq, budgetEvalInput{
		principalID: "user-1", principalType: "HUMAN",
		tenantSpends: map[string]float64{"monthly": 2000},
	})
	if !human.allow || human.hasLimit {
		t.Errorf("HUMAN must not be blocked or limited by the tenant cap, got %+v", human)
	}
}

func TestBudget_TenantCap_WeeklyWindow(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, nil, nil, tenantCap(300, "weekly"))

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		tenantSpends: map[string]float64{"weekly": 300, "monthly": 50},
	})
	if v.allow {
		t.Errorf("AGENT at weekly tenant cap must deny, got %+v", v)
	}
}

// ── per-run ceiling ──────────────────────────────────────────────────────────

func TestBudget_PerRunCeiling_EnforcedWithRunIdentity(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{grantPerRunBudget("agent-1", "g1", 5)}, nil, nil)

	at := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT", runID: "run-42",
		runSpend: 5,
	})
	if at.allow || !at.hasLimit {
		t.Errorf("run spend at per-run limit must deny, got %+v", at)
	}

	under := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT", runID: "run-42",
		runSpend: 4.99,
	})
	if !under.allow {
		t.Errorf("run spend under per-run limit must allow, got %+v", under)
	}

	// No run identity → per-run ceiling does not apply.
	noRun := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		runSpend: 9999,
	})
	if !noRun.allow || noRun.hasLimit {
		t.Errorf("without run identity the per-run ceiling must not apply, got %+v", noRun)
	}
}

func TestBudget_PerRunOnlyGrant_NoWindowCeiling(t *testing.T) {
	// A per-run-only grant must not create a zero-valued window ceiling.
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{grantPerRunBudget("agent-1", "g1", 5)}, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT", runID: "run-42",
		principalSpends: map[string]float64{"monthly": 9999},
		runSpend:        1,
	})
	if !v.allow {
		t.Errorf("per-run-only grant must not impose a window ceiling, got %+v", v)
	}
	if len(v.ceilings) != 1 || v.ceilings[0]["scope"] != "run" {
		t.Errorf("expected exactly the run ceiling, got %v", v.ceilings)
	}
}

// ── composition ──────────────────────────────────────────────────────────────

func TestBudget_AllCeilingsAND(t *testing.T) {
	// Agent with per-run $5, monthly grant $50, tenant cap $2000:
	// any single breach denies.
	grantList := []*grants.Grant{
		grantBudget("agent-1", "g-month", 50, "monthly"),
		grantPerRunBudget("agent-1", "g-run", 5),
	}
	pq := compileAndPrepareBudgetQuery(t, grantList, nil, tenantCap(2000, "monthly"))

	allUnder := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT", runID: "run-1",
		principalSpends: map[string]float64{"monthly": 20},
		tenantSpends:    map[string]float64{"monthly": 100},
		runSpend:        1,
	})
	if !allUnder.allow {
		t.Errorf("all ceilings under limit must allow, got %+v", allUnder)
	}
	if len(allUnder.ceilings) != 3 {
		t.Errorf("expected 3 ceilings (run, principal-monthly, tenant), got %v", allUnder.ceilings)
	}

	runBreach := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT", runID: "run-1",
		principalSpends: map[string]float64{"monthly": 20},
		tenantSpends:    map[string]float64{"monthly": 100},
		runSpend:        5,
	})
	if runBreach.allow {
		t.Errorf("run ceiling breach must deny even with other headroom, got %+v", runBreach)
	}
}

func TestBudget_SpendLimitReachedInverseOfAllow(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{grantBudget("pid-1", "g1", 100, "monthly")}, nil, nil)

	for _, spend := range []float64{0, 99.99, 100, 200} {
		v := evalBudget(t, pq, budgetEvalInput{principalID: "pid-1", principalSpends: map[string]float64{"monthly": spend}})
		if v.allow == v.limitReached {
			t.Errorf("spend %.2f: budget_allow=%v and spend_limit_reached=%v must differ", spend, v.allow, v.limitReached)
		}
	}
}
