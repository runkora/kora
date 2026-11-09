package grants

import (
	"context"
	"time"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// entity is the grant an event carries.
type entity struct {
	ID                 string            `json:"id"`
	PrincipalType      PrincipalType     `json:"principal_type"`
	PrincipalID        string            `json:"principal_id"`
	Scopes             []string          `json:"scopes"`
	Budget             *BudgetAssignment `json:"budget,omitempty"`
	DelegationRequired bool              `json:"delegation_required"`
	CreatedAt          time.Time         `json:"created_at"`
}

func newEntity(g *Grant) entity {
	return entity{
		ID: g.ID, PrincipalType: g.PrincipalType, PrincipalID: g.PrincipalID, Scopes: g.Scopes,
		Budget: g.Budget, DelegationRequired: g.DelegationRequired, CreatedAt: g.CreatedAt,
	}
}

// publish records a change to g. An update that changed nothing records nothing.
func publish(ctx context.Context, pool *storage.Pool, typ events.Type, g *Grant, changed []string) error {
	if typ == events.GrantUpdated && len(changed) == 0 {
		return nil
	}
	return events.Publish(ctx, pool, events.Event{
		Type: typ, Tenant: g.TenantID, Subject: "grants/" + g.ID, Entity: newEntity(g), Changed: changed,
	})
}
