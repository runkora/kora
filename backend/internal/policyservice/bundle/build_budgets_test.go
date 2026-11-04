package bundle

import (
	"testing"

	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
)

// These tests exercise buildBudgets directly (internal package test).

func grantWithBudget(principalID, grantID string, limit float64, cadence string) *grants.Grant {
	return &grants.Grant{
		ID:            grantID,
		TenantID:      "test-tenant",
		PrincipalType: grants.PrincipalTypeHuman,
		PrincipalID:   principalID,
		Scopes:        []string{"inference:invoke"},
		Budget:        &grants.BudgetAssignment{LimitUSD: limit, ResetCadence: cadence},
	}
}

func grantNoBudget(principalID, grantID string) *grants.Grant {
	return &grants.Grant{
		ID:            grantID,
		TenantID:      "test-tenant",
		PrincipalType: grants.PrincipalTypeHuman,
		PrincipalID:   principalID,
		Scopes:        []string{"inference:invoke"},
	}
}

func TestBuildBudgets_EmptyInputs(t *testing.T) {
	doc := buildBudgets(nil, nil)
	if len(doc) != 0 {
		t.Errorf("len = %d, want 0 for empty inputs", len(doc))
	}
}

func TestBuildBudgets_PerWindowEffectiveLimits(t *testing.T) {
	doc := buildBudgets([]*grants.Grant{
		grantWithBudget("pid-1", "g-month-a", 50, "monthly"),
		grantWithBudget("pid-1", "g-month-b", 200, "monthly"),
		grantWithBudget("pid-1", "g-week", 20, "weekly"),
	}, nil)

	pb, ok := doc["pid-1"]
	if !ok {
		t.Fatal("principal pid-1 not in budget map")
	}
	if pb.EffectiveLimits["monthly"] != 200 {
		t.Errorf("monthly effective = %v, want 200 (MAX of 50, 200)", pb.EffectiveLimits["monthly"])
	}
	if pb.EffectiveLimits["weekly"] != 20 {
		t.Errorf("weekly effective = %v, want 20", pb.EffectiveLimits["weekly"])
	}
	if len(pb.Categories) != 3 {
		t.Errorf("categories len = %d, want 3", len(pb.Categories))
	}
}

func TestBuildBudgets_PerRunEffectiveLimit(t *testing.T) {
	g1 := grantWithBudget("agent-1", "g1", 0, "monthly")
	g1.PrincipalType = grants.PrincipalTypeAgent
	g1.Budget = &grants.BudgetAssignment{PerRunLimitUSD: 5, ResetCadence: "monthly"}
	g2 := grantWithBudget("agent-1", "g2", 0, "monthly")
	g2.PrincipalType = grants.PrincipalTypeAgent
	g2.Budget = &grants.BudgetAssignment{PerRunLimitUSD: 8, ResetCadence: "monthly"}

	doc := buildBudgets([]*grants.Grant{g1, g2}, nil)

	pb, ok := doc["agent-1"]
	if !ok {
		t.Fatal("agent-1 not in budget map")
	}
	if pb.EffectivePerRunLimitUSD != 8 {
		t.Errorf("per-run effective = %v, want 8 (MAX of 5, 8)", pb.EffectivePerRunLimitUSD)
	}
	// Per-run-only grants must not leave a zero-valued window entry.
	if len(pb.EffectiveLimits) != 0 {
		t.Errorf("effective_limits = %v, want empty for per-run-only grants", pb.EffectiveLimits)
	}
}

func TestBuildBudgets_UnknownCadenceNormalizedToMonthly(t *testing.T) {
	doc := buildBudgets([]*grants.Grant{grantWithBudget("pid-1", "g1", 75, "fortnightly")}, nil)

	pb := doc["pid-1"]
	if pb.EffectiveLimits["monthly"] != 75 {
		t.Errorf("unknown cadence must normalize to monthly, got %v", pb.EffectiveLimits)
	}
}

func TestBuildBudgets_NoBudgetGrantsPruned(t *testing.T) {
	doc := buildBudgets([]*grants.Grant{
		grantWithBudget("pid-1", "grant-1", 75, "monthly"),
		grantNoBudget("pid-2", "grant-2"),
	}, nil)

	if _, ok := doc["pid-1"]; !ok {
		t.Error("pid-1 should be in budget map")
	}
	if _, ok := doc["pid-2"]; ok {
		t.Error("pid-2 with nil budget should be absent from map")
	}
}

func TestBuildBudgets_RoleEntriesIgnored(t *testing.T) {
	// Role entries are not resolved to principals at compile time — role budgets
	// live in data/role_budgets and are matched against input.principal.roles by
	// budget.rego at request time. The per-principal map must remain empty when
	// only roles (no grants) have budgets.
	roleWithBudget := &roles.RoleEntry{
		TenantID: "test-tenant",
		Role:     "power-user",
		Scopes:   []string{"inference:invoke"},
		Budget:   &roles.BudgetAssignment{LimitUSD: 500, ResetCadence: "monthly"},
	}
	doc := buildBudgets(nil, []*roles.RoleEntry{roleWithBudget})
	if len(doc) != 0 {
		t.Errorf("len = %d, want 0 (role-only budgets not in per-principal map)", len(doc))
	}
}

func TestBuildRoleBudgets_PooledAndCadence(t *testing.T) {
	doc := buildRoleBudgets([]*roles.RoleEntry{
		{TenantID: "t", Role: "engineering", Scopes: []string{"s"}, Budget: &roles.BudgetAssignment{LimitUSD: 500, ResetCadence: "weekly", Pooled: true}},
		{TenantID: "t", Role: "analyst", Scopes: []string{"s"}, Budget: &roles.BudgetAssignment{LimitUSD: 50, ResetCadence: "monthly"}},
		{TenantID: "t", Role: "no-budget", Scopes: []string{"s"}},
		{TenantID: "t", Role: "platform-admin", Scopes: []string{"s"}, Budget: &roles.BudgetAssignment{LimitUSD: 1, ResetCadence: "monthly"}},
	})

	eng := doc.RoleBudgets["engineering"]
	if !eng.Pooled || eng.LimitUSD != 500 || eng.ResetCadence != "weekly" {
		t.Errorf("engineering = %+v, want pooled weekly 500", eng)
	}
	if doc.RoleBudgets["analyst"].Pooled {
		t.Error("analyst must be per-member (pooled=false)")
	}
	if _, ok := doc.RoleBudgets["no-budget"]; ok {
		t.Error("role without budget must be absent")
	}
	if _, ok := doc.RoleBudgets["platform-admin"]; ok {
		t.Error("reserved roles cannot carry budgets")
	}
}
