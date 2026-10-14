// Scope publisher (Phase 3.3).
//
// After each introspection cycle the publisher maps the discovered tools to the
// mcp scope vocabulary and registers them with the policy service via the
// routescopes client (registered_by = "tool-gateway"). It registers once per
// tenant, so same-named servers in different tenants stay separate entries:
// global servers and the mcp:invoke coarse scope go in the global ("")
// registration, each tenant's servers under that tenant.
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
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Publisher publishes the mcp scope vocabulary from introspection snapshots.
type Publisher struct {
	registry     *Registry
	introspector *Introspector
	client       *routescopes.Client
	logger       logging.Logger
}

// NewPublisher constructs a Publisher.
func NewPublisher(registry *Registry, introspector *Introspector, client *routescopes.Client, logger logging.Logger) *Publisher {
	return &Publisher{registry: registry, introspector: introspector, client: client, logger: logger}
}

// groupByTenant splits snapshots by the tenant that owns each server. The global
// group ("") is always present, even with no global servers, because it carries
// the mcp:invoke coarse scope every tenant relies on.
func groupByTenant(snapshots []ServerSnapshot) map[string][]ServerSnapshot {
	groups := map[string][]ServerSnapshot{"": nil}
	for _, snap := range snapshots {
		groups[snap.Server.TenantID] = append(groups[snap.Server.TenantID], snap)
	}
	return groups
}

// Publish builds the scope vocabulary from the current snapshots and registers
// it. The destructive prune runs only when the keep-list is complete (registry
// mounted + every server seen-or-retained); otherwise the publish is
// Upsert-only.
//
// One registration attempt per call: a failure (the identity service or the
// policy service still starting) is returned to the
// caller. The introspection cycle that drives Publish is itself the retry, so a
// publish that fails is re-attempted on the next cycle rather than blocking
// this one.
func (p *Publisher) Publish(ctx context.Context) error {
	servers := p.registry.Servers()
	snapshots := p.introspector.Snapshot()

	prune := p.registry.Loaded() && p.introspector.AllSeenOrRetained(servers)

	groups := groupByTenant(snapshots)
	tenants := make([]string, 0, len(groups))
	for tenantID := range groups {
		tenants = append(tenants, tenantID)
	}
	// "" (global) sorts first: a tenant registration checks that mcp:invoke
	// exists, and only the global registration publishes it.
	sort.Strings(tenants)

	// One tenant failing must not stop the others publishing; the next
	// introspection cycle retries whatever failed.
	var errs []error
	for _, tenantID := range tenants {
		decls, collisions := BuildScopes(groups[tenantID])
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

		// mcp:invoke is the global coarse gate; a tenant keeps no copy of its own.
		if tenantID != "" {
			decls = slices.DeleteFunc(decls, func(d routescopes.ScopeDeclaration) bool {
				return d.Scope == mcpInvokeScope
			})
		}

		if err := p.client.RegisterTenantWithPrune(ctx, ServiceName, tenantID, CoarseRoutes(), decls, prune); err != nil {
			errs = append(errs, fmt.Errorf("register mcp scopes for tenant %q: %w", tenantID, err))
			continue
		}

		p.logger.Info(ctx, "MCP scope vocabulary registered with policy service",
			logging.Field{Key: "service", Value: ServiceName},
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "scopes", Value: len(decls)},
			logging.Field{Key: "servers", Value: len(groups[tenantID])},
			logging.Field{Key: "collisions", Value: len(collisions)},
			logging.Field{Key: "pruned", Value: prune})
	}
	return errors.Join(errs...)
}
