package api

import (
	"errors"
	"net/http"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
)

func (c *Controller) handleUpsertRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	var req upsertRoleRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.Role == "" || len(req.Scopes) == 0 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField,
				"role and scopes are required", nil), nil)
		return
	}

	if len(req.Role) > 255 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeInvalidField,
				"role name must not exceed 255 characters", nil), nil)
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

	var budget *roles.BudgetAssignment
	if req.Budget != nil {
		if req.Budget.LimitUSD <= 0 {
			platformapi.ErrorResponse(ctx, w, c.logger,
				platformapi.ValidationError(platformapi.ErrCodeInvalidField,
					"budget.limit_usd must be greater than zero", nil), nil)
			return
		}
		if req.Budget.PerRunLimitUSD != 0 {
			platformapi.ErrorResponse(ctx, w, c.logger,
				platformapi.ValidationError(platformapi.ErrCodeInvalidField,
					"budget.per_run_limit_usd is not valid on roles — set it on the agent's grant", nil), nil)
			return
		}
		cadence, okCadence := normalizeCadence(req.Budget.ResetCadence)
		if !okCadence {
			platformapi.ErrorResponse(ctx, w, c.logger,
				platformapi.ValidationError(platformapi.ErrCodeInvalidField,
					"budget.reset_cadence must be 'weekly' or 'monthly'", nil), nil)
			return
		}
		budget = &roles.BudgetAssignment{
			LimitUSD:     req.Budget.LimitUSD,
			ResetCadence: cadence,
			Pooled:       req.Budget.Pooled,
		}
	}

	// Anti-escalation: a role is an indirect grant, so a non-SYSTEM caller may only
	// bundle scopes it holds itself (2026-06-30 amendment to the authorisation model).
	if !c.enforceScopeCeiling(ctx, w, claims, req.Scopes) {
		return
	}

	entry, err := c.svc.UpsertRole(ctx, tenantID, req.Role, req.Scopes, budget)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, roleErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, entry)
}

func (c *Controller) handleGetRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	tenantID, ok := c.resolveTenantID(w, r, claims, r.URL.Query().Get("tenant_id"))
	if !ok {
		return
	}
	if tenantID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField, "tenant_id query parameter is required", nil), nil)
		return
	}

	entry, err := c.svc.GetRole(ctx, tenantID, r.PathValue("role"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, roleErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, entry)
}

func (c *Controller) handleListRoles(w http.ResponseWriter, r *http.Request) {
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

	limit, offset := parsePagination(r)
	entries, err := c.svc.ListRoles(ctx, tenantID, limit, offset)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, roleErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"roles": entries})
}

func (c *Controller) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}
	if c.refuseDelegated(ctx, w) {
		return
	}

	tenantID, ok := c.resolveTenantID(w, r, claims, r.URL.Query().Get("tenant_id"))
	if !ok {
		return
	}
	if tenantID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField, "tenant_id query parameter is required", nil), nil)
		return
	}

	if err := c.svc.DeleteRole(ctx, tenantID, r.PathValue("role")); err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, roleErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

func roleErrorResponse(err error) *platformapi.APIError {
	var scopeErr *ScopeVocabularyError
	switch {
	case isScopeVocabularyError(err, &scopeErr):
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"scope '"+scopeErr.Scope+"' is not in the scope vocabulary",
			map[string]any{"scope": scopeErr.Scope})
	case isReservedRoleError(err):
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			err.Error(), nil)
	case isRoleNotFound(err):
		return platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "role not found", nil)
	default:
		return platformapi.InternalError(platformapi.ErrCodeDatabaseError, "role operation failed", nil)
	}
}

func isReservedRoleError(err error) bool {
	var target *ReservedRoleError
	return errors.As(err, &target)
}

func isRoleNotFound(err error) bool {
	return errors.Is(err, roles.ErrRoleNotFound)
}
