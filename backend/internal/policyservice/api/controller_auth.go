package api

import (
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
)

// claimsFromContext retrieves the verified platform claims stored by AuthMiddleware.
// AuthMiddleware is applied at controller level so claims are always present —
// this is a safety net against misconfiguration.
func (c *Controller) claimsFromContext(w http.ResponseWriter, r *http.Request) (*token.PlatformClaims, bool) {
	ctx := r.Context()
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "platform token required", nil), nil)
		return nil, false
	}
	return claims, true
}

// resolveTenantID returns the effective tenant ID for a request.
//
// SYSTEM principals are trusted to supply any tenant ID (cross-tenant admin
// operations). All other principals are always scoped to the tenant in their
// token — the supplied value is ignored and a 403 is returned if the caller
// explicitly passes a different tenant ID.
func (c *Controller) resolveTenantID(w http.ResponseWriter, r *http.Request, claims *token.PlatformClaims, supplied string) (string, bool) {
	ctx := r.Context()
	if claims.Orbit.PrincipalType == token.PrincipalTypeSystem {
		return supplied, true
	}
	if supplied != "" && supplied != claims.Orbit.TenantID {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"tenant_id does not match your token", nil), nil)
		return "", false
	}
	return claims.Orbit.TenantID, true
}
