package inferencegw

import (
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// WithAgentInsights installs the service behind the agent usage routes. They
// answer 503 until it is set.
func (c *Controller) WithAgentInsights(s *AgentInsightsService) *Controller {
	c.agentInsights = s
	return c
}

// agentUsageRequest reads what the agent usage routes share: the verified
// caller, the period and the window end. It writes the error response and
// reports false when the request cannot be served. A token with no tenant, such
// as a service token, is refused: it has no usage of its own to read.
func (c *Controller) agentUsageRequest(w http.ResponseWriter, r *http.Request) (usageCaller, usagewindow.Period, time.Time, bool) {
	ctx := r.Context()
	claims, found := token.ClaimsFromContext(ctx)
	if !found || claims == nil || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return usageCaller{}, "", time.Time{}, false
	}
	if c.agentInsights == nil {
		api.ErrorResponse(ctx, w, c.logger, api.ServiceUnavailableError("agent_insights_unavailable", "agent usage is not available", nil), nil)
		return usageCaller{}, "", time.Time{}, false
	}
	period, err := usagewindow.Parse(r.URL.Query().Get("period"))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", err.Error(), nil), err)
		return usageCaller{}, "", time.Time{}, false
	}
	end := time.Now()
	if raw := r.URL.Query().Get("end"); raw != "" {
		if end, err = time.Parse(time.RFC3339, raw); err != nil {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "'end' must be a valid RFC3339 timestamp", nil), err)
			return usageCaller{}, "", time.Time{}, false
		}
	}
	return callerFromClaims(claims), period, end, true
}

// handleAgentInsights handles GET /v1/usage/agent-insights?period=24h|7d|30d|90d
// and an optional end timestamp. It reports usage, spend by model, spend over
// time, the previous window's total and budgets, for agent traffic the caller may see.
func (c *Controller) handleAgentInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, period, end, ok := c.agentUsageRequest(w, r)
	if !ok {
		return
	}
	out, err := c.agentInsights.Insights(ctx, caller, period, end)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get agent insights", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}

// handleAgentUsage handles GET /v1/usage/agent-type/{slug}?period=. It is the
// one-agent form of handleAgentInsights, under the same predicate.
func (c *Controller) handleAgentUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, period, end, ok := c.agentUsageRequest(w, r)
	if !ok {
		return
	}
	// Slugs are pod label values (spiffe.io/agent), so anything else cannot be an agent principal.
	slug := r.PathValue("slug")
	if slug == "" || len(validation.IsValidLabelValue(slug)) > 0 {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "a valid agent slug is required", nil), nil)
		return
	}
	out, err := c.agentInsights.AgentUsage(ctx, caller, period, end, slug)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get agent usage", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}
