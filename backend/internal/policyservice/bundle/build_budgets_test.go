package bundle

import (
	"testing"

	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
)

// These tests exercise buildBudgets directly (internal package test).

func grantWithBudget(principalID, grantID string, monthlyLimit float64) *grants.Grant {
	return &grants.Grant{
		ID:            grantID,
		TenantID:      "test-tenant",
		PrincipalType: grants.PrincipalTypeHuman,
		PrincipalID:   principalID,
		Scopes:        []string{"inference:invoke"},
		Budget: &grants.BudgetAssignment{
			MonthlyLimitUSD: monthlyLimit,
			ResetCadence:    "monthly",
		},
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

func agentGrantWithBudget(principalID, grantID string, monthlyLimit float64) *grants.Grant {
	return &grants.Grant{
		ID:            grantID,
		TenantID:      "test-tenant",
		PrincipalType: grants.PrincipalTypeAgent,
		PrincipalID:   principalID,
		Scopes:        []string{"inference:invoke"},
		Budget: &grants.BudgetAssignment{
			MonthlyLimitUSD: monthlyLimit,
			ResetCadence:    "monthly",
		},
	}
}

func TestBuildBudgets_EmptyInputs(t *testing.T) {
	doc := buildBudgets(nil, nil)
	if len(doc) != 0 {
		t.Errorf("len = %d, want 0 for empty inputs", len(doc))
	}
}

func TestBuildBudgets_SingleGrantWithBudget(t *testing.T) {
	doc := buildBudgets([]*grants.Grant{grantWithBudget("pid-1", "grant-1", 100)}, nil)

	pb, ok := doc["pid-1"]
	if !ok {
		t.Fatal("principal pid-1 not in budget map")
	}
	if len(pb.Categories) != 1 {
		t.Errorf("categories len = %d, want 1", len(pb.Categories))
	}
	if pb.EffectiveLimitUSD != 100 {
		t.Errorf("effective_limit_usd = %v, want 100", pb.EffectiveLimitUSD)
	}
	if pb.Categories[0].Source != "grant" {
		t.Errorf("source = %q, want %q", pb.Categories[0].Source, "grant")
	}
	if pb.Categories[0].ID != "grant-1" {
		t.Errorf("id = %q, want %q", pb.Categories[0].ID, "grant-1")
	}
	if pb.Categories[0].MonthlyLimitUSD != 100 {
		t.Errorf("monthly_limit_usd = %v, want 100", pb.Categories[0].MonthlyLimitUSD)
	}
}

func TestBuildBudgets_NilBudgetGrantExcluded(t *testing.T) {
	// A human grant with Budget == nil contributes no category and the principal
	// should be pruned from the map entirely.
	doc := buildBudgets([]*grants.Grant{grantNoBudget("pid-1", "grant-1")}, nil)
	if _, ok := doc["pid-1"]; ok {
		t.Error("principal with nil-budget grant should be absent from budget map")
	}
}

func TestBuildBudgets_AgentGrantExcluded(t *testing.T) {
	// Agent grants must not appear in the budget map.
	doc := buildBudgets([]*grants.Grant{agentGrantWithBudget("agent-1", "grant-1", 100)}, nil)
	if _, ok := doc["agent-1"]; ok {
		t.Error("agent principal should be absent from budget map")
	}
}

func TestBuildBudgets_MAXResolution(t *testing.T) {
	// Two grants for the same principal: $50 and $200.
	// effective_limit_usd must be MAX = 200.
	doc := buildBudgets([]*grants.Grant{
		grantWithBudget("pid-1", "grant-a", 50),
		grantWithBudget("pid-1", "grant-b", 200),
	}, nil)

	pb, ok := doc["pid-1"]
	if !ok {
		t.Fatal("principal pid-1 not in budget map")
	}
	if pb.EffectiveLimitUSD != 200 {
		t.Errorf("effective_limit_usd = %v, want 200 (MAX of 50, 200)", pb.EffectiveLimitUSD)
	}
	if len(pb.Categories) != 2 {
		t.Errorf("categories len = %d, want 2", len(pb.Categories))
	}
}

func TestBuildBudgets_MultiplePrincipalsIndependent(t *testing.T) {
	doc := buildBudgets([]*grants.Grant{
		grantWithBudget("pid-a", "grant-1", 100),
		grantWithBudget("pid-b", "grant-2", 200),
	}, nil)

	if len(doc) != 2 {
		t.Errorf("len = %d, want 2", len(doc))
	}
	if doc["pid-a"].EffectiveLimitUSD != 100 {
		t.Errorf("pid-a effective = %v, want 100", doc["pid-a"].EffectiveLimitUSD)
	}
	if doc["pid-b"].EffectiveLimitUSD != 200 {
		t.Errorf("pid-b effective = %v, want 200", doc["pid-b"].EffectiveLimitUSD)
	}
}

func TestBuildBudgets_RoleEntriesIgnored(t *testing.T) {
	// Role entries are not yet resolved to principals — the map must remain empty
	// when only roles (no grants) have budgets.
	roleWithBudget := &roles.RoleEntry{
		TenantID: "test-tenant",
		Role:     "power-user",
		Scopes:   []string{"inference:invoke"},
		Budget: &roles.BudgetAssignment{
			MonthlyLimitUSD: 500,
			ResetCadence:    "monthly",
		},
	}
	doc := buildBudgets(nil, []*roles.RoleEntry{roleWithBudget})
	if len(doc) != 0 {
		t.Errorf("len = %d, want 0 (role-only budgets not yet resolved)", len(doc))
	}
}

func TestBuildBudgets_MixedGrantsOnePrincipalHasBudget(t *testing.T) {
	// pid-1 has a budget; pid-2 has only a nil-budget grant → pruned.
	doc := buildBudgets([]*grants.Grant{
		grantWithBudget("pid-1", "grant-1", 75),
		grantNoBudget("pid-2", "grant-2"),
	}, nil)

	if _, ok := doc["pid-1"]; !ok {
		t.Error("pid-1 should be in budget map")
	}
	if _, ok := doc["pid-2"]; ok {
		t.Error("pid-2 with nil budget should be pruned from map")
	}
}
