package devenvironments

import (
	"errors"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
)

// Controller wires the devenvironments service to HTTP routes. It is a
// self-contained api.Controller mounted by the identity binary (decision 5:
// dev environments have nothing to do with agent execution, so they are
// hosted alongside identity rather than orbit-agents).
type Controller struct {
	svc      *Service
	authMW   mw.Middleware
	eval     policy.Evaluator
	routes   []routescopes.RouteScope
	basePath string
	logger   logging.Logger
}

// NewController builds the devenvironments controller. authMW is the
// platform JWT auth middleware supplied by the identity service; eval/routes
// drive ScopeEnforcementMiddleware on every route.
func NewController(svc *Service, authMW mw.Middleware, eval policy.Evaluator, routes []routescopes.RouteScope, basePath string, logger logging.Logger) *Controller {
	return &Controller{svc: svc, authMW: authMW, eval: eval, routes: routes, basePath: basePath, logger: logger}
}

// GroupedBy returns "" — routes are registered relative to the server base
// path ("/api/v1/dev-environments/..."), matching the ADR's route names.
func (c *Controller) GroupedBy() string { return "" }

// Middleware returns nil; auth + scope enforcement is applied per-route below.
func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }

// Boot registers the devenvironments routes.
//
//	POST   /api/v1/dev-environments               Provision a new environment (devenvironments:write).
//	GET    /api/v1/dev-environments               List the caller's own environments (devenvironments:read).
//	GET    /api/v1/dev-environments/all           List every environment, any owner (devenvironments:admin).
//	GET    /api/v1/dev-environments/{id}          Get an environment the caller owns (devenvironments:read).
//	POST   /api/v1/dev-environments/{id}/extend   Raise an environment's TTL (devenvironments:write).
//	POST   /api/v1/dev-environments/{id}/wake     Explicit wake from sleep (devenvironments:write).
//	POST   /api/v1/dev-environments/{id}/kubeconfig  Broker a short-lived kubeconfig (devenvironments:write).
//	DELETE /api/v1/dev-environments/{id}          Destroy an environment (devenvironments:write).
func (c *Controller) Boot(mux *api.Mux) {
	auth := mw.MiddlewareChain(
		c.authMW,
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)

	mux.HandleWithMiddleware("POST /dev-environments", auth, c.handleCreate)
	mux.HandleWithMiddleware("GET /dev-environments", auth, c.handleList)
	// Registered before the wildcard GET below so the literal "all" segment
	// is never captured as {id} — mirrors identity's own /tokens/attribution
	// vs /tokens/{id} ordering.
	mux.HandleWithMiddleware("GET /dev-environments/all", auth, c.handleListAll)
	mux.HandleWithMiddleware("GET /dev-environments/{id}", auth, c.handleGet)
	mux.HandleWithMiddleware("POST /dev-environments/{id}/extend", auth, c.handleExtend)
	mux.HandleWithMiddleware("POST /dev-environments/{id}/wake", auth, c.handleWake)
	mux.HandleWithMiddleware("POST /dev-environments/{id}/kubeconfig", auth, c.handleKubeconfig)
	mux.HandleWithMiddleware("DELETE /dev-environments/{id}", auth, c.handleDestroy)
}

func (c *Controller) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	env, err := c.svc.CreateEnvironment(ctx, req)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusCreated, env)
}

func (c *Controller) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	envs, err := c.svc.ListEnvironments(ctx)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	if envs == nil {
		envs = []Environment{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"environments": envs})
}

func (c *Controller) handleListAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	envs, err := c.svc.ListAllEnvironments(ctx)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	if envs == nil {
		envs = []Environment{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"environments": envs})
}

func (c *Controller) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	env, err := c.svc.GetEnvironment(ctx, r.PathValue("id"))
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, env)
}

func (c *Controller) handleExtend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req ExtendRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.TTL == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "ttl is required", map[string]any{"field": "ttl"}), nil)
		return
	}
	env, err := c.svc.ExtendEnvironment(ctx, r.PathValue("id"), req)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, env)
}

func (c *Controller) handleWake(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	env, err := c.svc.WakeEnvironment(ctx, r.PathValue("id"))
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, env)
}

func (c *Controller) handleKubeconfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := c.svc.Kubeconfig(ctx, r.PathValue("id"))
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

func (c *Controller) handleDestroy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := c.svc.DestroyEnvironment(ctx, r.PathValue("id")); err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeServiceError maps service-layer sentinel errors to structured HTTP responses.
func (c *Controller) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	switch {
	case errors.Is(err, ErrNotFound):
		api.ErrorResponse(ctx, w, c.logger,
			api.NotFoundError(api.ErrCodeResourceNotFound, "dev environment not found", nil), nil)
	case errors.Is(err, ErrQuotaExceeded):
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError("devenvironments_quota_exceeded", "active dev environment quota exceeded", nil), nil)
	case errors.Is(err, ErrTTLExceedsHardCap):
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField, err.Error(), map[string]any{"field": "ttl"}), nil)
	case errors.Is(err, ErrValidation):
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeValidationFailed, err.Error(), nil), nil)
	case errors.Is(err, ErrKubeconfigSigningTimedOut):
		api.ErrorResponse(ctx, w, c.logger,
			api.ServiceUnavailableError("devenvironments_kubeconfig_sign_timeout",
				"the operator did not sign the kubeconfig request in time — retry", nil), nil)
	default:
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "dev environment operation failed", nil), err)
	}
}
