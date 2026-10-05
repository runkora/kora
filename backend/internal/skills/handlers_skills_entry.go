package skills

import (
	"context"
	"errors"
	"net/http"
	"path"
	"regexp"
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

const (
	marketplaceReadScope = "marketplace:read"
	defaultSkillsLimit   = 15
	maxSkillsLimit       = 100
)

var marketplaceSeparators = regexp.MustCompile(`[^a-z0-9]+`)

type marketplaceRegistry interface {
	ListSources(tenantID string) []MarketplaceSource
}

type marketplaceCache interface {
	Get(ctx context.Context, tenantID, org, repo string) ([]Skill, bool, error)
	Set(ctx context.Context, tenantID, org, repo string, skills []Skill) error
}

type marketplaceCrawler interface {
	Crawl(ctx context.Context, source MarketplaceSource) ([]Skill, error)
}

// Controller handles marketplace listing, skill listing, and skill details.
type Controller struct {
	registry   marketplaceRegistry
	cache      marketplaceCache
	crawler    marketplaceCrawler
	logger     logging.Logger
	middleware mw.MiddlewareHandler
}

// NewController protects every route with authentication and marketplace:read.
//
// Arguments:
//   - logger: records logs from handlers and middleware.
//   - registry: lists the marketplaces configured for each tenant.
//   - cache: reads and writes cached skill lists in Redis.
//   - crawler: fetches skill files and documents from GitHub on a cache miss.
//   - verifier: validates platform authentication tokens and delegation tokens.
//   - eval: checks whether policy allows the caller to read marketplaces and skills.
//   - basePath: the API prefix, such as "/v1"; it must match api.Server.
//
// Publish MarketplaceRoutes and MarketplaceScopes to policy-service when wiring
// this controller into the binary.
func NewController(
	logger logging.Logger,
	registry marketplaceRegistry,
	cache marketplaceCache,
	crawler marketplaceCrawler,
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
		registry: registry,
		cache:    cache,
		crawler:  crawler,
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
	offset, limit := 0, defaultSkillsLimit
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
		if err != nil || value < 1 || value > maxSkillsLimit {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, "limit must be an integer between 1 and 100", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		limit = value
	}

	skills, err := c.readSkills(ctx, tenantID, r.PathValue("marketplaceName"))
	if err != nil {
		c.respondError(w, r, err)
		return
	}

	search := strings.ToLower(query.Get("search"))
	items := make([]map[string]string, 0, len(skills))
	for _, skill := range skills {
		if !strings.Contains(strings.ToLower(skill.Name), search) {
			continue
		}
		items = append(items, map[string]string{
			"slug":        skill.Slug,
			"name":        skill.Name,
			"description": skill.Description,
		})
	}

	total := len(items)
	start := min(offset, total)
	// Subtract before adding so a very large offset cannot overflow or panic.
	end := start + min(limit, total-start)
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
		"skills": items[start:end],
		"pagination": map[string]int{
			"offset": offset, "limit": limit, "totalItems": total,
		},
	})
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

	skills, err := c.readSkills(ctx, tenantID, r.PathValue("marketplaceName"))
	if err != nil {
		c.respondError(w, r, err)
		return
	}

	for _, skill := range skills {
		if path.Base(skill.Slug) != skillName {
			continue
		}
		documents := skill.Documents
		if documents == nil {
			documents = []SkillFile{}
		}
		api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
			"name":        skill.Name,
			"description": skill.Description,
			"skill":       skill.Skill,
			"documents":   documents,
		})
		return
	}
	c.respondError(w, r, ErrSkillNotFound)
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

// readSkills finds the tenant's marketplace, reads Redis, and crawls on a miss.
func (c *Controller) readSkills(ctx context.Context, tenantID, marketplaceName string) ([]Skill, error) {
	var source MarketplaceSource
	found := false
	for _, candidate := range c.registry.ListSources(tenantID) {
		if marketplaceSlug(candidate.Name) != marketplaceName {
			continue
		}
		// Refuse ambiguous names rather than choosing an arbitrary repository.
		if found {
			return nil, ErrMarketplaceNotFound
		}
		source, found = candidate, true
	}
	if !found {
		return nil, ErrMarketplaceNotFound
	}

	skills, hit, err := c.cache.Get(ctx, tenantID, source.Org, source.Repo)
	if err != nil {
		return nil, err
	}
	if hit {
		return skills, nil
	}

	skills, err = c.crawler.Crawl(ctx, source)
	if err != nil {
		return nil, err
	}
	if err := c.cache.Set(ctx, tenantID, source.Org, source.Repo, skills); err != nil {
		return nil, err
	}
	return skills, nil
}

func marketplaceSlug(name string) string {
	return strings.Trim(marketplaceSeparators.ReplaceAllString(strings.ToLower(name), "-"), "-")
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
