package policygw

import (
	"context"
	"net/http"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
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

	delegation, ok := c.parseDelegation(ctx, w, req.DelegationToken)
	if !ok {
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

	delegation, ok := c.parseDelegation(ctx, w, req.DelegationToken)
	if !ok {
		return
	}

	allowed, err := c.svc.Query(ctx, claims, delegation, req.ResourceType, req.Action, req.Candidates)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, evaluationErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, QueryResponse{Allowed: allowed})
}

// parseDelegation verifies the optional delegation token with the shared delegation check.
// It writes the error response when it returns false.
func (c *Controller) parseDelegation(ctx context.Context, w http.ResponseWriter, raw string) (*policy.DelegationClaims, bool) {
	delegation, err := mw.VerifyDelegation(ctx, c.logger, c.verifier, c.blocklist, raw)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, mw.DelegationRefusalMessage(err), nil), err)
		return nil, false
	}
	return delegation, true
}
