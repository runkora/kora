package identity

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/events"
)

// Every payload identity publishes validates against its type schema.
func TestSpinePayloads_MatchSchema(t *testing.T) {
	now := time.Now()
	run := uuid.NewString()
	grant := "g-1"
	for typ, entity := range map[events.Type]any{
		events.SessionCreated: newSessionEntity(&Session{ID: "s", PrincipalID: "p", PrincipalType: PrincipalTypeHuman,
			IdentitySource: IdentitySourceFederated, DisplayName: "Ada", CreatedAt: now, ExpiresAt: now}),
		events.PATUpdated: newPATEntity(&PAT{ID: "pat", UserID: "u", Name: "ci", Scopes: []string{"a"}, GrantID: &grant, ExpiresAt: now, CreatedAt: now, RevokedAt: &now}),
		events.DelegationIssued: delegationEvent(&DelegationIssuance{RunID: run, AgentPrincipalID: "a", DelegatorPrincipalID: "p",
			DelegatorPrincipalType: PrincipalTypeHuman, CreatedAt: now}).Entity,
	} {
		if err := events.ValidatePayload(typ, entity); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
	// SwitchSession records the side the person left and the side they joined.
	for _, direction := range []string{"out", "in"} {
		if err := events.ValidatePayload(events.TenantSwitched, events.TenantSwitchPayload{SessionID: "s", PrincipalID: "p", Direction: direction}); err != nil {
			t.Errorf("switch %s: %v", direction, err)
		}
	}
}
