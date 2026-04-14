// Package api provides the HTTP handlers for the Policy Service (PAP).
// It exposes grant CRUD and scope vocabulary management endpoints.
// All endpoints are tenant-scoped — cross-tenant operations are not possible.
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// ReservedRoleError is returned when a request attempts to modify a hardcoded
// platform admin role (tenant-admin, platform-admin).
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
}

// NewController creates a ready-to-boot policy Controller.
func NewController(svc *Service, logger logging.Logger, verifier *token.Verifier) *Controller {
	return &Controller{svc: svc, logger: logger, verifier: verifier}
}

func (c *Controller) GroupedBy() string { return "/policy" }

// Middleware applies JWT auth to all policy routes except GET /bundles/{tenant_id},
// which is polled by OPA sidecars running inside the cluster with no credentials.
// The bundle payload is compiled policy logic (not credentials or PII), so
// unauthenticated cluster-internal access is acceptable.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	authChain := mw.MiddlewareChain(mw.AuthMiddleware(c.verifier, c.logger))
	return func(next http.Handler) http.Handler {
		authed := authChain(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/bundles/") {
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
//	POST   /policy/grants                           Create a grant
//	GET    /policy/grants/{id}                      Get a grant by ID
//	DELETE /policy/grants/{id}                      Delete a grant
//	GET    /policy/grants?tenant_id=&principal_id=  List grants for a principal or tenant
//	GET    /policy/vocabulary                       List scope vocabulary
//	GET    /policy/vocabulary/{scope}               Get a single scope entry
//	PUT    /policy/roles                            Create or replace a role→scope mapping
//	GET    /policy/roles?tenant_id=                 List role→scope mappings for a tenant
//	GET    /policy/roles/{role}?tenant_id=          Get a single role→scope mapping
//	DELETE /policy/roles/{role}?tenant_id=          Remove a role→scope mapping
//	GET    /policy/routes                           List all registered route→scope mappings
//	POST   /policy/routes/register                  Register route→scope mappings (SYSTEM or SERVICE principal)
//	GET    /policy/bundles/{tenant_id}              Serve OPA bundle (polling endpoint)
func (c *Controller) Boot(mux *platformapi.Mux) {
	mux.Handle("POST /grants", c.handleCreateGrant)
	mux.Handle("GET /grants/{id}", c.handleGetGrant)
	mux.Handle("DELETE /grants/{id}", c.handleDeleteGrant)
	mux.Handle("GET /grants", c.handleListGrants)
	mux.Handle("GET /vocabulary", c.handleListVocabulary)
	mux.Handle("GET /vocabulary/{scope}", c.handleGetScope)
	mux.Handle("PUT /roles", c.handleUpsertRole)
	mux.Handle("GET /roles", c.handleListRoles)
	mux.Handle("GET /roles/{role}", c.handleGetRole)
	mux.Handle("DELETE /roles/{role}", c.handleDeleteRole)
	mux.Handle("GET /routes", c.handleListRoutes)
	mux.Handle("POST /routes/register", c.handleRegisterRoutes)
	mux.Handle("GET /bundles/{tenant_id}", c.svc.BundleHandler().ServeHTTP)
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
