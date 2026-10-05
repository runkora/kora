package skills

import (
	"context"
	"errors"
	"net/http"
	"strconv"
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

// NewController protects every route with authentication and marketplace:read.
func NewController(
	logger logging.Logger,
	service *Service,
	registry marketplaceRegistry,
	verifier *token.Verifier,
	eval policy.Evaluator,
	basePath string,
) *Controller {
	if verifier == nil || eval == nil {
		panic("skills controller requires a token verifier and policy evaluator")
	}
	if basePath == "/" {
		basePath = ""
	}

	return &Controller{
		service:  service,
		registry: registry,
		logger:   logger,
		middleware: mw.MiddlewareChain(
			mw.AuthMiddleware(verifier, logger),
			mw.DelegationMiddleware(logger, verifier),
			mw.ScopeEnforcementMiddleware(eval, MarketplaceRoutes(basePath), basePath, logger),
		),
	}
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

	query := r.URL.Query()
	offset, limit := 0, defaultLimit
	if query.Has("offset") {
		value, err := strconv.Atoi(query.Get("offset"))
		if err != nil || value < 0 {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, "offset must be a non-negative integer", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		offset = value
	}
	if query.Has("limit") {
		value, err := strconv.Atoi(query.Get("limit"))
		if err != nil || value < 1 || value > maxLimit {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, "limit must be an integer between 1 and 100", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		limit = value
	}

	result, err := c.service.ListSkills(ctx, tenantID, r.PathValue("marketplaceName"), query.Get("search"), offset, limit)
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
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "skillName must be a single directory name", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	skill, err := c.service.GetSkill(ctx, tenantID, r.PathValue("marketplaceName"), skillName)
	if err != nil {
		c.respondError(w, r, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, skill)
}

// tenantID always comes from verified platform claims, never request parameters.
func (c *Controller) tenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	claims, ok := token.ClaimsFromContext(r.Context())
	if !ok || claims == nil || claims.Orbit.TenantID == "" {
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "tenant-scoped platform token required", nil)
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
	case errors.Is(err, ErrRedisUnavailable), errors.Is(err, ghclient.ErrUnavailable),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		apiErr = api.ServiceUnavailableError(api.ErrCodeServiceUnavailable, "marketplace is temporarily unavailable", nil)
	default:
		apiErr = api.InternalError(api.ErrCodeInternalError, "failed to read marketplace", nil)
	}
	api.ErrorResponse(r.Context(), w, c.logger, apiErr, err)
}
