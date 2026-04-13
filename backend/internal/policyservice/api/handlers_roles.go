package api

import (
	"errors"
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/policyservice/roles"
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

	tenantID, ok := c.resolveTenantID(w, r, claims, req.TenantID)
	if !ok {
		return
	}
	if tenantID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField, "tenant_id is required", nil), nil)
		return
	}

	entry, err := c.svc.UpsertRole(ctx, tenantID, req.Role, req.Scopes)
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

	tenantID, ok := c.resolveTenantID(w, r, claims, r.URL.Query().Get("tenant_id"))
	if !ok {
		return
	}
	if tenantID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField, "tenant_id query parameter is required", nil), nil)
		return
	}

	entries, err := c.svc.ListRoles(ctx, tenantID)
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

	w.WriteHeader(http.StatusNoContent)
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
