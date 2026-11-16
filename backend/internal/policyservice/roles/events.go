package roles

import (
	"context"
	"errors"
	"fmt"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

func newEntity(e *RoleEntry) events.RolePayload {
	var budget *events.RoleBudget
	if b := e.Budget; b != nil {
		budget = &events.RoleBudget{LimitUSD: b.LimitUSD, ResetCadence: b.ResetCadence, Pooled: b.Pooled}
	}
	return events.RolePayload{
		ID: e.ID, Role: e.Role, Scopes: e.Scopes, Budget: budget,
		CreatedAt: e.CreatedAt, Source: e.Source, Repository: e.Repository,
	}
}

// publish records a change to e. An update that changed nothing records nothing.
func publish(ctx context.Context, pool *storage.Pool, typ events.Type, e *RoleEntry, changed []string) error {
	if typ == events.RoleUpdated && len(changed) == 0 {
		return nil
	}
	return events.Publish(ctx, pool, events.Event{
		Type: typ, Tenant: e.TenantID, Subject: "roles/" + e.Role, Entity: newEntity(e), Changed: changed,
	})
}

// publishWrite records the role after a write as created when before is nil
// and as updated otherwise.
func publishWrite(ctx context.Context, pool *storage.Pool, before, after *RoleEntry) error {
	if before == nil {
		return publish(ctx, pool, events.RoleCreated, after, nil)
	}
	return publish(ctx, pool, events.RoleUpdated, after, events.Changed(newEntity(before), newEntity(after)))
}

// existing locks and returns the stored role, or nil when there is none.
func (r *postgresRepository) existing(ctx context.Context, tenantID, role string) (*RoleEntry, error) {
	// FOR UPDATE locks no row on a first write. The lock makes one of two concurrent first writes the creator.
	if _, err := r.pool.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('policy_role_scopes/' || $1 || '/' || $2))`, tenantID, role); err != nil {
		return nil, fmt.Errorf("lock role: %w", err)
	}
	e, err := r.scanEntry(r.pool.QueryRow(ctx,
		`SELECT `+entryColumns+` FROM policy_role_scopes WHERE tenant_id = $1 AND role = $2 FOR UPDATE`, tenantID, role))
	if errors.Is(err, ErrRoleNotFound) {
		return nil, nil
	}
	return e, err
}
