package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/declarative"
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
	if c.refuseDelegated(ctx, w) {
		return
	}

	var req upsertRoleRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if apiErr := validateRole(req.Role, req.Scopes); apiErr != nil {
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

	if c.refuseRoleWrite(ctx, w, tenantID, req.Role) {
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
	if declarative.WriteRefusal(ctx, w, c.logger, err) {
		return
	}
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, roleErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, roleWriteResponse{RoleEntry: entry, Warnings: c.svc.LintScopes(ctx, req.Scopes)})
}

// validateRole checks a role name and its scopes as every role write does.
func validateRole(role string, scopes []string) *platformapi.APIError {
	if role == "" || len(scopes) == 0 {
		return platformapi.ValidationError(platformapi.ErrCodeMissingField, "role and scopes are required", nil)
	}
	if len(role) > 255 {
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField, "role name must not exceed 255 characters", nil)
	}
	return nil
}

// roleBudgetFromRequest validates a role budget body. nil clears the budget.
func roleBudgetFromRequest(req *budgetAssignmentRequest) (*roles.BudgetAssignment, *platformapi.APIError) {
	if req == nil {
		return nil, nil
	}
	budget, apiErr := validRoleBudget(&roles.BudgetAssignment{LimitUSD: req.LimitUSD, ResetCadence: req.ResetCadence, Pooled: req.Pooled})
	if apiErr != nil {
		return nil, apiErr
	}
	if req.PerRunLimitUSD != 0 {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget.per_run_limit_usd is not valid on roles — set it on the agent's grant", nil)
	}
	return budget, nil
}

// validRoleBudget checks a role budget and normalizes its cadence.
func validRoleBudget(b *roles.BudgetAssignment) (*roles.BudgetAssignment, *platformapi.APIError) {
	if b.LimitUSD <= 0 {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget.limit_usd must be greater than zero", nil)
	}
	cadence, ok := normalizeCadence(b.ResetCadence)
	if !ok {
		return nil, platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"budget.reset_cadence must be 'weekly' or 'monthly'", nil)
	}
	return &roles.BudgetAssignment{LimitUSD: b.LimitUSD, ResetCadence: cadence, Pooled: b.Pooled}, nil
}

// refuseRoleWrite writes 403 when the tenant takes its roles from resources
// only, and 409 when the role is projected from a resource.
func (c *Controller) refuseRoleWrite(ctx context.Context, w http.ResponseWriter, tenantID, role string) bool {
	if !c.registry.APIAuthoringEnabled(tenantID) {
		return declarative.WriteRefusal(ctx, w, c.logger, declarative.ErrAPIAuthoringDisabled)
	}
	declared, err := c.svc.isDeclaredRole(ctx, tenantID, role)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, roleErrorResponse(err), err)
		return true
	}
	return declared && declarative.WriteRefusal(ctx, w, c.logger, declarative.ErrManagedByResource)
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
	if c.refuseDelegated(ctx, w) {
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

	if c.refuseRoleWrite(ctx, w, tenantID, r.PathValue("role")) {
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

	if c.refuseRoleWrite(ctx, w, tenantID, r.PathValue("role")) {
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
