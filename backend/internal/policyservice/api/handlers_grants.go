package api

import (
	"net/http"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
)

func (c *Controller) handleCreateGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}
	if c.refuseDelegated(ctx, w) {
		return
	}

	var req createGrantRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.PrincipalType == "" || req.PrincipalID == "" || len(req.Scopes) == 0 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField,
				"principal_type, principal_id, and scopes are required", nil), nil)
		return
	}

	if len(req.PrincipalID) > 255 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeInvalidField,
				"principal_id must not exceed 255 characters", nil), nil)
		return
	}

	// Only a SYSTEM caller writes a non-HUMAN grant (2026-10-22 amendment to the
	// policy administration dashboard ADR).
	if c.refuseNonHumanGrantWrite(ctx, w, claims, grants.PrincipalType(req.PrincipalType)) {
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

	budget, apiErr := grantBudgetFromRequest(req.Budget)
	if apiErr != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	// Anti-escalation: a non-SYSTEM caller may only grant scopes it holds itself
	// (2026-06-30 amendment to the authorisation model).
	if !c.enforceScopeCeiling(ctx, w, claims, req.Scopes) {
		return
	}

	g, err := c.svc.CreateGrant(ctx, tenantID, grants.PrincipalType(req.PrincipalType), req.PrincipalID, req.Scopes, budget, req.DelegationRequired)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusCreated, grantWriteResponse{Grant: g, Warnings: c.svc.LintScopes(ctx, tenantID, req.Scopes)})
}

// grantBudgetFromRequest validates and maps a budget request body onto a grant
// budget assignment. A grant budget carries a per-window limit, a per-run
// limit, or both; pooling is a role-only concept.
func grantBudgetFromRequest(req *budgetAssignmentRequest) (*grants.BudgetAssignment, *platformapi.APIError) {
	if req == nil {
		return nil, nil
	}
	if req.Pooled {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget.pooled is not valid on grants — pools are configured on roles", nil)
	}
	if req.LimitUSD < 0 || req.PerRunLimitUSD < 0 {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget limits must not be negative", nil)
	}
	if req.LimitUSD == 0 && req.PerRunLimitUSD == 0 {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget requires limit_usd or per_run_limit_usd greater than zero", nil)
	}
	cadence, ok := normalizeCadence(req.ResetCadence)
	if !ok {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget.reset_cadence must be 'weekly' or 'monthly'", nil)
	}
	return &grants.BudgetAssignment{
		LimitUSD:       req.LimitUSD,
		ResetCadence:   cadence,
		PerRunLimitUSD: req.PerRunLimitUSD,
	}, nil
}

func (c *Controller) handleGetGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	g, err := c.svc.GetGrant(ctx, r.PathValue("id"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	// Non-SYSTEM principals can only see grants within their own tenant.
	if claims.Kora.PrincipalType != token.PrincipalTypeSystem && g.TenantID != claims.Kora.TenantID {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "grant not found", nil), nil)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, g)
}

func (c *Controller) handleUpdateGrantBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}
	if c.refuseDelegated(ctx, w) {
		return
	}

	var req updateGrantBudgetRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	budget, apiErr := grantBudgetFromRequest(req.Budget)
	if apiErr != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	// Fetch first to enforce tenant ownership before mutating.
	g, err := c.svc.GetGrant(ctx, r.PathValue("id"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	if claims.Kora.PrincipalType != token.PrincipalTypeSystem && g.TenantID != claims.Kora.TenantID {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "grant not found", nil), nil)
		return
	}

	// Checks the stored type, after the tenant 404, so a foreign grant still reads as not found.
	if c.refuseNonHumanGrantWrite(ctx, w, claims, g.PrincipalType) {
		return
	}

	updated, err := c.svc.UpdateGrantBudget(ctx, g.ID, g.TenantID, budget)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, updated)
}

func (c *Controller) handleDeleteGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}
	if c.refuseDelegated(ctx, w) {
		return
	}

	// Fetch first to enforce tenant ownership before deleting.
	g, err := c.svc.GetGrant(ctx, r.PathValue("id"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	if claims.Kora.PrincipalType != token.PrincipalTypeSystem && g.TenantID != claims.Kora.TenantID {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "grant not found", nil), nil)
		return
	}

	// Checks the stored type, after the tenant 404, so a foreign grant still reads as not found.
	if c.refuseNonHumanGrantWrite(ctx, w, claims, g.PrincipalType) {
		return
	}

	if err := c.svc.DeleteGrant(ctx, g.ID, g.TenantID); err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

func (c *Controller) handleListGrants(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	supplied := r.URL.Query().Get("tenant_id")
	if supplied == "" {
		supplied = claims.Kora.TenantID
	}
	tenantID, ok := c.resolveTenantID(w, r, claims, supplied)
	if !ok {
		return
	}

	q := r.URL.Query()
	limit, offset := parsePagination(r)
	result, total, err := c.svc.ListGrants(ctx, tenantID, grants.PrincipalType(q.Get("principal_type")), q.Get("principal_id"), limit, offset)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
		"grants": result,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
