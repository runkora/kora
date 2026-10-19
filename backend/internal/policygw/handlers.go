package policygw

import (
	"net/http"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/policy"
)

func (c *Controller) handleCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CheckRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	claims, err := c.verifier.Verify(ctx, req.Token)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "invalid token", nil), err)
		return
	}

	delegation, err := policy.ParseDelegationToken(ctx, req.DelegationToken, c.verifier)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "invalid delegation token", nil), err)
		return
	}

	result, err := c.svc.Check(ctx, claims, delegation,
		req.Resource.Type, req.Resource.PathSegments, req.Action, req.Resource.Attributes)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, evaluationErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, CheckResponse{Allow: result.Allow, Reason: result.Reason})
}

func (c *Controller) handleQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req QueryRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	claims, err := c.verifier.Verify(ctx, req.Token)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "invalid token", nil), err)
		return
	}

	delegation, err := policy.ParseDelegationToken(ctx, req.DelegationToken, c.verifier)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "invalid delegation token", nil), err)
		return
	}

	allowed, err := c.svc.Query(ctx, claims, delegation, req.ResourceType, req.Action, req.Candidates)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, evaluationErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, QueryResponse{Allowed: allowed})
}
