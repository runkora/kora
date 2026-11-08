package inferencegw

import (
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/events"
)

// The budget payload validates against both budget type schemas.
func TestSpinePayloads_MatchSchema(t *testing.T) {
	for typ, level := range map[events.Type]int{events.BudgetThresholdCrossed: 80, events.BudgetExceeded: 0} {
		p := events.BudgetPayload{Scope: "tenant", ID: "t1", Window: "monthly", PeriodStart: time.Now(), LimitUSD: 10, SpendUSD: 8, Level: level}
		if err := events.ValidatePayload(typ, p); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
	for _, level := range []int{50, 80, 100} {
		if err := events.ValidatePayload(events.BudgetThresholdCrossed, events.BudgetPayload{Scope: "principal", Level: level}); err != nil {
			t.Errorf("level %d: %v", level, err)
		}
	}
}
