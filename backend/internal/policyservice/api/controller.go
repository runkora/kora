// Package api provides the HTTP handlers for the Policy Service (PAP).
// It exposes grant CRUD and scope vocabulary management endpoints.
// All endpoints are tenant-scoped — cross-tenant operations are not possible.
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// ReservedRoleError is returned when a request attempts to modify the
// reserved platform-admin role.
type ReservedRoleError struct {
	Role string
}

func (e *ReservedRoleError) Error() string {
	return fmt.Sprintf("role %q is reserved and cannot be modified", e.Role)
}

// isScopeVocabularyError is a shared helper for handlers that validate scopes.
func isScopeVocabularyError(err error, target **ScopeVocabularyError) bool {
	return errors.As(err, target)
}

// Controller wires the policy service to HTTP routes.
type Controller struct {
	svc      *Service
	logger   logging.Logger
	verifier *token.Verifier
	// blocklist refuses revoked delegation tokens before they expire.
	blocklist mw.JTIBlocklist
	eval      policy.Evaluator
	routes    []routescopes.RouteScope
	basePath  string
}

// NewController creates a ready-to-boot policy Controller.
//
// eval and routes are passed to ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). The
// route table should cover every authenticated route registered in Boot.
// Routes intentionally omitted from the table (e.g. POST /effective-scopes,
// POST /routes/register) fail-closed at the middleware with 403. basePath
// must match the server's BasePath so the middleware can reconstruct absolute
// paths after http.StripPrefix.
func NewController(svc *Service, logger logging.Logger, verifier *token.Verifier, blocklist mw.JTIBlocklist, eval policy.Evaluator, routes []routescopes.RouteScope, basePath string) *Controller {
	return &Controller{svc: svc, logger: logger, verifier: verifier, blocklist: blocklist, eval: eval, routes: routes, basePath: basePath}
}

func (c *Controller) GroupedBy() string { return "/policy" }

// Middleware applies JWT auth, delegation verification and scope enforcement to all policy routes
// except bundle polling endpoints.
//
// GET /bundles/{tenant_id} — polled by per-tenant OPA sidecars in tenant namespaces.
// GET /bundles/gateway     — polled by the OPA gateway pod in orbit-system only;
//
//	an Istio AuthorizationPolicy (authorizationpolicy-policy-service.yaml) denies
//	access from outside orbit-system, since the gateway bundle contains all tenants'
//	grants, roles, and routes.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	// The verified delegation bounds a delegated run's scope check by the person's access.
	authChain := mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.DelegationMiddlewareWithBlocklist(c.logger, c.verifier, c.blocklist),
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)
	return func(next http.Handler) http.Handler {
		authed := authChain(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bypass auth for GET requests to the bundle polling endpoints.
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/policy/bundles/") {
				next.ServeHTTP(w, r)
				return
			}
			authed.ServeHTTP(w, r)
		})
	}
}

// Boot registers all policy service routes.
//
// Route map:
//
//	GET    /policy/stats?tenant_id=                           Aggregate counts for dashboard overview
//	POST   /policy/grants                                     Create a grant
//	GET    /policy/grants/{id}                                Get a grant by ID
//	PATCH  /policy/grants/{id}                                Update a grant's budget in place
//	DELETE /policy/grants/{id}                                Delete a grant
//	PUT    /policy/tenant-budget                              Set the tenant's rollup spending cap
//	GET    /policy/tenant-budget?tenant_id=                   Get the tenant's cap
//	DELETE /policy/tenant-budget?tenant_id=                   Remove the tenant's cap
//	GET    /policy/grants?tenant_id=&principal_id=            List grants for a principal or tenant
//	GET    /policy/vocabulary                                 List scope vocabulary
//	GET    /policy/vocabulary/{scope}                         Get a single scope entry
//	PUT    /policy/roles                                      Create or replace a role→scope mapping
//	GET    /policy/roles?tenant_id=                           List role→scope mappings for a tenant
//	GET    /policy/roles/{role}?tenant_id=                    Get a single role→scope mapping
//	DELETE /policy/roles/{role}?tenant_id=                    Remove a role→scope mapping
//	GET    /policy/routes                                     List all registered route→scope mappings
//	POST   /policy/routes/register                            Register route→scope mappings (SYSTEM or SERVICE principal)
//	GET    /policy/bundles/{tenant_id}                        Serve OPA bundle (polling endpoint)
//	GET    /policy/effective-scopes                           Return the caller's full effective scope set
func (c *Controller) Boot(mux *platformapi.Mux) {
	mux.Handle("GET /stats", c.handleStats)
	mux.Handle("GET /effective-scopes", c.handleEffectiveScopes)
	mux.Handle("POST /grants", c.handleCreateGrant)
	mux.Handle("GET /grants/{id}", c.handleGetGrant)
	mux.Handle("PATCH /grants/{id}", c.handleUpdateGrantBudget)
	mux.Handle("DELETE /grants/{id}", c.handleDeleteGrant)
	mux.Handle("GET /grants", c.handleListGrants)
	mux.Handle("PUT /tenant-budget", c.handleUpsertTenantBudget)
	mux.Handle("GET /tenant-budget", c.handleGetTenantBudget)
	mux.Handle("DELETE /tenant-budget", c.handleDeleteTenantBudget)
	mux.Handle("GET /vocabulary", c.handleListVocabulary)
	mux.Handle("GET /vocabulary/{scope}", c.handleGetScope)
	mux.Handle("PUT /roles", c.handleUpsertRole)
	mux.Handle("GET /roles", c.handleListRoles)
	mux.Handle("GET /roles/{role}", c.handleGetRole)
	mux.Handle("DELETE /roles/{role}", c.handleDeleteRole)
	mux.Handle("GET /routes", c.handleListRoutes)
	mux.Handle("POST /routes/register", c.handleRegisterRoutes)
	mux.Handle("GET /bundles/{tenant_id}", c.svc.BundleHandler().ServeHTTP)
	mux.Handle("GET /bundles/gateway", c.svc.GatewayBundleHandler())
}

// grantErrorResponse maps service-layer grant errors to API error responses.
// Shared by all grant handlers.
func grantErrorResponse(err error) *platformapi.APIError {
	var scopeErr *ScopeVocabularyError
	switch {
	case errors.Is(err, ErrInvalidPrincipalType):
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField, err.Error(), nil)
	case errors.As(err, &scopeErr):
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"scope '"+scopeErr.Scope+"' is not in the scope vocabulary",
			map[string]any{"scope": scopeErr.Scope})
	case errors.Is(err, grants.ErrGrantNotFound):
		return platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "grant not found", nil)
	default:
		return platformapi.InternalError(platformapi.ErrCodeDatabaseError, "grant operation failed", nil)
	}
}

// vocabularyErrorResponse maps service-layer vocabulary errors to API error responses.
// Shared by all vocabulary handlers.
func vocabularyErrorResponse(err error) *platformapi.APIError {
	if errors.Is(err, vocabulary.ErrScopeNotFound) {
		return platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "scope not found", nil)
	}
	return platformapi.InternalError(platformapi.ErrCodeDatabaseError, "vocabulary operation failed", nil)
}
