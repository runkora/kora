package egressgw

import (
	"context"
	"fmt"
	"sort"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ServiceName is the service identity the egress gateway uses when registering
// scopes with the policy service. The platform token must carry this subject for
// the registration to be accepted under SERVICE-principal rules.
const ServiceName = "egress-gateway"

// readConnectionsScope is the scope guarding the read-only connections catalog
// (GET /api/v1/connections). It is declared and bound to the route so the
// policy service accepts the registration with a non-empty routes list and
// publishes the scope into the vocabulary. The read controller itself stays
// AuthMiddleware-only (no scope-enforcement middleware) — registering the route
// is only to satisfy the policy service and publish the connection vocabulary.
const readConnectionsScope = "platform:connections:read"

// writeConnectionsScope is the connection-management scope. Connections are
// GitOps-managed CRs, so there is no write HTTP route to bind it to — but the
// reserved tenant-admin/platform-admin roles reference it, so it must exist in
// the vocabulary to be grantable (e.g. via a PAT). It is published as a
// declaration-only scope (no route binding).
const writeConnectionsScope = "platform:connections:write"

// ReadRoutes returns the route→scope binding for the gateway's read-only admin
// API. It binds GET /api/v1/connections to readConnectionsScope so the
// policy-service registration carries a non-empty routes list (an empty routes
// list is rejected with 400 missing_required_field). This non-empty route is
// the sole reason connections_read_controller.go is retained — see its doc.
func ReadRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/api/v1/connections", Method: "GET", RequiredScope: readConnectionsScope},
	}
}

// ReadScope returns the scope declaration for the read-only connections catalog.
func ReadScope() routescopes.ScopeDeclaration {
	return routescopes.ScopeDeclaration{
		Scope:        readConnectionsScope,
		ResourceType: "platform",
		PathSegments: []string{"connections"},
		Action:       "read",
		Description:  "Read the egress-gateway connection catalog",
	}
}

// WriteScope returns the declaration for the connection-management scope. It is
// declaration-only (no bound route) — see writeConnectionsScope.
func WriteScope() routescopes.ScopeDeclaration {
	return routescopes.ScopeDeclaration{
		Scope:        writeConnectionsScope,
		ResourceType: "platform",
		PathSegments: []string{"connections"},
		Action:       "write",
		Description:  "Manage connections (reserved-role scope; connections are GitOps-managed)",
	}
}

// ConnectionScopes builds the per-connection scope vocabulary for the policy
// service from the supplied connections and their registered adapters.
//
// For each connection it emits one ScopeDeclaration per action the connection's
// adapter can classify — carrying the adapter's declared requires edges,
// resolved to full same-connection scopes — plus the covering parents
// "connection:<name>:read" / "connection:<name>:write", which the scope_covers
// prefix rule makes the umbrella over every category and qualifier beneath
// them. (The former "connection:<name>:*" wildcard entry is gone: no wildcard
// rule exists in scope_covers, so it never authorized anything. See
// docs/adr/2026-07-22-github-connection-category-scoped-actions.md.) The
// returned list is sorted by Scope so registration is order-stable across calls.
//
// Connections whose adapter is not in the registry are silently skipped — the
// broker will reject calls to those connections at request time with
// ErrUnsupportedAdapter, so it is correct to omit their scopes from the
// vocabulary rather than register dangling entries.
func ConnectionScopes(conns []*credentials.Connection, registry *AdapterRegistry) []routescopes.ScopeDeclaration {
	if registry == nil {
		return nil
	}

	decls := make([]routescopes.ScopeDeclaration, 0, len(conns)*3) //nolint:mnd // initial capacity hint, not magic
	seen := make(map[string]struct{})

	for _, conn := range conns {
		if conn == nil {
			continue
		}
		adapter, ok := registry.Lookup(conn.Adapter)
		if !ok {
			continue
		}

		// Covering parents: connection:<name>:read / :write cover every
		// category (and repository qualifier) of the matching verb by prefix.
		for _, verb := range []string{"read", "write"} {
			if d, fresh := dedupe(seen, routescopes.ScopeDeclaration{
				Scope:        fmt.Sprintf("connection:%s:%s", conn.Name, verb),
				ResourceType: "connection",
				PathSegments: []string{conn.Name},
				Action:       verb,
				Description:  fmt.Sprintf("Every %s action on the %q connection, all resources (covering parent)", verb, conn.Name),
			}); fresh {
				decls = append(decls, d)
			}
		}

		for _, action := range adapter.Actions() {
			requires := make([]string, 0, len(action.Requires))
			for _, r := range action.Requires {
				requires = append(requires, fmt.Sprintf("connection:%s:%s", conn.Name, r))
			}
			if d, fresh := dedupe(seen, routescopes.ScopeDeclaration{
				Scope:        fmt.Sprintf("connection:%s:%s", conn.Name, action.Action),
				ResourceType: "connection",
				PathSegments: []string{conn.Name},
				Action:       action.Action,
				Description:  fmt.Sprintf("%s — %s", conn.Name, action.Description),
				Requires:     requires,
			}); fresh {
				decls = append(decls, d)
			}
		}
	}

	sort.Slice(decls, func(i, j int) bool { return decls[i].Scope < decls[j].Scope })
	return decls
}

