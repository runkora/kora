package egressgw

import (
	"context"
	"net/http"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/api"
	platformconns "github.com/runkora/kora/backend/internal/platform/connections"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
)

// ConnectionsReadController serves the read-only connection catalog for the
// caller's tenant.
//
// GET /connections is the gateway's only HTTP route. The frontend picker reads
// its catalog from kora-agents rather than from here.
//
// GET /connections carries only a connection's name, adapter type, and
// grantable actions, so it is auth-only with no scope enforcement. The list is
// tenant-scoped from the verified token.
//
// GET /connections/{name}/resources mints a credential for the connection and
// calls the upstream, so it additionally requires the caller to hold the
// connection's baseline read scope (connection:{name}:{baseline}).
type ConnectionsReadController struct {
	connRegistry connLister
	registry     *AdapterRegistry
	policy       policyChecker
	verifier     *platformtoken.Verifier
	logger       logging.Logger
}

// NewConnectionsReadController constructs the read-only connections controller.
func NewConnectionsReadController(
	connRegistry connLister,
	registry *AdapterRegistry,
	pol policyChecker,
	verifier *platformtoken.Verifier,
	logger logging.Logger,
) *ConnectionsReadController {
	return &ConnectionsReadController{
		connRegistry: connRegistry,
		registry:     registry,
		policy:       pol,
		verifier:     verifier,
		logger:       logger,
	}
}

// GroupedBy returns the route group prefix for this controller.
func (c *ConnectionsReadController) GroupedBy() string { return "" }

// Middleware enforces platform JWT auth on this controller's routes only.
// GET /connections/{name}/resources also checks policy in its handler. The
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
// The caller must hold the connection's baseline read scope, checked before any
// credential is minted; otherwise 403.
func (c *ConnectionsReadController) handleListConnectionResources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Kora.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}
	tenantID := claims.Kora.TenantID

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

	// Authorize before Open / ListQualifierValues: enumeration mints a token.
	if !c.authorizeResources(w, r, claims, conn) {
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

// authorizeResources checks that the caller holds the connection's baseline read
// scope. It writes the error response and returns false when the caller may not
// enumerate: 403 for a deny or an adapter with no baseline action, 500 when the
// evaluator fails (fail closed).
func (c *ConnectionsReadController) authorizeResources(w http.ResponseWriter, r *http.Request, claims *platformtoken.PlatformClaims, conn *credentials.Connection) bool {
	ctx := r.Context()
	baselines := platformconns.BaselineActions(string(conn.Adapter))
	if len(baselines) == 0 {
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError(api.ErrCodeForbidden, "connection resources are not available", nil), nil)
		return false
	}
	segments, verb := scopePath(conn, baselines[0], "")
	decision, err := c.policy.Check(ctx, policy.CheckInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: resourceType,
		PathSegments: segments,
		Action:       verb,
	})
	if err != nil {
		c.logger.Error(ctx, "policy evaluation failed",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "error", Value: err.Error()},
		)
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "policy evaluation unavailable", nil), err)
		return false
	}
	if !decision.Allow {
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError(api.ErrCodeForbidden, "caller lacks the required grant for this connection", nil), nil)
		return false
	}
	return true
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
// is scoped to claims.Kora.TenantID read from the verified token; a request
// without a tenant is rejected 401.
func (c *ConnectionsReadController) handleListConnections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Kora.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}
	tenantID := claims.Kora.TenantID

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
