package policygw

import (
	"context"
	"net/http"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
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

// parseDelegation verifies the optional delegation token and refuses a revoked
// one. A blocklist lookup error is logged and the request continues, as the
// delegation middleware does. It writes the error response when it returns false.
func (c *Controller) parseDelegation(ctx context.Context, w http.ResponseWriter, raw string) (*policy.DelegationClaims, bool) {
	delegation, err := policy.ParseDelegationToken(ctx, raw, c.verifier)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "invalid delegation token", nil), err)
		return nil, false
	}
	if delegation == nil || delegation.JTI == "" || c.blocklist == nil {
		return delegation, true
	}
	blocked, err := c.blocklist.IsBlocked(ctx, delegation.JTI)
	if err != nil {
		c.logger.Error(ctx, "delegation JTI blocklist check failed, allowing request",
			logging.Field{Key: "jti", Value: delegation.JTI},
			logging.Field{Key: "error", Value: err.Error()})
		return delegation, true
	}
	if blocked {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "delegation has been revoked", nil), nil)
		return nil, false
	}
	return delegation, true
}
