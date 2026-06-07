package egressgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/clear-route/orbit/internal/credentials"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// AdminController is the gateway's HTTP surface for managing connections.
// Tenant admins (or operator automation) use it to register, list, and delete
// connections; the gateway itself uses the registry on the egress hot path.
//
// Auth + scope:
//
//	POST   /api/v1/connections        platform:connections:write
//	GET    /api/v1/connections        platform:connections:read
//	GET    /api/v1/connections/{id}   platform:connections:read
//	DELETE /api/v1/connections/{id}   platform:connections:write
//
// Tenant scoping is implicit: the caller's tenant from claims is the tenant the
// operation runs against. Cross-tenant administration is not supported.
type AdminController struct {
	repo     credentials.ConnectionRepository
	verifier *token.Verifier
	eval     policy.Evaluator
	logger   logging.Logger
	basePath string
}

// NewAdminController constructs an AdminController.
func NewAdminController(
	repo credentials.ConnectionRepository,
	verifier *token.Verifier,
	eval policy.Evaluator,
	basePath string,
	logger logging.Logger,
) *AdminController {
	return &AdminController{repo: repo, verifier: verifier, eval: eval, basePath: basePath, logger: logger}
}

// GroupedBy adds no extra group prefix: the server base path ("/api/v1") is the
// only prefix, so admin URLs are "/api/v1/connections".
func (c *AdminController) GroupedBy() string { return "" }

// Middleware returns the controller-level middleware chain applied to every
// admin route. AuthMiddleware verifies the platform JWT; ScopeEnforcementMiddleware
// gates each route on the scope declared in AdminRoutes().
func (c *AdminController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.ScopeEnforcementMiddleware(c.eval, AdminRoutes(), c.basePath, c.logger),
	)
}

// Boot registers the admin connection routes. Paths are RELATIVE to the server
// base path ("/api/v1"), which http.StripPrefix removes before the controller
// mux runs; the absolute paths are "/api/v1/connections", matching AdminRoutes()
// and the scopes registered with the policy service. Repeating "/api/v1" here
// would double the prefix to "/api/v1/api/v1/connections".
func (c *AdminController) Boot(mux *platformapi.Mux) {
	mux.Handle("POST /connections", c.handleCreate)
	mux.Handle("GET /connections", c.handleList)
	mux.Handle("GET /connections/{id}", c.handleGet)
	mux.Handle("DELETE /connections/{id}", c.handleDelete)
}

// ── Handlers ─────────────────────────────────────────────────────────────────

// connectionRequest is the wire format for POST /api/v1/connections.
//
// Note: admins do NOT submit raw secret material through this API. They upload
// the secret to the per-tenant Kubernetes Secret separately (operator tooling
// or kubectl), and reference it here by its relative path under the gateway's
// connection-secret root.
type connectionRequest struct {
	Name         string                  `json:"name"`
	Adapter      credentials.AdapterType `json:"adapter"`
	BaseHost     string                  `json:"base_host"`
	SecretRef    string                  `json:"secret_ref"`
	ScopeCeiling []string                `json:"scope_ceiling"`
	Config       json.RawMessage         `json:"config"`
}

func (c *AdminController) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, ok := c.tenantFromClaims(ctx, w)
	if !ok {
		return
	}

	var req connectionRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if err := validateConnection(req); err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeValidationFailed, err.Error(), nil), err)
		return
	}

	conn := &credentials.Connection{
		TenantID:     tenantID,
		Name:         req.Name,
		Adapter:      req.Adapter,
		BaseHost:     req.BaseHost,
		SecretRef:    req.SecretRef,
		ScopeCeiling: req.ScopeCeiling,
		Config:       req.Config,
	}
	if err := c.repo.Create(ctx, conn); err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeDatabaseError, "create connection", nil), err)
		return
	}
	platformapi.JSON(ctx, w, c.logger, http.StatusCreated, conn)
}

func (c *AdminController) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, ok := c.tenantFromClaims(ctx, w)
	if !ok {
		return
	}
	conns, err := c.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeDatabaseError, "list connections", nil), err)
		return
	}
	if conns == nil {
		conns = []*credentials.Connection{}
	}
	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"connections": conns})
}

func (c *AdminController) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, ok := c.tenantFromClaims(ctx, w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	conn, err := c.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		c.repoErrorResponse(ctx, w, err)
		return
	}
	platformapi.JSON(ctx, w, c.logger, http.StatusOK, conn)
}

func (c *AdminController) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, ok := c.tenantFromClaims(ctx, w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := c.repo.Delete(ctx, id, tenantID); err != nil {
		c.repoErrorResponse(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (c *AdminController) tenantFromClaims(ctx context.Context, w http.ResponseWriter) (string, bool) {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims.Orbit.TenantID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "missing tenant", nil), nil)
		return "", false
	}
	return claims.Orbit.TenantID, true
}

func (c *AdminController) repoErrorResponse(ctx context.Context, w http.ResponseWriter, err error) {
	if errors.Is(err, credentials.ErrConnectionNotFound) {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "connection not found", nil), err)
		return
	}
	platformapi.ErrorResponse(ctx, w, c.logger,
		platformapi.InternalError(platformapi.ErrCodeDatabaseError, "connection lookup", nil), err)
}

func validateConnection(req connectionRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.Adapter == "" {
		return fmt.Errorf("adapter is required")
	}
	if req.BaseHost == "" {
		return fmt.Errorf("base_host is required")
	}
	// secret_ref is required for every adapter that resolves or mints a
	// credential. The passthrough adapter injects nothing, so it has no secret
	// to reference — requiring one would be meaningless.
	if req.SecretRef == "" && req.Adapter != credentials.AdapterPassthrough {
		return fmt.Errorf("secret_ref is required")
	}
	return nil
}
