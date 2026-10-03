package inferencegw

import (
	"errors"
	"net/http"
	"strings"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// WithAgentInsights installs the service behind the agent rollup routes. The
// routes answer 503 until it is set, because they cannot say which agents a
// caller may see without it.
func (c *Controller) WithAgentInsights(s *AgentInsightsService) *Controller {
	c.agentInsights = s
	return c
}

// callerForAgentInsights returns the verified tenant, subject and bearer the
// rollups need, or writes the error response and reports false.
func (c *Controller) callerForAgentInsights(w http.ResponseWriter, r *http.Request) (tenantID, subject, bearer string, ok bool) {
	ctx := r.Context()
	claims, found := token.ClaimsFromContext(ctx)
	if !found || claims == nil || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return "", "", "", false
	}
	if c.agentInsights == nil {
		api.ErrorResponse(ctx, w, c.logger, api.ServiceUnavailableError("agent_insights_unavailable", "agent usage is not available", nil), nil)
		return "", "", "", false
	}
	bearer = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return claims.Orbit.TenantID, claims.Subject, bearer, true
}

// agentInsightsError maps a rollup failure to a response.
func (c *Controller) agentInsightsError(w http.ResponseWriter, r *http.Request, err error, what string) {
	ctx := r.Context()
	if errors.Is(err, ErrAgentVisibilityUnavailable) {
		api.ErrorResponse(ctx, w, c.logger, api.ServiceUnavailableError("agent_visibility_unavailable", "could not determine which agents you can see", nil), err)
		return
	}
	api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to "+what, nil), err)
}

// handleAgentInsights handles GET /v1/usage/agent-insights?period=24h|7d|30d|90d.
// It reports usage, spend by model, spend over time and the previous window's
// total for the agents the caller may see. Agent traffic only: rows without an
// agent slug are never counted.
func (c *Controller) handleAgentInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, subject, bearer, ok := c.callerForAgentInsights(w, r)
	if !ok {
		return
	}
	period, err := usagewindow.Parse(r.URL.Query().Get("period"))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", err.Error(), nil), err)
		return
	}
	out, err := c.agentInsights.Insights(ctx, tenantID, subject, bearer, period)
	if err != nil {
		c.agentInsightsError(w, r, err, "get agent insights")
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}

// handleAgentBudgets handles GET /v1/usage/agent-budgets: each visible agent's
// spend in the current week and month, for the budget bars.
func (c *Controller) handleAgentBudgets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, subject, bearer, ok := c.callerForAgentInsights(w, r)
	if !ok {
		return
	}
	out, err := c.agentInsights.Budgets(ctx, tenantID, subject, bearer)
	if err != nil {
		c.agentInsightsError(w, r, err, "get agent budgets")
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}
