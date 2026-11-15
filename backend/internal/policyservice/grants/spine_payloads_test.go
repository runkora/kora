package grants

import (
	"slices"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/events"
)

// The grant payload validates against its type schema, and its principal type
// enum holds every grant principal type.
func TestSpinePayloads_MatchSchema(t *testing.T) {
	g := &Grant{ID: "g", PrincipalType: PrincipalTypeAgent, PrincipalID: "p", Scopes: []string{"a"},
		Budget: &BudgetAssignment{LimitUSD: 5, ResetCadence: "monthly"}, CreatedAt: time.Now()}
	if err := events.ValidatePayload(events.GrantUpdated, newEntity(g)); err != nil {
		t.Error(err)
	}
	want := []string{string(PrincipalTypeAgent), string(PrincipalTypeHuman), string(PrincipalTypeService), string(PrincipalTypePAT)}
	if got := events.Enum("grant_principal_type"); !slices.Equal(got, want) {
		t.Errorf("grant_principal_type = %v, want %v", got, want)
	}
}
