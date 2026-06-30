// Read-only MCP tool catalog API (Phase 3.4).
//
// Serves GET /v1/catalog — the caller's tenant's tools plus all global tools —
// for the admin UI. The tool-gateway is a plain-HTTP mesh service, so the BFF
// reaches this directly. The response is non-sensitive (server, tool, scope,
// description, schema), so it is auth-only: any authenticated caller may read
// their own tenant's catalog, scoped from the verified token.
package toolgateway

import (
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
)

// CatalogController serves the read-only MCP tool catalog.
type CatalogController struct {
	repo     CatalogRepository
	verifier *platformtoken.Verifier
	logger   logging.Logger
}

// NewCatalogController constructs the catalog controller.
func NewCatalogController(repo CatalogRepository, verifier *platformtoken.Verifier, logger logging.Logger) *CatalogController {
	return &CatalogController{repo: repo, verifier: verifier, logger: logger}
}

// GroupedBy returns the route group prefix (none — routes sit under basePath).
func (c *CatalogController) GroupedBy() string { return "" }

// Middleware enforces platform JWT auth on this controller's routes. The health
// endpoint is wired outside the controller mux and stays open.
func (c *CatalogController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(mw.AuthMiddleware(c.verifier, c.logger))
}

// Boot registers the read-only catalog route.
func (c *CatalogController) Boot(mux *api.Mux) {
	mux.Handle("GET /catalog", c.handleListCatalog)
}

// handleListCatalog returns the caller's tenant's tools (plus global tools),
// scoped to claims.Orbit.TenantID from the verified token.
func (c *CatalogController) handleListCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}

	tools, err := c.repo.ListByTenant(ctx, claims.Orbit.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list MCP tool catalog", nil), err)
		return
	}

	if tools == nil {
		tools = []CatalogTool{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"tools": tools})
}
