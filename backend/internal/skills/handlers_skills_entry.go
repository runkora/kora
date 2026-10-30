package skills

import (
	"context"
	"errors"
	"net/http"
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
	service       *Service
	registry      marketplaceRegistry
	logger        logging.Logger
	middleware    mw.MiddlewareHandler
	localTenantID string
}

// NewController protects every route with authentication and marketplace:read.
func NewController(
	logger logging.Logger,
	service *Service,
	registry marketplaceRegistry,
	verifier *token.Verifier,
	eval policy.Evaluator,
	basePath string,
	localTenantID string,
) *Controller {
	if verifier == nil || eval == nil {
		panic("skills controller requires a token verifier and policy evaluator")
	}

	if basePath == "/" {
		basePath = ""
	}

	var middleware mw.MiddlewareHandler

	if localTenantID != "" {
		// Local development: bypass auth, delegation, and scope checks.
		middleware = func(next http.Handler) http.Handler {
			return next
		}
	} else {
		middleware = mw.MiddlewareChain(
			mw.AuthMiddleware(verifier, logger),
			mw.DelegationMiddleware(logger, verifier),
			mw.ScopeEnforcementMiddleware(
				eval,
				MarketplaceRoutes(basePath),
				basePath,
				logger,
			),
		)
	}

	return &Controller{
		service:       service,
		registry:      registry,
		logger:        logger,
		localTenantID: localTenantID,
		middleware:    middleware,
	}
}

// MarketplaceRoutes declares the scope required by every marketplace route.
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
func (c *Controller) GroupedBy() string {
	return ""
}

// Middleware verifies identity and delegation, then checks marketplace:read.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	return c.middleware
}

// Boot registers all marketplace endpoints on the platform mux.
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /marketplaces", c.handleListMarketplaces)
	mux.Handle("GET /marketplaces/{marketplaceSlug}", c.handleGetMarketplace)
	mux.Handle("GET /marketplaces/{marketplaceSlug}/{skillName}", c.handleGetSkill)
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
		marketplaces = append(marketplaces, map[string]string{
			"name":            source.Name,
			"marketplaceSlug": source.MarketplaceSlug,
			"link":            source.Link,
		})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"marketplaces": marketplaces})
}

// handleGetMarketplace returns the selected marketplace and up to serverCap of
// its skills. Clients pass the previous response's nextCursor as ?cursor= to
// fetch the next batch, and ?search= for full-text search ordered by relevance.
func (c *Controller) handleGetMarketplace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID, ok := c.tenantID(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	result, err := c.service.GetMarketplace(
		ctx,
		tenantID,
		r.PathValue("marketplaceSlug"),
		query.Get("search"),
		query.Get("cursor"),
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

	marketplaceSlug := r.PathValue("marketplaceSlug")
	// Accept a full skillSlug ({marketplaceSlug}__{skillName}) as well, for
	// callers that still address skills by slug.
	skillName := strings.TrimPrefix(r.PathValue("skillName"), marketplaceSlug+"__")

	// skillName is the slugified name segment of a skillSlug. Anything else,
	// including another marketplace's slug or a value with a slash, is malformed.
	if !skillNamePattern.MatchString(skillName) {
		apiErr := api.ValidationError(
			api.ErrCodeInvalidField,
			"skillName must be lowercase letters, digits, and single hyphens",
			nil,
		)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	skill, err := c.service.GetSkill(ctx, tenantID, marketplaceSlug, skillName)
	if err != nil {
		c.respondError(w, r, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, skill)
}

// tenantID returns the tenant ID from the verified platform token.
// In local development mode the configured localTenantID is used instead.
func (c *Controller) tenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if c.localTenantID != "" {
		return c.localTenantID, true
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
	case errors.Is(err, ErrInvalidCursor):
		apiErr = api.ValidationError(api.ErrCodeInvalidField, "cursor is invalid", nil)

	case errors.Is(err, ErrInvalidSearch):
		apiErr = api.ValidationError(api.ErrCodeInvalidField, ErrInvalidSearch.Error(), nil)

	case errors.Is(err, ErrMarketplaceNotFound):
		apiErr = api.NotFoundError(api.ErrCodeResourceNotFound, "marketplace not found", nil)

	case errors.Is(err, ErrSkillNotFound):
		apiErr = api.NotFoundError(api.ErrCodeResourceNotFound, "skill not found", nil)

	case errors.Is(err, ErrRedisUnavailable),
		errors.Is(err, ErrSearchUnavailable),
		errors.Is(err, ghclient.ErrUnavailable),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		apiErr = api.ServiceUnavailableError(api.ErrCodeServiceUnavailable, "marketplace is temporarily unavailable", nil)

	default:
		apiErr = api.InternalError(api.ErrCodeInternalError, "failed to read marketplace", nil)
	}

	api.ErrorResponse(r.Context(), w, c.logger, apiErr, err)
}
