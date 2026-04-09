package api

import (
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/policyservice/grants"
)

func (c *Controller) handleCreateGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req createGrantRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.TenantID == "" || req.PrincipalType == "" || req.PrincipalID == "" || len(req.Scopes) == 0 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField,
				"tenant_id, principal_type, principal_id, and scopes are required", nil), nil)
		return
	}

	g, err := c.svc.CreateGrant(ctx, req.TenantID, grants.PrincipalType(req.PrincipalType), req.PrincipalID, req.Scopes)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusCreated, g)
}

func (c *Controller) handleGetGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	g, err := c.svc.GetGrant(ctx, r.PathValue("id"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, g)
}

func (c *Controller) handleDeleteGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := c.svc.DeleteGrant(ctx, r.PathValue("id")); err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (c *Controller) handleListGrants(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	tenantID := q.Get("tenant_id")

	if tenantID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField, "tenant_id query parameter is required", nil), nil)
		return
	}

	result, err := c.svc.ListGrants(ctx, tenantID, grants.PrincipalType(q.Get("principal_type")), q.Get("principal_id"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, grantErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"grants": result})
}
