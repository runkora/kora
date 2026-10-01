package api

import (
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policyservice/routes"
)

// registerRoutesRequest is the body for POST /policy/routes/register.
// The tenant scope (global vs tenant-specific) is derived from the caller's
// platform token — it is not a caller-supplied field.
type registerRoutesRequest struct {
	Service string                    `json:"service"`
	Routes  []routes.RouteScope       `json:"routes"`
	Scopes  []routes.ScopeDeclaration `json:"scopes,omitempty"`
	// SkipPrune upserts the declared scopes without the destructive orphan
	// prune (DeleteOrphans + PurgeScopeFromAll). Default false preserves the
	// prune-on-publish behavior. Honored only for global (SYSTEM) registrations
	// — tenant registrations never prune.
	SkipPrune bool `json:"skip_prune,omitempty"`
}

// handleListRoutes handles GET /policy/routes.
// Returns all registered route→scope mappings.
// Any authenticated principal can read routes — the data is not sensitive
// and platform-admins need visibility for the policy dashboard.
func (c *Controller) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if _, ok := c.claimsFromContext(w, r); !ok {
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK,
		map[string]any{"registrations": c.svc.ListRoutes()})
}

// handleRegisterRoutes handles POST /policy/routes/register.
// The caller's principal type determines registration scope:
//   - SYSTEM → global registration (compiled into every tenant bundle)
//   - SERVICE → tenant-scoped registration (compiled into that tenant's bundle only;
//     all required_scopes must exist in the tenant's vocabulary)
//
// Either way the caller may only register its own service's routes.
// AGENT and HUMAN principals are rejected — route registration is a service-only operation.
func (c *Controller) handleRegisterRoutes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	var tenantID string
	switch claims.Orbit.PrincipalType {
	case token.PrincipalTypeSystem:
		tenantID = ""
	case token.PrincipalTypeService:
		tenantID = claims.Orbit.TenantID
	default:
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"route registration requires a SYSTEM or SERVICE principal", nil), nil)
		return
	}

	var req registerRoutesRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.Service == "" || len(req.Routes) == 0 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField,
				"service and routes are required", nil), nil)
		return
	}

	// A caller registers, and so prunes, only its own service's routes and scopes.
	// A SYSTEM token names its SPIFFE component in display_name; a SERVICE token names itself in sub.
	if req.Service != ownService(claims) {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"service identity mismatch: cannot register routes for another service", nil), nil)
		return
	}

	if err := c.svc.RegisterRoutesWithPrune(ctx, req.Service, tenantID, req.Routes, req.Scopes, !req.SkipPrune); err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, routesErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ownService is the service name the caller may register routes under.
func ownService(claims *token.PlatformClaims) string {
	if claims.Orbit.PrincipalType == token.PrincipalTypeSystem {
		return claims.Orbit.DisplayName
	}
	return claims.Subject
}

func routesErrorResponse(err error) *platformapi.APIError {
	var scopeErr *ScopeVocabularyError
	if isScopeVocabularyError(err, &scopeErr) {
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"scope '"+scopeErr.Scope+"' is not in the scope vocabulary",
			map[string]any{"scope": scopeErr.Scope})
	}
	return platformapi.InternalError(platformapi.ErrCodeDatabaseError, "route registration failed", nil)
}
