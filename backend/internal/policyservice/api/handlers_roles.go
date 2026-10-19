package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
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

	budget, apiErr := roleBudgetFromRequest(req.Budget)
	if apiErr != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}
	if !c.roleBudgetChangeAllowed(ctx, w, claims, tenantID, req.Role, budget) {
		return
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

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, roleWriteResponse{RoleEntry: entry, Warnings: c.svc.LintScopes(ctx, req.Scopes)})
}

// roleBudgetFromRequest validates a role budget body. nil clears the budget.
func roleBudgetFromRequest(req *budgetAssignmentRequest) (*roles.BudgetAssignment, *platformapi.APIError) {
	if req == nil {
		return nil, nil
	}
	if req.LimitUSD <= 0 {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget.limit_usd must be greater than zero", nil)
	}
	if req.PerRunLimitUSD != 0 {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget.per_run_limit_usd is not valid on roles — set it on the agent's grant", nil)
	}
	cadence, ok := normalizeCadence(req.ResetCadence)
	if !ok {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget.reset_cadence must be 'weekly' or 'monthly'", nil)
	}
	return &roles.BudgetAssignment{LimitUSD: req.LimitUSD, ResetCadence: cadence, Pooled: req.Pooled}, nil
}

// roleBudgetChangeAllowed lets a role write change the role's budget only when
// the caller holds platform:budgets:write. A write that keeps the stored budget
// needs only platform:roles:write. It writes the error response otherwise.
func (c *Controller) roleBudgetChangeAllowed(ctx context.Context, w http.ResponseWriter, claims *token.PlatformClaims, tenantID, role string, budget *roles.BudgetAssignment) bool {
	if claims.Kora.PrincipalType == token.PrincipalTypeSystem {
		return true
	}
	var stored *roles.BudgetAssignment
	existing, err := c.svc.GetRole(ctx, tenantID, role)
	switch {
	case err == nil:
		stored = existing.Budget
	case !errors.Is(err, roles.ErrRoleNotFound):
		platformapi.ErrorResponse(ctx, w, c.logger, roleErrorResponse(err), err)
		return false
	}
	if reflect.DeepEqual(stored, budget) {
		return true
	}
	result, err := c.eval.Check(ctx, policy.CheckInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "platform",
		PathSegments: []string{"budgets"},
		Action:       "write",
	})
	if err != nil || !result.Allow {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"changing a role's budget requires platform:budgets:write", nil), err)
		return false
	}
	return true
}

// handleUpdateRoleBudget handles PATCH /policy/roles/{role}/budget: it sets or
// clears a role's budget and leaves its scopes alone. The route requires
// platform:budgets:write.
func (c *Controller) handleUpdateRoleBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}
	if policy.DelegationFromContext(ctx) != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"a delegated run cannot change budgets", nil), nil)
		return
	}

	var req updateRoleBudgetRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	budget, apiErr := roleBudgetFromRequest(req.Budget)
	if apiErr != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, apiErr, nil)
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

	entry, err := c.svc.UpdateRoleBudget(ctx, tenantID, r.PathValue("role"), budget)
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
