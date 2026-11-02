// Scope publisher (Phase 3.3).
//
// After each introspection cycle the publisher maps the discovered tools to the
// mcp scope vocabulary and registers them with the policy service via the
// routescopes client (registered_by = "tool-gateway"). It publishes the global
// union keep-list (all servers, all tenants), exactly as the egress gateway
// republishes connection scopes.
//
// Prune-guard: the policy service's DeleteOrphans is destructive — it strips
// pruned scopes out of every grant and role. So the publisher requests the
// destructive prune ONLY once the registry is mounted AND every listed server
// has been seen-or-retained this lifetime; until then it publishes Upsert-only
// (prune=false), so a cold/partial keep-list can add scopes but never revoke
// live ones.
//
// See docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
package toolgateway

import (
	"context"
	"fmt"
	"sync"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Publisher publishes the mcp scope vocabulary from introspection snapshots.
//
// Publish serializes against itself: the introspection cycle calls it when a
// cycle changed something, and KeepRegistered calls it on its own heartbeat, so
// two callers can reach it at once. Scope publication is single-writer because
// the prune is destructive (see the package comment), and the replicas=1
// topology only rules out a second process.
type Publisher struct {
	registry     *Registry
	introspector *Introspector
	client       *routescopes.Client
	logger       logging.Logger

	mu sync.Mutex
}

// NewPublisher constructs a Publisher.
func NewPublisher(registry *Registry, introspector *Introspector, client *routescopes.Client, logger logging.Logger) *Publisher {
	return &Publisher{registry: registry, introspector: introspector, client: client, logger: logger}
}

// Publish builds the scope vocabulary from the current snapshots and registers
// it. The destructive prune runs only when the keep-list is complete (registry
// mounted + every server seen-or-retained); otherwise the publish is
// Upsert-only.
//
// One registration attempt per call: a failure (the identity service or the
// policy service still starting) is returned to the caller, which owns the
// retry. Both callers have one — KeepRegistered backs off until its first
// success and then re-registers on every tick, and the introspection cycle
// re-publishes on the next cycle — so a failed publish never blocks either.
func (p *Publisher) Publish(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	servers := p.registry.Servers()
	snapshots := p.introspector.Snapshot()

	decls, collisions := BuildScopes(snapshots)
	for _, c := range collisions {
		p.logger.Warn(ctx, "MCP tool scope collision — leaf shadows a sibling rollup, excluded",
			logging.Field{Key: "server", Value: c.Server},
			logging.Field{Key: "tenant_id", Value: c.TenantID},
			logging.Field{Key: "tool", Value: c.Tool},
			logging.Field{Key: "scope", Value: c.Scope},
			logging.Field{Key: "shadowed_by", Value: c.ShadowedBy})
		p.introspector.FlagDegraded(c.TenantID, c.Server,
			fmt.Sprintf("tool %q scope shadows sibling %q", c.Tool, c.ShadowedBy))
	}

	// Both halves read what this call already captured, never live state: see
	// AllSeenOrRetained.
	prune := p.registry.Loaded() && AllSeenOrRetained(servers, snapshots)
	if err := p.client.RegisterWithPrune(ctx, ServiceName, CoarseRoutes(), decls, prune); err != nil {
		return fmt.Errorf("register mcp scopes: %w", err)
	}

	p.logger.Info(ctx, "MCP scope vocabulary registered with policy service",
		logging.Field{Key: "service", Value: ServiceName},
		logging.Field{Key: "scopes", Value: len(decls)},
		logging.Field{Key: "servers", Value: len(servers)},
		logging.Field{Key: "collisions", Value: len(collisions)},
		logging.Field{Key: "pruned", Value: prune})
	return nil
}
