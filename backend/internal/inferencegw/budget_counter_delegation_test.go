package inferencegw

import (
	"context"
	"slices"
	"testing"
	"time"
)

// ─── Delegator counters (delegated-agent-identity ADR, Decision 5) ───────────
//
// The delegator's canonical window counters use the SAME key shape as any
// principal's, so their own usage and their delegated runs accumulate into
// one set of counters.

func TestGetSpends_ReadsDelegatorWindows(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rdb := &stubRedisOps{mgetVals: map[string]any{
		principalKey("agent-1", "monthly", now): "10",
		principalKey("user-1", "monthly", now):  "22.5",
		principalKey("user-1", "weekly", now):   "4",
	}}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	spends, err := c.GetSpends(context.Background(), SpendScopes{
		TenantID:         "t1",
		PrincipalID:      "agent-1",
		PrincipalWindows: []string{"monthly"},
		DelegatorID:      "user-1",
		DelegatorWindows: []string{"monthly", "weekly"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spends.Principal["monthly"] != 10 {
		t.Errorf("principal spends = %v", spends.Principal)
	}
	if spends.Delegator["monthly"] != 22.5 || spends.Delegator["weekly"] != 4 {
		t.Errorf("delegator spends = %v, want monthly 22.5 / weekly 4", spends.Delegator)
	}
}

func TestGetSpends_DelegatorMissRehydratesFromDB(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{} // all keys missing
	usage := &stubUsageRepo{principalSpend: 33}
	c := newBudgetCounter(rdb, usage)

	spends, err := c.GetSpends(context.Background(), SpendScopes{
		TenantID:         "t1",
		PrincipalID:      "agent-1",
		DelegatorID:      "user-1",
		DelegatorWindows: []string{"monthly"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// GetSpendSince attributes delegated rows to the delegator, so the primed
	// value is the person's own + delegated spend.
	if spends.Delegator["monthly"] != 33 {
		t.Errorf("rehydrated delegator spend = %v, want 33", spends.Delegator)
	}
	now := time.Now().UTC()
	if !slices.Contains(rdb.setKeys, principalKey("user-1", "monthly", now)) {
		t.Errorf("delegator miss must prime its key, primed: %v", rdb.setKeys)
	}
}

func TestIncrementSpends_WritesDelegatorCounters(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rdb := &stubRedisOps{}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	err := c.IncrementSpends(context.Background(), SpendScopes{
		TenantID:         "t1",
		PrincipalID:      "agent-1",
		DelegatorID:      "user-1",
		DelegatorWindows: []string{"weekly"},
	}, nil, 2.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Agent monthly (always) + delegator monthly (always) + delegator weekly.
	for _, want := range []string{
		principalKey("agent-1", "monthly", now),
		principalKey("user-1", "monthly", now),
		principalKey("user-1", "weekly", now),
	} {
		if !slices.Contains(rdb.incrKeys, want) {
			t.Errorf("expected increment of %q, incremented: %v", want, rdb.incrKeys)
		}
	}
}

func TestIncrementSpends_NoDelegator_NoDelegatorKeys(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rdb := &stubRedisOps{}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	if err := c.IncrementSpends(context.Background(), SpendScopes{
		TenantID:    "t1",
		PrincipalID: "agent-1",
	}, nil, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rdb.incrKeys) != 1 || rdb.incrKeys[0] != principalKey("agent-1", "monthly", now) {
		t.Errorf("expected only the agent monthly counter, incremented: %v", rdb.incrKeys)
	}
}
