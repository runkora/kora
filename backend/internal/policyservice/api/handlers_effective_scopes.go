package api

import (
	"net/http"
	"strings"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
)

// handleEffectiveScopes handles GET /policy/effective-scopes.
// Returns the full set of scopes the authenticated HUMAN principal currently
// holds, derived from their OIDC roles and explicit grants. Used by the PAT
// creation and update UI to populate the scope selector.
func (c *Controller) handleEffectiveScopes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	if claims.Orbit.PrincipalType != token.PrincipalTypeHuman {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"effective scopes are only available for human principals", nil), nil)
		return
	}

	var roles []string
	if raw := claims.Orbit.Claims["roles"]; raw != "" {
		for role := range strings.SplitSeq(raw, ",") {
			if t := strings.TrimSpace(role); t != "" {
				roles = append(roles, t)
			}
		}
	}

	scopes, err := c.svc.EffectiveScopes(ctx, claims.Orbit.TenantID, string(claims.Orbit.PrincipalType), claims.Subject, roles)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError, "failed to resolve effective scopes", nil), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"scopes": scopes})
}
