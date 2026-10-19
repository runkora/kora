package agents

import (
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ConnectionCatalogController serves the read-only connection catalog for the
// caller's tenant on orbit-agents.
//
// The catalog was previously served by the egress-gateway, but that service is
// a CONNECT/egress broker carrying special istio config the frontend cannot
// reach over plain HTTP through the mesh. orbit-agents is a normal HTTP service
// the frontend already reaches (it serves agent-types), so the read-only catalog
// lives here. The egress-gateway reads the same orbit-tenant-connections
// ConfigMap for brokering; this controller reads it only to render the picker.
//
// The response carries non-sensitive data only (connection name, adapter type,
// and the adapter's static action vocabulary). It is therefore auth-only — any
// authenticated caller may read their own tenant's catalog — with no route →
// scope enforcement, mirroring the agent-types reads' reliance on the verified
// platform token for tenant scoping. The list is tenant-scoped from the token,
// so a caller can never see another tenant's connections.
type ConnectionCatalogController struct {
	catalog  *ConnectionCatalog
	verifier *token.Verifier
	logger   logging.Logger
}

// NewConnectionCatalogController constructs the read-only catalog controller.
func NewConnectionCatalogController(
	catalog *ConnectionCatalog,
	verifier *token.Verifier,
	logger logging.Logger,
) *ConnectionCatalogController {
	return &ConnectionCatalogController{
		catalog:  catalog,
		verifier: verifier,
		logger:   logger,
	}
}

// GroupedBy returns the route group prefix for this controller.
func (c *ConnectionCatalogController) GroupedBy() string { return "" }

// Middleware enforces platform JWT auth on this controller's routes only. Unlike
// the main agents Controller it omits ScopeEnforcementMiddleware: the catalog is
// a read-only, non-sensitive surface gated by tenant scoping from the verified
// token, exactly like the egress-gateway's read endpoint it replaces.
func (c *ConnectionCatalogController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(mw.AuthMiddleware(c.verifier, c.logger))
}

// Boot registers the read-only catalog route.
func (c *ConnectionCatalogController) Boot(mux *api.Mux) {
	mux.Handle("GET /api/v1/connections", c.handleListConnections)
}

// handleListConnections returns the caller's tenant's connection catalog. The
// result is scoped to claims.Kora.TenantID read from the verified token; a
// request without a tenant is rejected 401.
func (c *ConnectionCatalogController) handleListConnections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims.Kora.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}

	conns, err := c.catalog.ListByTenant(ctx, claims.Kora.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list connections", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"connections": conns})
}
