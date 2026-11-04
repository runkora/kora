package skills

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	"github.com/runkora/kora/backend/internal/platform/token"
)

const skillsReadScope = "skills:read"

// Error codes in skills-service responses.
const (
	codeInvalidRequest         = "invalid_request"
	codeNotFound               = "not_found"
	codeMarketplaceUnavailable = "marketplace_unavailable"
	codeUnavailable            = "unavailable"
	codeTimeout                = "timeout"
)

// skillReader is the service surface the handlers use.
type skillReader interface {
	ListMarketplaces(tenantID string) []MarketplaceSummary
	ListSkills(ctx context.Context, tenantID, slug, search string, offset, limit int) (SkillPage, error)
	GetSkill(ctx context.Context, tenantID, slug, path string) (SkillDetail, error)
}

// Controller serves the Marketplace API. Every route needs skills:read.
type Controller struct {
	service    skillReader
	logger     logging.Logger
	middleware mw.MiddlewareHandler
}

// NewController protects every route with authentication, delegation and the
// skills:read scope.
func NewController(
	logger logging.Logger,
	service skillReader,
	verifier *token.Verifier,
	eval policy.Evaluator,
	revoked mw.RevocationChecker,
	basePath string,
) *Controller {
	if verifier == nil || eval == nil || revoked == nil {
		panic("skills controller requires a token verifier, policy evaluator and JTI blocklist")
	}
	if basePath == "/" {
		basePath = ""
	}
	return &Controller{
		service: service,
		logger:  logger,
		middleware: mw.MiddlewareChain(
			mw.AuthMiddlewareWithRevocation(verifier, revoked, logger),
			mw.DelegationMiddlewareWithBlocklist(logger, verifier, revoked),
			mw.ScopeEnforcementMiddleware(eval, Routes(basePath), basePath, logger),
		),
	}
}

// Routes declares the scope every route requires.
func Routes(basePath string) []routescopes.RouteScope {
	return []routescopes.RouteScope{{
		PathPrefix:    strings.TrimRight(basePath, "/") + "/marketplaces",
		Method:        http.MethodGet,
		RequiredScope: skillsReadScope,
	}}
}

// Scopes declares the scope vocabulary skills-service owns.
func Scopes() []routescopes.ScopeDeclaration {
	return []routescopes.ScopeDeclaration{{
		Scope:        skillsReadScope,
		ResourceType: "skills",
		Action:       "read",
		Description:  "Browse skill Marketplaces and read skills",
	}}
}

// GroupedBy leaves paths explicit in Boot; the server supplies the base path.
func (c *Controller) GroupedBy() string { return "" }

// Middleware returns the auth, delegation and scope chain.
func (c *Controller) Middleware() mw.MiddlewareHandler { return c.middleware }

// Boot registers the routes.
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /marketplaces", c.handleListMarketplaces)
	mux.Handle("GET /marketplaces/{slug}", c.handleListSkills)
	mux.Handle("GET /marketplaces/{slug}/skills/{path...}", c.handleGetSkill)
}

func (c *Controller) handleListMarketplaces(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := c.tenantID(w, r)
	if !ok {
		return
	}
	api.JSON(r.Context(), w, c.logger, http.StatusOK, ListMarketplacesResponse{Marketplaces: c.service.ListMarketplaces(tenantID)})
}

func (c *Controller) handleListSkills(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := c.tenantID(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	offset, okOffset := parseInt(query.Get("offset"), 0, 0)
	limit, okLimit := parseInt(query.Get("limit"), DefaultLimit, 1)
	if !okOffset || !okLimit {
		c.respondError(w, r, ErrInvalidPage)
		return
	}
	page, err := c.service.ListSkills(r.Context(), tenantID, r.PathValue("slug"), query.Get("search"), offset, min(limit, MaxLimit))
	if err != nil {
		c.respondError(w, r, err)
		return
	}
	api.JSON(r.Context(), w, c.logger, http.StatusOK, page)
}

func (c *Controller) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := c.tenantID(w, r)
	if !ok {
		return
	}
	path := r.PathValue("path")
	if !isSkillPath(path) {
		c.respondError(w, r, ErrSkillNotFound)
		return
	}
	skill, err := c.service.GetSkill(r.Context(), tenantID, r.PathValue("slug"), path)
	if err != nil {
		c.respondError(w, r, err)
		return
	}
	api.JSON(r.Context(), w, c.logger, http.StatusOK, skill)
}

// isSkillPath rejects a path with an empty, "." or ".." segment. Only paths
// from the crawled tree are ever fetched, so this is defense in depth.
func isSkillPath(p string) bool {
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// parseInt reads a non-negative query value no lower than floor. An empty
// value gives def.
func parseInt(raw string, def, floor int) (int, bool) {
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil && n >= floor
}

// tenantID returns the tenant of the verified platform token.
func (c *Controller) tenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	claims, ok := token.ClaimsFromContext(r.Context())
	if !ok || claims == nil || claims.Kora.TenantID == "" {
		api.ErrorResponse(r.Context(), w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "tenant-scoped platform token required", nil), nil)
		return "", false
	}
	return claims.Kora.TenantID, true
}

func (c *Controller) respondError(w http.ResponseWriter, r *http.Request, err error) {
	// A caller that went away gets no response.
	if r.Context().Err() != nil {
		c.logger.Debug(r.Context(), "skills request cancelled", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	var apiErr *api.APIError
	switch {
	case errors.Is(err, ErrInvalidPage):
		apiErr = api.ValidationError(codeInvalidRequest, "offset must be 0 or more and limit 1 or more.", nil)
	case errors.Is(err, ErrMarketplaceNotFound):
		apiErr = api.NotFoundError(codeNotFound, "Marketplace not found.", nil)
	case errors.Is(err, ErrRepositoryNotFound):
		apiErr = api.NotFoundError(codeNotFound, "The repository or ref of this Marketplace was not found.", nil)
	case errors.Is(err, ErrSkillNotFound):
		apiErr = api.NotFoundError(codeNotFound, "Skill not found.", nil)
	case errors.Is(err, ErrFileNotFound):
		apiErr = api.NotFoundError(codeNotFound, "File not found.", nil)
	case errors.Is(err, ErrCredentialUnavailable):
		apiErr = statusError(http.StatusBadGateway, codeMarketplaceUnavailable, "The credential for this Marketplace is unavailable.")
	case errors.Is(err, ErrGitHubFailure):
		apiErr = statusError(http.StatusBadGateway, codeMarketplaceUnavailable, "This Marketplace could not be loaded. Try again later.")
	case errors.Is(err, storage.ErrRedisUnavailable):
		apiErr = statusError(http.StatusServiceUnavailable, codeUnavailable, "Skills are unavailable right now. Try again later.")
	case errors.Is(err, context.DeadlineExceeded):
		apiErr = statusError(http.StatusGatewayTimeout, codeTimeout, "Loading this Marketplace took too long. Try again later.")
	default:
		apiErr = api.InternalError(api.ErrCodeInternalError, "Failed to read the Marketplace.", nil)
	}
	api.ErrorResponse(r.Context(), w, c.logger, apiErr, err)
}

func statusError(status int, code, message string) *api.APIError {
	return &api.APIError{Code: code, Message: message, StatusCode: status}
}
