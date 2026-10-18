package api

import (
	"net/http"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// handleEffectiveScopes handles GET /policy/effective-scopes.
// Returns the full set of scopes the authenticated principal currently holds,
// derived from its OIDC roles (HUMAN only) and explicit grants. The PAT UI
// reads it to populate the scope selector, and orbit-agents reads it with the
// caller's bearer to keep an agent's grant within its creator's scopes.
func (c *Controller) handleEffectiveScopes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	if claims.Kora.PrincipalType == token.PrincipalTypeSystem {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"effective scopes are not available for system principals", nil), nil)
		return
	}

	scopes, err := c.callerEffectiveScopes(ctx, claims)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError, "failed to resolve effective scopes", nil), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"scopes": scopes})
}
