package api

import (
	"errors"
	"net/http"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/policyservice/tenantbudget"
)

// handleUpsertTenantBudget handles PUT /policy/tenant-budget.
// The tenant cap is the rollup spending ceiling for the tenant: all
// principals' spend accumulates against it; AGENT/SERVICE principals are
// blocked at the limit (humans are not).
func (c *Controller) handleUpsertTenantBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	var req tenantBudgetRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.LimitUSD <= 0 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeInvalidField,
				"limit_usd must be greater than zero", nil), nil)
		return
	}
	cadence, okCadence := normalizeCadence(req.ResetCadence)
	if !okCadence {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeInvalidField,
				"reset_cadence must be 'weekly' or 'monthly'", nil), nil)
		return
	}

	tenantID, ok := c.resolveTenantID(w, r, claims, req.TenantID)
	if !ok {
		return
	}
	if tenantID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField, "tenant_id is required", nil), nil)
		return
	}

	tb, err := c.svc.UpsertTenantBudget(ctx, tenantID, req.LimitUSD, cadence)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeDatabaseError, "tenant budget operation failed", nil), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, tb)
}

// handleGetTenantBudget handles GET /policy/tenant-budget.
// Returns 404 when no cap is configured for the tenant.
func (c *Controller) handleGetTenantBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	tenantID, ok := c.resolveTenantID(w, r, claims, r.URL.Query().Get("tenant_id"))
	if !ok {
		return
	}

	tb, err := c.svc.GetTenantBudget(ctx, tenantID)
	if errors.Is(err, tenantbudget.ErrTenantBudgetNotFound) {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "no tenant budget configured", nil), nil)
		return
	}
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeDatabaseError, "tenant budget operation failed", nil), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, tb)
}

// handleDeleteTenantBudget handles DELETE /policy/tenant-budget.
func (c *Controller) handleDeleteTenantBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	tenantID, ok := c.resolveTenantID(w, r, claims, r.URL.Query().Get("tenant_id"))
	if !ok {
		return
	}

	if err := c.svc.DeleteTenantBudget(ctx, tenantID); err != nil {
		if errors.Is(err, tenantbudget.ErrTenantBudgetNotFound) {
			platformapi.ErrorResponse(ctx, w, c.logger,
				platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "no tenant budget configured", nil), nil)
			return
		}
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeDatabaseError, "tenant budget operation failed", nil), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}
