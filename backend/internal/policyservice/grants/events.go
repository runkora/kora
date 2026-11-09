package grants

import (
	"context"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

func newEntity(g *Grant) events.GrantPayload {
	var budget *events.GrantBudget
	if b := g.Budget; b != nil {
		budget = &events.GrantBudget{LimitUSD: b.LimitUSD, ResetCadence: b.ResetCadence, PerRunLimitUSD: b.PerRunLimitUSD}
	}
	return events.GrantPayload{
		ID: g.ID, PrincipalType: string(g.PrincipalType), PrincipalID: g.PrincipalID, Scopes: g.Scopes,
		Budget: budget, DelegationRequired: g.DelegationRequired, CreatedAt: g.CreatedAt,
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
