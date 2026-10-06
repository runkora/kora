package inferencegw

import (
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// agentInsightsRequest reads the tenant and period, writing the error and reporting
// false on failure. A token with no tenant has no usage to read.
func (c *Controller) agentInsightsRequest(w http.ResponseWriter, r *http.Request) (string, usagewindow.Period, bool) {
	ctx := r.Context()
	p, err := principalFromClaims(ctx)
	if err != nil || p.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), err)
		return "", "", false
	}
	period, err := usagewindow.Parse(r.URL.Query().Get("period"))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, err.Error(), nil), err)
		return "", "", false
	}
	return p.TenantID, period, true
}

// handleAgentInsights handles GET /v1/usage/agent-insights?period=24h|7d|30d|90d.
func (c *Controller) handleAgentInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, period, ok := c.agentInsightsRequest(w, r)
	if !ok {
		return
	}
	out, err := c.svc.Insights(ctx, tenantID, period, "", time.Now().UTC())
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get agent insights", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}

// handleAgentTypeInsights handles GET /v1/usage/agent-type/{slug}?period=, the
// insights of one agent.
func (c *Controller) handleAgentTypeInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, period, ok := c.agentInsightsRequest(w, r)
	if !ok {
		return
	}
	// Slugs are pod label values (spiffe.io/agent), so anything else cannot be an agent principal.
	slug := r.PathValue("slug")
	if slug == "" || len(validation.IsValidLabelValue(slug)) > 0 {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, "a valid agent slug is required", nil), nil)
		return
	}
	out, err := c.svc.Insights(ctx, tenantID, period, slug, time.Now().UTC())
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get agent insights", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}
