package egressgw

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
)

// ServiceName is the service identity the egress gateway uses when registering
// scopes with the policy service. The platform token must carry this subject for
// the registration to be accepted under SERVICE-principal rules.
const ServiceName = "egress-gateway"

// readConnectionsScope is the scope guarding the read-only connections catalog
// (GET /api/v1/connections). Binding it to the route gives the policy-service
// registration a non-empty routes list, which it requires. The controller
// itself stays AuthMiddleware-only, with no scope-enforcement middleware.
const readConnectionsScope = "platform:connections:read"

// writeConnectionsScope is the connection-management scope. Connections are
// GitOps-managed CRs, so no write route binds it. The reserved
// platform-admin roles reference it, so it must exist in the vocabulary to be
// grantable, for instance via a PAT. It is published without a route binding.
const writeConnectionsScope = "platform:connections:write"

// ReadRoutes returns the route-to-scope binding for the gateway's read-only
// admin API. The policy service rejects a registration whose routes list is
// empty, so this binding must exist for the vocabulary to publish at all.
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
// declaration-only, with no bound route. See writeConnectionsScope.
func WriteScope() routescopes.ScopeDeclaration {
	return routescopes.ScopeDeclaration{
		Scope:        writeConnectionsScope,
		ResourceType: "platform",
		PathSegments: []string{"connections"},
		Action:       "write",
		Description:  "Manage connections (reserved-role scope; connections are GitOps-managed)",
	}
}

// scopesPerConnectionHint sizes the declaration slice up front: every
// connection contributes the two covering parents plus at least one adapter
// action. It is a capacity hint, never a limit.
const scopesPerConnectionHint = 3

// ConnectionScopes builds the per-connection scope vocabulary for the policy
// service from the supplied connections and their registered adapters.
//
// Each connection emits one declaration per action its adapter can classify,
// carrying the adapter's requires edges resolved to full same-connection
// scopes. It also emits the covering parents "connection:<name>:read" and
// "connection:<name>:write", which the scope_covers prefix rule makes the
// umbrella over every category and qualifier beneath them. The result is sorted
// by Scope so registration is order-stable.
//
// A connection whose adapter is not registered is skipped. The broker rejects
// calls to it with ErrUnsupportedAdapter, so publishing its scopes would leave
// dangling vocabulary entries.
func ConnectionScopes(conns []*credentials.Connection, registry *AdapterRegistry) []routescopes.ScopeDeclaration {
	if registry == nil {
		return nil
	}

	decls := make([]routescopes.ScopeDeclaration, 0, len(conns)*scopesPerConnectionHint)
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
				Scope:        policy.DeriveRequiredScope(resourceType, []string{conn.Name}, verb),
				ResourceType: resourceType,
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
				segs, verb := connectionScopeParts(conn.Name, r)
				requires = append(requires, policy.DeriveRequiredScope(resourceType, segs, verb))
			}
			// An adapter action fragment ("issues:write", "repo:read", "use")
			// carries its own category. Split it so the stored PathSegments and
			// Action match the scope grammar the platform's ScopeCovers rule
			// uses (last segment = action, middle = path): "issues:write"
			// becomes path [name, issues] + action write, not path [name] +
			// action "issues:write". The Scope string itself is unchanged.
			pathSegs, verb := connectionScopeParts(conn.Name, action.Action)
			if d, fresh := dedupe(seen, routescopes.ScopeDeclaration{
				Scope:        policy.DeriveRequiredScope(resourceType, pathSegs, verb),
				ResourceType: resourceType,
				PathSegments: pathSegs,
				Action:       verb,
				Description:  fmt.Sprintf("%s: %s", conn.Name, action.Description),
				Requires:     requires,
			}); fresh {
				decls = append(decls, d)
			}
		}
	}

	slices.SortFunc(decls, func(a, b routescopes.ScopeDeclaration) int { return cmp.Compare(a.Scope, b.Scope) })
	return decls
}

// connectionScopeParts renders a connection scope in the grammar's shape: the
// path segments and the final action. The fragment's category becomes a path
// suffix after the connection name, so "issues:write" on connection "gh" is
// path [gh, issues] with action "write". A category-less fragment ("use")
// yields path [gh] and "use" as the action.
func connectionScopeParts(name, actionFragment string) (path []string, action string) {
	category, verb := splitActionFragment(actionFragment)
	path = []string{name}
	if category != "" {
		path = append(path, strings.Split(category, ":")...)
	}
	return path, verb
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

// connLister enumerates the connections whose vocabulary is being published,
// and whose catalog the read controller serves.
type connLister interface {
	ListByTenant(ctx context.Context, tenantID string) ([]*credentials.Connection, error)
}

// ScopePublisher publishes the connection:<name>:<action> vocabulary to the
// policy service. It is driven by the mounted connection registry:
//
//   - at startup, Publish(ctx, "") registers every connection across all
//     tenants, since ListByTenant("") returns all of them;
//   - on a registry reload, the gateway republishes the same way.
//
// The policy service upserts scopes, so republishing never disturbs an
// unaffected entry.
type ScopePublisher struct {
	conns    connLister
	registry *AdapterRegistry
	client   *routescopes.Client
	logger   logging.Logger
}

// NewScopePublisher constructs a ScopePublisher.
func NewScopePublisher(conns connLister, registry *AdapterRegistry, client *routescopes.Client, logger logging.Logger) *ScopePublisher {
	return &ScopePublisher{conns: conns, registry: registry, client: client, logger: logger}
}

// Publish lists tenantID's connections (tenantID == "" lists ALL) and registers
// their connection:<name>:<action> scopes with the policy service. It makes one
// attempt; routescopes.KeepRegistered owns the retry.
func (p *ScopePublisher) Publish(ctx context.Context, tenantID string) error {
	conns, err := p.conns.ListByTenant(ctx, tenantID)
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
