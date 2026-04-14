package policygw

import (
	"net/http"
	"time"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
)

func (c *Controller) handleScopes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "missing claims", nil), nil)
		return
	}

	scopes, err := c.svc.Scopes(ctx, claims)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, evaluationErrorResponse(err), err)
		return
	}

	if scopes == nil {
		scopes = []string{}
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, ScopesResponse{
		Scopes:      scopes,
		TenantID:    claims.Orbit.TenantID,
		EvaluatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}
