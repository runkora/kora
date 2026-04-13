package api

import (
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policyservice/grants"
)

func (c *Controller) handleCreateGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
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

	tenantID, ok := c.resolveTenantID(w, r, claims, req.TenantID)
	if !ok {
		return
	}
	if tenantID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField, "tenant_id is required", nil), nil)
		return
	}

	g, err := c.svc.CreateGrant(ctx, tenantID, grants.PrincipalType(req.PrincipalType), req.PrincipalID, req.Scopes)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusCreated, g)
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
	if claims.Orbit.PrincipalType != token.PrincipalTypeSystem && g.TenantID != claims.Orbit.TenantID {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "grant not found", nil), nil)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, g)
}

func (c *Controller) handleDeleteGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	// Fetch first to enforce tenant ownership before deleting.
	g, err := c.svc.GetGrant(ctx, r.PathValue("id"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	if claims.Orbit.PrincipalType != token.PrincipalTypeSystem && g.TenantID != claims.Orbit.TenantID {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "grant not found", nil), nil)
		return
	}

	if err := c.svc.DeleteGrant(ctx, g.ID); err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (c *Controller) handleListGrants(w http.ResponseWriter, r *http.Request) {
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

	q := r.URL.Query()
	result, err := c.svc.ListGrants(ctx, tenantID, grants.PrincipalType(q.Get("principal_type")), q.Get("principal_id"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"grants": result})
}
