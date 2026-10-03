package inferencegw

import (
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
)

// ─── GET /v1/budgets/principals ──────────────────────────────────────────────

// handleListBudgetPrincipals handles GET /v1/budgets/principals.
//
// Returns per-principal budget status for the tenant — budget categories,
// effective limit, current period spend, and utilization — joined from the
// compiled policy bundle, the Redis attribution counters, and the usage log.
// Requires the platform:budgets:read scope, enforced by the route registration
// in inferenceRoutes(). TenantID is always sourced from the verified JWT.
func (c *Controller) handleListBudgetPrincipals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := tracer.Start(ctx, "handleListBudgetPrincipals")
	defer span.End()

	p, err := principalFromClaims(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(ErrTokenParseFailed), err)
		return
	}

	resp, err := c.svc.ListBudgetPrincipals(ctx, p.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleTenantCap handles GET /v1/budgets/tenant-cap. It returns the tenant
// rollup cap alone, for callers that need the cap and not the per-principal
// report GET /v1/budgets/principals builds.
func (c *Controller) handleTenantCap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	p, err := principalFromClaims(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(ErrTokenParseFailed), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, TenantCapResponse{TenantCap: c.svc.TenantCap(ctx, p.TenantID)})
}
