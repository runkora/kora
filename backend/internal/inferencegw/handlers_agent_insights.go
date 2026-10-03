package inferencegw

import (
	"net/http"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// agentUsageRequest reads the verified caller and the period. It writes the
// error response and reports false when the request cannot be served. A token
// with no tenant, such as a service token, has no usage to read.
func (c *Controller) agentUsageRequest(w http.ResponseWriter, r *http.Request) (usageCaller, usagewindow.Period, bool) {
	ctx := r.Context()
	claims, found := token.ClaimsFromContext(ctx)
	if !found || claims == nil || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return usageCaller{}, "", false
	}
	period, err := usagewindow.Parse(r.URL.Query().Get("period"))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", err.Error(), nil), err)
		return usageCaller{}, "", false
	}
	return callerFromClaims(claims), period, true
}

// handleAgentInsights handles GET /v1/usage/agent-insights?period=24h|7d|30d|90d.
func (c *Controller) handleAgentInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, period, ok := c.agentUsageRequest(w, r)
	if !ok {
		return
	}
	out, err := c.insights.Insights(ctx, caller, period)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get agent insights", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}

// handleAgentUsage handles GET /v1/usage/agent-type/{slug}?period=, the
// one-agent form of handleAgentInsights under the same predicate.
func (c *Controller) handleAgentUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, period, ok := c.agentUsageRequest(w, r)
	if !ok {
		return
	}
	// Slugs are pod label values (spiffe.io/agent), so anything else cannot be an agent principal.
	slug := r.PathValue("slug")
	if slug == "" || len(validation.IsValidLabelValue(slug)) > 0 {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "a valid agent slug is required", nil), nil)
		return
	}
	out, err := c.insights.AgentUsage(ctx, caller, period, slug)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get agent usage", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}
