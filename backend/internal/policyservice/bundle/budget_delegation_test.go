package bundle_test

import (
	"testing"

	"github.com/clear-route/orbit/internal/policyservice/grants"
)

// ─── Delegator ceilings (delegated-agent-identity ADR, Decision 5) ───────────
//
// When delegation is present, the DELEGATOR's per-window limits and per-run
// limit apply as additional independent ceilings alongside the agent's. The
// run stops at whichever ceiling is reached first, and a delegator breach is
// distinguishable from an agent breach via the ceiling scope
// ("delegator"/"delegator_run" vs "principal"/"run").

// breachedScopes returns the scopes of all ceilings at/over their limit.
func breachedScopes(t *testing.T, v budgetVerdict) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, c := range v.ceilings {
		scope, _ := c["scope"].(string)
		if num(t, c["spend_usd"]) >= num(t, c["limit_usd"]) {
			out[scope] = true
		}
	}
	return out
}

func TestBudget_DelegatorWindowBreach_DeniesWhileAgentFine(t *testing.T) {
	// Agent has a generous monthly grant budget; the delegator's own monthly
	// allowance is exhausted. The delegator ceiling must deny independently.
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{
		grantBudget("agent-1", "g-agent", 1000, "monthly"),
		grantBudget("user-1", "g-user", 50, "monthly"),
	}, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		delegatorID:     "user-1",
		principalSpends: map[string]float64{"monthly": 10},
		delegatorSpends: map[string]float64{"monthly": 50},
	})
	if v.allow {
		t.Errorf("delegator window breach must deny even with agent headroom, got %+v", v)
	}
	breached := breachedScopes(t, v)
	if !breached["delegator"] || breached["principal"] {
		t.Errorf("expected exactly the delegator ceiling breached, got ceilings %v", v.ceilings)
	}
}

func TestBudget_AgentWindowBreach_DeniesWhileDelegatorFine(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{
		grantBudget("agent-1", "g-agent", 50, "monthly"),
		grantBudget("user-1", "g-user", 1000, "monthly"),
	}, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		delegatorID:     "user-1",
		principalSpends: map[string]float64{"monthly": 50},
		delegatorSpends: map[string]float64{"monthly": 10},
	})
	if v.allow {
		t.Errorf("agent window breach must deny even with delegator headroom, got %+v", v)
	}
	breached := breachedScopes(t, v)
	if !breached["principal"] || breached["delegator"] {
		t.Errorf("expected exactly the principal ceiling breached, got ceilings %v", v.ceilings)
	}
}

func TestBudget_BothUnderLimits_AllowsWithBothCeilings(t *testing.T) {
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{
		grantBudget("agent-1", "g-agent", 100, "monthly"),
		grantBudget("user-1", "g-user", 100, "monthly"),
	}, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		delegatorID:     "user-1",
		principalSpends: map[string]float64{"monthly": 10},
		delegatorSpends: map[string]float64{"monthly": 20},
	})
	if !v.allow || !v.hasLimit {
		t.Errorf("both under their limits must allow with has_limit, got %+v", v)
	}
	if len(v.ceilings) != 2 {
		t.Errorf("expected 2 ceilings (principal + delegator), got %v", v.ceilings)
	}
}

func TestBudget_DelegatorPerRunCeiling(t *testing.T) {
	// The delegator carries a per-run limit; the agent has none. The same run
	// counter feeds both agent and delegator per-run ceilings.
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{
		grantPerRunBudget("user-1", "g-user-run", 5),
	}, nil, nil)

	at := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT", runID: "run-42",
		delegatorID: "user-1",
		runSpend:    5,
	})
	if at.allow {
		t.Errorf("run spend at the delegator's per-run limit must deny, got %+v", at)
	}
	if len(at.ceilings) != 1 || at.ceilings[0]["scope"] != "delegator_run" {
		t.Errorf("expected exactly the delegator_run ceiling, got %v", at.ceilings)
	}

	under := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT", runID: "run-42",
		delegatorID: "user-1",
		runSpend:    4.99,
	})
	if !under.allow {
		t.Errorf("run spend under the delegator's per-run limit must allow, got %+v", under)
	}

	// No run identity → the delegator's per-run ceiling does not apply.
	noRun := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		delegatorID: "user-1",
		runSpend:    9999,
	})
	if !noRun.allow {
		t.Errorf("without run identity the delegator per-run ceiling must not apply, got %+v", noRun)
	}
}

func TestBudget_NoDelegation_DelegatorBudgetsDoNotApply(t *testing.T) {
	// user-1 has an exhausted budget, but the request carries no delegation:
	// the agent must not be constrained by an unrelated person's ceilings.
	pq := compileAndPrepareBudgetQuery(t, []*grants.Grant{
		grantBudget("user-1", "g-user", 50, "monthly"),
	}, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		delegatorSpends: map[string]float64{"monthly": 9999},
	})
	if !v.allow || v.hasLimit {
		t.Errorf("without delegation no delegator ceiling may apply, got %+v", v)
	}
}

func TestBudget_UnbudgetedDelegator_FailsOpen(t *testing.T) {
	// Delegation present but the delegator has no budget configured: no
	// delegator ceiling, consistent with the fail-open budget model.
	pq := compileAndPrepareBudgetQuery(t, nil, nil, nil)

	v := evalBudget(t, pq, budgetEvalInput{
		principalID: "agent-1", principalType: "AGENT",
		delegatorID:     "user-1",
		delegatorSpends: map[string]float64{"monthly": 9999},
	})
	if !v.allow || v.hasLimit {
		t.Errorf("unbudgeted delegator must fail open, got %+v", v)
	}
}
