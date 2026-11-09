package inferencegw

import (
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/policy"
)

// The budget payload validates against both budget type schemas.
func TestSpinePayloads_MatchSchema(t *testing.T) {
	for typ, level := range map[events.Type]int{events.BudgetThresholdCrossed: 80, events.BudgetExceeded: 0} {
		p := events.BudgetPayload{Scope: "tenant", ID: "t1", Window: "monthly", PeriodStart: time.Now(), LimitUSD: 10, SpendUSD: 8, Level: level}
		if err := events.ValidatePayload(typ, p); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
	// Every level the gateway records, for both scopes it records them for.
	for _, level := range budgetLevels {
		for _, scope := range []string{"tenant", "principal"} {
			if !eventBudget(policy.BudgetCeiling{Scope: scope, LimitUSD: 1}) {
				t.Errorf("scope %s records no budget event", scope)
			}
			if err := events.ValidatePayload(events.BudgetThresholdCrossed, events.BudgetPayload{Scope: scope, Level: level}); err != nil {
				t.Errorf("%s level %d: %v", scope, level, err)
			}
		}
	}
}
