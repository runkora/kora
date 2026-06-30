package api

import (
	"context"
	"net/http"
	"strings"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/policy"
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

// enforceScopeCeiling implements the grant-administration anti-escalation invariant
// (2026-06-30 amendment to the authorisation model): a non-SYSTEM caller may assign,
// in a grant or role, only scopes that are covered by its own effective scopes.
// SYSTEM principals (e.g. orbit-agents provisioning agent grants) are exempt.
// Returns true if the request may proceed; otherwise it has written the error
// response (403 naming the rejected scopes, or 500 if the caller's scopes can't be
// resolved) and the caller must return.
func (c *Controller) enforceScopeCeiling(ctx context.Context, w http.ResponseWriter, claims *token.PlatformClaims, scopes []string) bool {
	if claims.Orbit.PrincipalType == token.PrincipalTypeSystem {
		return true
	}

	var roles []string
	if raw := claims.Orbit.Claims["roles"]; raw != "" {
		for role := range strings.SplitSeq(raw, ",") {
			if t := strings.TrimSpace(role); t != "" {
				roles = append(roles, t)
			}
		}
	}

	callerScopes, err := c.svc.EffectiveScopes(ctx, claims.Orbit.TenantID, claims.Subject, roles)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError, "failed to resolve caller scopes", nil), err)
		return false
	}

	if rejected := uncoveredScopes(callerScopes, scopes); len(rejected) > 0 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"cannot grant scopes you do not hold: "+strings.Join(rejected, ", "), nil), nil)
		return false
	}
	return true
}

// uncoveredScopes returns the scopes in `requested` that are not covered by any
// scope in `held` (per the Orbit scope hierarchy). Empty result means every
// requested scope is within the holder's authority.
func uncoveredScopes(held, requested []string) []string {
	var out []string
	for _, s := range requested {
		if !policy.AnyScopeCovers(held, s) {
			out = append(out, s)
		}
	}
	return out
}
