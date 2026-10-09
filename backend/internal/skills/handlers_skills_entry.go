package skills

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/clear-route/orbit/internal/platform/api"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

const marketplaceReadScope = "marketplace:read"

type marketplaceRegistry interface {
	ListSources(tenantID string) []MarketplaceSource
}

// Controller handles marketplace listing, skill listing, and skill details.
type Controller struct {
	service    *Service
	registry   marketplaceRegistry
	logger     logging.Logger
	middleware mw.MiddlewareHandler
}

func localAuthBypassEnabled() bool {
	return os.Getenv("APP_ENV") == "local" &&
		os.Getenv("LOCAL_AUTH_BYPASS") == "true"
}

// NewController protects every route with authentication and marketplace:read,
// except when the explicit local-development bypass is enabled.
func NewController(
	logger logging.Logger,
	service *Service,
	registry marketplaceRegistry,
	verifier *token.Verifier,
	eval policy.Evaluator,
	basePath string,
) *Controller {
	if basePath == "/" {
		basePath = ""
	}

	controller := &Controller{
		service:  service,
		registry: registry,
		logger:   logger,
	}

	if localAuthBypassEnabled() {
		logger.Info(context.Background(),
			"local authentication bypass enabled; using tenant-a")
		controller.middleware = func(next http.Handler) http.Handler {
			return next
		}
		return controller
	}

	if verifier == nil || eval == nil {
		panic("skills controller requires a token verifier and policy evaluator")
	}

	controller.middleware = mw.MiddlewareChain(
		mw.AuthMiddleware(verifier, logger),
		mw.DelegationMiddleware(logger, verifier),
		mw.ScopeEnforcementMiddleware(
			eval,
			MarketplaceRoutes(basePath),
			basePath,
			logger,
		),
	)

	return controller
}

// MarketplaceRoutes declares the scope required by all three marketplace routes.
func MarketplaceRoutes(basePath string) []routescopes.RouteScope {
	return []routescopes.RouteScope{{
		PathPrefix:    strings.TrimRight(basePath, "/") + "/marketplaces",
		Method:        http.MethodGet,
		RequiredScope: marketplaceReadScope,
	}}
}

// MarketplaceScopes declares the scope vocabulary owned by skills-service.
func MarketplaceScopes() []routescopes.ScopeDeclaration {
	return []routescopes.ScopeDeclaration{{
		Scope:        marketplaceReadScope,
		ResourceType: "marketplace",
		Action:       "read",
		Description:  "Browse marketplaces and read skills",
	}}
}

// GroupedBy leaves paths explicit in Boot; the server supplies the /v1 prefix.
func (c *Controller) GroupedBy() string { return "" }

// Middleware verifies identity and delegation, then checks marketplace:read.
func (c *Controller) Middleware() mw.MiddlewareHandler { return c.middleware }

// Boot registers all marketplace endpoints on the platform mux.
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /marketplaces", c.handleListMarketplaces)
	mux.Handle("GET /marketplaces/{marketplaceName}/skills", c.handleListSkills)
	mux.Handle("GET /marketplaces/{marketplaceName}/skills/{skillName}", c.handleGetSkill)
}

func (c *Controller) handleListMarketplaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, ok := c.tenantID(w, r)
	if !ok {
		return
	}

	sources := c.registry.ListSources(tenantID)
	marketplaces := make([]map[string]string, 0, len(sources))
	for _, source := range sources {
		marketplaces = append(marketplaces, map[string]string{"name": source.Name})
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"marketplaces": marketplaces})
}

func (c *Controller) handleListSkills(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, ok := c.tenantID(w, r)
	if !ok {
		return
	}

	result, err := c.service.ListSkills(
		ctx,
		tenantID,
		r.PathValue("marketplaceName"),
		r.URL.Query().Get("search"),
	)
	if err != nil {
		c.respondError(w, r, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, result)
}

func (c *Controller) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, ok := c.tenantID(w, r)
	if !ok {
		return
	}

	skillName := r.PathValue("skillName")
	if skillName == "" || skillName == "." || skillName == ".." || strings.Contains(skillName, "/") {
		apiErr := api.ValidationError(
			api.ErrCodeInvalidField,
			"skillName must be a single directory name",
			nil,
		)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	skill, err := c.service.GetSkill(
		ctx,
		tenantID,
		r.PathValue("marketplaceName"),
		skillName,
	)
	if err != nil {
		c.respondError(w, r, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, skill)
}

// tenantID comes from verified platform claims, except in explicit local testing.
func (c *Controller) tenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if localAuthBypassEnabled() {
		return "tenant-a", true
	}

	claims, ok := token.ClaimsFromContext(r.Context())
	if !ok || claims == nil || claims.Orbit.TenantID == "" {
		apiErr := api.UnauthorizedError(
			api.ErrCodeUnauthorized,
			"tenant-scoped platform token required",
			nil,
		)
		api.ErrorResponse(r.Context(), w, c.logger, apiErr, nil)
		return "", false
	}
	return claims.Orbit.TenantID, true
}

func (c *Controller) respondError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *api.APIError
	switch {
	case errors.Is(err, ErrMarketplaceNotFound):
		apiErr = api.NotFoundError(api.ErrCodeResourceNotFound, "marketplace not found", nil)
	case errors.Is(err, ErrSkillNotFound):
		apiErr = api.NotFoundError(api.ErrCodeResourceNotFound, "skill not found", nil)
	case errors.Is(err, ErrRedisUnavailable),
		errors.Is(err, ghclient.ErrUnavailable),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		apiErr = api.ServiceUnavailableError(
			api.ErrCodeServiceUnavailable,
			"marketplace is temporarily unavailable",
			nil,
		)
	default:
		apiErr = api.InternalError(api.ErrCodeInternalError, "failed to read marketplace", nil)
	}
	api.ErrorResponse(r.Context(), w, c.logger, apiErr, err)
}
