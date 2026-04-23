package inferencegw

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/clear-route/orbit/internal/platform/api"
	"go.opentelemetry.io/otel/attribute"
)

// ─── GET /v1/analytics/me ────────────────────────────────────────────────────

// handleAnalyticsMe handles GET /v1/analytics/me.
//
// Returns aggregated analytics (time-series buckets, KPIs, cost-by-model) for
// the authenticated principal. TenantID and PrincipalID are always sourced from
// the verified JWT — they cannot be supplied or overridden via query parameters.
func (c *Controller) handleAnalyticsMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := tracer.Start(ctx, "handleAnalyticsMe")
	defer span.End()

	filter, apiErr, parseErr := parseAnalyticsFilter(r.URL.Query())
	if apiErr != nil {
		api.ErrorResponse(ctx, w, c.logger, apiErr, parseErr)
		return
	}

	span.SetAttributes(
		attribute.String("app.analytics.scope", "personal"),
		attribute.Int("app.analytics.days", filter.Days),
		attribute.String("app.analytics.granularity", filter.Granularity),
	)

	resp, err := c.svc.AnalyticsMe(ctx, filter)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// ─── GET /v1/analytics ───────────────────────────────────────────────────────

// handleAnalytics handles GET /v1/analytics.
//
// Returns tenant-wide aggregated analytics. Requires the platform:analytics:read
// scope, enforced by the route registration in inferenceRoutes().
func (c *Controller) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := tracer.Start(ctx, "handleAnalytics")
	defer span.End()

	filter, apiErr, parseErr := parseAnalyticsFilter(r.URL.Query())
	if apiErr != nil {
		api.ErrorResponse(ctx, w, c.logger, apiErr, parseErr)
		return
	}

	// model_id filter is only supported on the tenant-wide path.
	if raw := r.URL.Query().Get("model_id"); raw != "" {
		filter.VirtualModelID = &raw
	}

	span.SetAttributes(
		attribute.String("app.analytics.scope", "tenant"),
		attribute.Int("app.analytics.days", filter.Days),
		attribute.String("app.analytics.granularity", filter.Granularity),
	)

	resp, err := c.svc.Analytics(ctx, filter)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// ─── GET /v1/analytics/principals ────────────────────────────────────────────

// handleAnalyticsPrincipals handles GET /v1/analytics/principals.
//
// Returns per-principal usage aggregates (cost, tokens, requests, error rate,
// latency) for the tenant, sorted by cost descending. Paginated via page/limit.
// Requires the platform:analytics:read scope.
func (c *Controller) handleAnalyticsPrincipals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := tracer.Start(ctx, "handleAnalyticsPrincipals")
	defer span.End()

	q := r.URL.Query()
	filter := AnalyticsPrincipalsFilter{Days: 30, Page: 1, Limit: 20}

	if raw := q.Get("days"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || !slices.Contains(validDays, v) {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "'days' must be one of: 7, 30, 90", nil), err)
			return
		}
		filter.Days = v
	}

	if raw := q.Get("page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "'page' must be a positive integer", nil), err)
			return
		}
		filter.Page = v
	}

	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "'limit' must be a positive integer", nil), err)
			return
		}
		filter.Limit = v
	}

	span.SetAttributes(
		attribute.Int("app.analytics.principals.days", filter.Days),
		attribute.Int("app.analytics.principals.page", filter.Page),
	)

	resp, err := c.svc.AnalyticsPrincipals(ctx, filter)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// ─── shared parameter parsing ─────────────────────────────────────────────────

var validDays = []int{7, 30, 90}
var validGranularities = []string{"daily", "weekly", "monthly"}

// parseAnalyticsFilter extracts and validates the common query parameters
// shared by both analytics endpoints.
func parseAnalyticsFilter(q url.Values) (AnalyticsFilter, *api.APIError, error) {
	f := AnalyticsFilter{
		Days:        30,
		Granularity: "daily",
	}

	if raw := q.Get("days"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || !slices.Contains(validDays, v) {
			return AnalyticsFilter{}, api.BadRequestError("invalid_param", "'days' must be one of: 7, 30, 90", nil), err
		}
		f.Days = v
	}

	if raw := q.Get("granularity"); raw != "" {
		if !slices.Contains(validGranularities, raw) {
			return AnalyticsFilter{}, api.BadRequestError("invalid_param", "'granularity' must be one of: daily, weekly, monthly", nil), nil
		}
		f.Granularity = raw
	}

	// token_id is honored on the personal path; ignored on the tenant-wide path.
	if raw := q.Get("token_id"); raw != "" {
		f.TokenID = &raw
	}

	return f, nil, nil
}
