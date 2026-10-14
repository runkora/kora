package api

import (
	"errors"
	"net/http"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
)

// registerRoutesRequest is the body for POST /policy/routes/register.
// The tenant scope (global vs tenant-specific) is derived from the caller's
// platform token, except that a SYSTEM caller may name the tenant itself.
type registerRoutesRequest struct {
	Service string                    `json:"service"`
	Routes  []routes.RouteScope       `json:"routes"`
	Scopes  []routes.ScopeDeclaration `json:"scopes,omitempty"`
	// TenantID is the tenant to register under. Only a SYSTEM caller may name a
	// tenant (empty means global); any other caller is bound to its token's tenant.
	TenantID string `json:"tenant_id,omitempty"`
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
//   - SYSTEM → global registration (compiled into every tenant bundle), or a
//     tenant-scoped one when the request names a tenant_id
//   - SERVICE → tenant-scoped registration (compiled into that tenant's bundle only;
//     it may not declare scopes, and all required_scopes must already exist)
//
// Either way the caller may only register its own service's routes.
// AGENT and HUMAN principals are rejected — route registration is a service-only operation.
func (c *Controller) handleRegisterRoutes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	switch claims.Kora.PrincipalType {
	case token.PrincipalTypeSystem, token.PrincipalTypeService:
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

	// SYSTEM names the tenant ("" is global); a SERVICE is bound to its token's tenant.
	tenantID, ok := c.resolveTenantID(w, r, claims, req.TenantID)
	if !ok {
		return
	}

	// A registration publishes routes, scopes, or both. Scopes alone suit a
	// service whose routes stay unpublished.
	if req.Service == "" || (len(req.Routes) == 0 && len(req.Scopes) == 0) {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField,
				"service and routes or scopes are required", nil), nil)
		return
	}

	// The vocabulary is tenant-keyed, but a tenant principal may still not write
	// to it: only SYSTEM declares scopes, for a tenant or globally.
	if claims.Kora.PrincipalType != token.PrincipalTypeSystem && len(req.Scopes) > 0 && tenantID != "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden, ErrTenantScopeDeclaration.Error(), nil), nil)
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
	if claims.Kora.PrincipalType == token.PrincipalTypeSystem {
		return claims.Kora.DisplayName
	}
	return claims.Subject
}

func routesErrorResponse(err error) *platformapi.APIError {
	if errors.Is(err, ErrTenantScopeDeclaration) {
		return platformapi.ForbiddenError(platformapi.ErrCodeForbidden, ErrTenantScopeDeclaration.Error(), nil)
	}
	if errors.Is(err, ErrInvalidRegistration) {
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField, err.Error(), nil)
	}
	var scopeErr *ScopeVocabularyError
	if isScopeVocabularyError(err, &scopeErr) {
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"scope '"+scopeErr.Scope+"' is not in the scope vocabulary",
			map[string]any{"scope": scopeErr.Scope})
	}
	return platformapi.InternalError(platformapi.ErrCodeDatabaseError, "route registration failed", nil)
}
