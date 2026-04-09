// Package api provides the HTTP handlers for the Policy Service (PAP).
// It exposes grant CRUD and scope vocabulary management endpoints.
// All endpoints are tenant-scoped — cross-tenant operations are not possible.
package api

import (
	"errors"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// Controller wires the policy service to HTTP routes.
type Controller struct {
	svc    *Service
	logger logging.Logger
}

// NewController creates a ready-to-boot policy Controller.
func NewController(svc *Service, logger logging.Logger) *Controller {
	return &Controller{svc: svc, logger: logger}
}

func (c *Controller) GroupedBy() string                { return "/policy" }
func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }

// Boot registers all policy service routes.
//
// Route map:
//
//	POST   /policy/grants                           Create a grant
//	GET    /policy/grants/{id}                      Get a grant by ID
//	DELETE /policy/grants/{id}                      Delete a grant
//	GET    /policy/grants?tenant_id=&principal_id=  List grants for a principal or tenant
//	GET    /policy/vocabulary                       List scope vocabulary
//	POST   /policy/vocabulary                       Register a scope
//	DELETE /policy/vocabulary/{scope}               Remove a scope
//	GET    /policy/bundles/{tenant_id}              Serve OPA bundle (polling endpoint)
func (c *Controller) Boot(mux *platformapi.Mux) {
	mux.Handle("POST /grants", c.handleCreateGrant)
	mux.Handle("GET /grants/{id}", c.handleGetGrant)
	mux.Handle("DELETE /grants/{id}", c.handleDeleteGrant)
	mux.Handle("GET /grants", c.handleListGrants)
	mux.Handle("GET /vocabulary", c.handleListVocabulary)
	mux.Handle("POST /vocabulary", c.handleRegisterScope)
	mux.Handle("DELETE /vocabulary/{scope}", c.handleDeleteScope)
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
	switch {
	case errors.Is(err, vocabulary.ErrScopeAlreadyExists):
		return platformapi.ConflictError(platformapi.ErrCodeResourceExists, "scope already registered", nil)
	case errors.Is(err, vocabulary.ErrScopeNotFound):
		return platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "scope not found", nil)
	default:
		return platformapi.InternalError(platformapi.ErrCodeDatabaseError, "vocabulary operation failed", nil)
	}
}
