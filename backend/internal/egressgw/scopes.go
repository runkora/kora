package egressgw

import (
	"fmt"
	"sort"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
)

// ServiceName is the service identity the egress gateway uses when registering
// routes and scopes with the policy service. The platform token must carry
// this subject for the registration to be accepted under SERVICE-principal
// rules.
const ServiceName = "egress-gateway"

// Scope constants for the gateway's own administrative API. These are the
// scopes any caller must hold to register, list, or delete connections via the
// gateway's HTTP surface.
const (
	ScopePlatformConnectionsRead  = "platform:connections:read"
	ScopePlatformConnectionsWrite = "platform:connections:write"
)

// AdminRoutes is the static route→scope binding for the egress gateway's
// administrative HTTP API (connection registration). Mirrors the inferencegw
// pattern: registered at startup via routescopes.Client.Register and consumed
// by ScopeEnforcementMiddleware on the gateway's own HTTP server.
//
// The connection-egress hot path (the proxy itself) is not in this list — it
// is authorized in-process by Broker.Authorize against connection:<name>:<action>
// scopes published via ConnectionScopes.
func AdminRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/api/v1/connections", Method: "GET", RequiredScope: ScopePlatformConnectionsRead},
		{PathPrefix: "/api/v1/connections", Method: "POST", RequiredScope: ScopePlatformConnectionsWrite},
		{PathPrefix: "/api/v1/connections", Method: "PUT", RequiredScope: ScopePlatformConnectionsWrite},
		{PathPrefix: "/api/v1/connections", Method: "DELETE", RequiredScope: ScopePlatformConnectionsWrite},
	}
}

// adminScopes returns the static scope vocabulary entries the gateway always
// owns, regardless of which connections are registered.
func adminScopes() []routescopes.ScopeDeclaration {
	return []routescopes.ScopeDeclaration{
		{Scope: ScopePlatformConnectionsRead, ResourceType: "platform", PathSegments: []string{"connections"}, Action: "read", Description: "List and inspect tenant connections"},
		{Scope: ScopePlatformConnectionsWrite, ResourceType: "platform", PathSegments: []string{"connections"}, Action: "write", Description: "Create, update, and delete tenant connections"},
	}
}

// ConnectionScopes builds the per-connection scope vocabulary for the policy
// service from the supplied connections and their registered adapters.
//
// For each connection it emits one ScopeDeclaration per action the connection's
// adapter can classify, plus a wildcard "connection:<name>:*" entry that an
// admin can grant to bestow every action at once. The returned list is sorted
// by Scope so registration is order-stable across calls.
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

		// Wildcard scope: "connection:<name>:*" covers every action.
		if d, fresh := dedupe(seen, routescopes.ScopeDeclaration{
			Scope:        fmt.Sprintf("connection:%s:*", conn.Name),
			ResourceType: "connection",
			PathSegments: []string{conn.Name},
			Action:       "*",
			Description:  fmt.Sprintf("Use the %q connection for any action", conn.Name),
		}); fresh {
			decls = append(decls, d)
		}

		for _, action := range adapter.Actions() {
			if d, fresh := dedupe(seen, routescopes.ScopeDeclaration{
				Scope:        fmt.Sprintf("connection:%s:%s", conn.Name, action.Action),
				ResourceType: "connection",
				PathSegments: []string{conn.Name},
				Action:       action.Action,
				Description:  fmt.Sprintf("%s — %s", conn.Name, action.Description),
			}); fresh {
				decls = append(decls, d)
			}
		}
	}

	sort.Slice(decls, func(i, j int) bool { return decls[i].Scope < decls[j].Scope })
	return decls
}

// AllScopes is the full set of scope declarations the gateway publishes —
// the static administrative scopes plus per-connection scopes for every
// connection in conns. Used directly with routescopes.Client.Register.
func AllScopes(conns []*credentials.Connection, registry *AdapterRegistry) []routescopes.ScopeDeclaration {
	admin := adminScopes()
	connection := ConnectionScopes(conns, registry)
	all := make([]routescopes.ScopeDeclaration, 0, len(admin)+len(connection))
	all = append(all, admin...)
	all = append(all, connection...)
	sort.Slice(all, func(i, j int) bool { return all[i].Scope < all[j].Scope })
	return all
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
