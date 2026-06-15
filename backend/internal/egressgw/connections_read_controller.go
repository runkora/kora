package egressgw

import (
	"net/http"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
)

// ConnectionsReadController serves the read-only connection catalog for the
// caller's tenant.
//
// Why this endpoint still exists (it is NOT dead code): the frontend connector
// picker no longer calls this route — the catalog it reads moved to orbit-agents
// (a plain-HTTP mesh service the BFF can reach; the gateway is a CONNECT/egress
// broker that is not reachable over plain HTTP through the mesh). What this
// controller is retained for is its route REGISTRATION. GET /connections is the
// only non-empty HTTP route the gateway exposes, and the gateway's scope
// vocabulary publish (ScopePublisher.Publish → ReadRoutes() in scopes.go) must
// register a non-empty routes list with the policy service, which rejects an
// empty routes list with 400 missing_required_field. Registering this route is
// what lets the connection scope vocabulary publish at all. Deleting it would
// break scope publication, so leave it in place.
//
// It is also the minimal replacement for the admin connections CRUD API removed
// by Amendment 3 of the JIT credential-brokering ADR
// (docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md): connections
// are now GitOps CRs, created and edited via Connection custom resources and
// aggregated by the operator into the mounted registry — never through this
// API.
//
// The response carries non-sensitive data only (connection name, adapter type,
// and the adapter's grantable action vocabulary). It is therefore auth-only —
// any authenticated caller may read their own tenant's catalog — with no
// route → scope enforcement. The list is tenant-scoped from the verified token,
// so a caller can never see another tenant's connections.
type ConnectionsReadController struct {
	connRegistry credentials.ConnectionRepository
	registry     *AdapterRegistry
	verifier     *platformtoken.Verifier
	logger       logging.Logger
}

// NewConnectionsReadController constructs the read-only connections controller.
func NewConnectionsReadController(
	connRegistry credentials.ConnectionRepository,
	registry *AdapterRegistry,
	verifier *platformtoken.Verifier,
	logger logging.Logger,
) *ConnectionsReadController {
	return &ConnectionsReadController{
		connRegistry: connRegistry,
		registry:     registry,
		verifier:     verifier,
		logger:       logger,
	}
}

// GroupedBy returns the route group prefix for this controller.
func (c *ConnectionsReadController) GroupedBy() string { return "" }

// Middleware enforces platform JWT auth on this controller's routes only. The
// server's health endpoint is wired outside the controller mux and stays open;
// scoping AuthMiddleware here (rather than on the whole server) keeps that
// invariant while requiring a token for GET /connections.
func (c *ConnectionsReadController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(mw.AuthMiddleware(c.verifier, c.logger))
}

// Boot registers the read-only routes.
func (c *ConnectionsReadController) Boot(mux *api.Mux) {
	mux.Handle("GET /connections", c.handleListConnections)
}

// connectionJSON is the wire shape for one connection in the list response.
// actions is the connection's adapter's declared action vocabulary; an
// unregistered adapter yields an empty list rather than an error.
type connectionJSON struct {
	Name    string   `json:"name"`
	Adapter string   `json:"adapter"`
	Actions []string `json:"actions"`
}

// handleListConnections returns the caller's tenant's connections. The result
// is scoped to claims.Orbit.TenantID read from the verified token; a request
// without a tenant is rejected 401.
func (c *ConnectionsReadController) handleListConnections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}
	tenantID := claims.Orbit.TenantID

	conns, err := c.connRegistry.ListByTenant(ctx, tenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list connections", nil), err)
		return
	}

	items := make([]connectionJSON, 0, len(conns))
	for _, conn := range conns {
		items = append(items, connectionJSON{
			Name:    conn.Name,
			Adapter: string(conn.Adapter),
			Actions: c.actionsFor(conn.Adapter),
		})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"connections": items})
}

// actionsFor returns the action strings declared by the adapter for adapterType.
// An adapter not present in the registry yields an empty (non-nil) slice so the
// JSON field serializes as [] rather than null.
func (c *ConnectionsReadController) actionsFor(adapterType credentials.AdapterType) []string {
	adapter, ok := c.registry.Lookup(adapterType)
	if !ok {
		return []string{}
	}
	decls := adapter.Actions()
	actions := make([]string, 0, len(decls))
	for _, d := range decls {
		actions = append(actions, d.Action)
	}
	return actions
}
