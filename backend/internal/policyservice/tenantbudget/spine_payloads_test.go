package tenantbudget

import (
	"testing"

	"github.com/runkora/kora/backend/internal/platform/events"
)

func TestSpinePayloads_MatchSchema(t *testing.T) {
	if err := events.ValidatePayload(events.TenantBudgetUpdated, newEntity(&TenantBudget{LimitUSD: 5, ResetCadence: "monthly", Source: "api"})); err != nil {
		t.Error(err)
	}
}
