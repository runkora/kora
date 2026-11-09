package roles

import (
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/events"
)

func TestSpinePayloads_MatchSchema(t *testing.T) {
	e := &RoleEntry{ID: "r", Role: "eng", Scopes: []string{"a"}, Budget: &BudgetAssignment{LimitUSD: 5, ResetCadence: "monthly", Pooled: true},
		CreatedAt: time.Now(), Source: "crd", Repository: "github.com/example-org/policy"}
	if err := events.ValidatePayload(events.RoleUpdated, newEntity(e)); err != nil {
		t.Error(err)
	}
}
