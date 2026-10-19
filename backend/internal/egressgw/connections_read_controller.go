package egressgw

import (
	"context"
	"net/http"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
)

// ConnectionsReadController serves the read-only connection catalog for the
// caller's tenant.
//
// GET /connections is the gateway's only HTTP route, and the policy service
// rejects a scope registration whose routes list is empty. The route therefore
// has to exist for the connection vocabulary to publish at all, even though the
// frontend picker reads its catalog from orbit-agents rather than from here.
//
// The response carries only a connection's name, adapter type, and grantable
// actions, so the route is auth-only with no scope enforcement. The list is
// tenant-scoped from the verified token.
type ConnectionsReadController struct {
	connRegistry connLister
	registry     *AdapterRegistry
	verifier     *platformtoken.Verifier
	logger       logging.Logger
}

// NewConnectionsReadController constructs the read-only connections controller.
func NewConnectionsReadController(
	connRegistry connLister,
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
	mux.Handle("GET /connections/{name}/resources", c.handleListConnectionResources)
}

// QualifierEnumerator is the optional connector capability behind
// GET /connections/{name}/resources: listing the upstream's current qualifier
// values (repository names, namespaces) for the connector attachment picker,
// through the connection's own credential. A connector without a qualifier
// dimension simply does not implement it. See
// docs/adr/2026-07-22-connection-scope-qualifiers.md (Decision D).
type QualifierEnumerator interface {
	ListQualifierValues(ctx context.Context) ([]string, error)
}

// handleListConnectionResources enumerates the named connection's qualifier
// values (e.g. the GitHub installation's repositories). Tenant-scoped from the
// verified token; a connection whose adapter has no enumerator yields 404.
func (c *ConnectionsReadController) handleListConnectionResources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}
	tenantID := claims.Orbit.TenantID

	name := r.PathValue("name")
	conns, err := c.connRegistry.ListByTenant(ctx, tenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list connections", nil), err)
		return
	}
	var conn *credentials.Connection
	for _, cn := range conns {
		if cn != nil && cn.Name == name {
			conn = cn
			break
		}
	}
	if conn == nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.NotFoundError(api.ErrCodeResourceNotFound, "connection not found", nil), nil)
		return
	}

	adapter, ok := c.registry.Lookup(conn.Adapter)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.NotFoundError(api.ErrCodeResourceNotFound, "connection adapter not registered", nil), nil)
		return
	}
	connector, err := adapter.Open(*conn)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "connection configuration is invalid", nil), err)
		return
	}
	enum, ok := connector.(QualifierEnumerator)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.NotFoundError(api.ErrCodeResourceNotFound, "connection declares no resource dimension", nil), nil)
		return
	}

	values, err := enum.ListQualifierValues(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to enumerate connection resources", nil), err)
		return
	}
	if values == nil {
		values = []string{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"resources": values})
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