// AllScopes is the full set of scope declarations the gateway publishes: the
// static platform:connections:read and platform:connections:write scopes, plus
// the per-connection connection:<name>:<action> scopes for every connection in
// conns. The two static scopes are always published (read guards a static
// route; write is the reserved-role management scope); the connection scopes
// vary with the mounted registry.
func AllScopes(conns []*credentials.Connection, registry *AdapterRegistry) []routescopes.ScopeDeclaration {
	return append([]routescopes.ScopeDeclaration{ReadScope(), WriteScope()}, ConnectionScopes(conns, registry)...)
}

// dedupe returns the declaration if its Scope has not been emitted yet,
// recording the scope in seen. fresh is false on a duplicate.
func dedupe(seen map[string]struct{}, d routescopes.ScopeDeclaration) (routescopes.ScopeDeclaration, bool) {
	if _, ok := seen[d.Scope]; ok {
		return routescopes.ScopeDeclaration{}, false
	}
	seen[d.Scope] = struct{}{}
	return d, true
}

// ScopePublisher (re)publishes the gateway's connection scope vocabulary to the
// policy service: the connection:<name>:<action> scopes for the registered
// connections.
//
// Since Amendment 3 it is driven by the mounted connection registry, not an
// admin API:
//   - at startup, Publish(ctx, "") registers the vocabulary for every connection
//     across all tenants (ListByTenant("") returns ALL);
//   - on a registry reload (a Connection CR / Secret change re-aggregated by the
//     operator), the gateway calls Publish(ctx, "") again to republish.
//
// Scopes are upserted by the policy service, so republishing never disturbs an
// unaffected entry.
type ScopePublisher struct {
	repo     credentials.ConnectionRepository
	registry *AdapterRegistry
	client   *routescopes.Client
	logger   logging.Logger
}

// NewScopePublisher constructs a ScopePublisher.
func NewScopePublisher(repo credentials.ConnectionRepository, registry *AdapterRegistry, client *routescopes.Client, logger logging.Logger) *ScopePublisher {
	return &ScopePublisher{repo: repo, registry: registry, client: client, logger: logger}
}

// Publish lists tenantID's connections (tenantID == "" lists ALL) and registers
// their connection:<name>:<action> scopes with the policy service. The
// routescopes client retries until the identity-proxy sidecar is ready, so
// callers run it in a goroutine.
func (p *ScopePublisher) Publish(ctx context.Context, tenantID string) error {
	conns, err := p.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list connections: %w", err)
	}
	if err := p.client.Register(ctx, ServiceName, ReadRoutes(), AllScopes(conns, p.registry)); err != nil {
		return fmt.Errorf("register scopes: %w", err)
	}
	p.logger.Info(ctx, "connection scope vocabulary registered with policy service",
		logging.Field{Key: "service", Value: ServiceName},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "connections", Value: len(conns)})
	return nil
}
