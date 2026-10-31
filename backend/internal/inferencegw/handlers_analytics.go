package inferencegw

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/observability"
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
		observability.AnalyticsScopeKey.String("personal"),
		observability.AnalyticsDaysKey.Int(filter.Days),
		observability.AnalyticsGranularityKey.String(filter.Granularity),
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
		observability.AnalyticsScopeKey.String("tenant"),
		observability.AnalyticsDaysKey.Int(filter.Days),
		observability.AnalyticsGranularityKey.String(filter.Granularity),
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
// latency) for the tenant, sorted by cost descending by default. Paginated via
// page/limit. Sort column and direction are controlled by sort_by and order.
// Requires the platform:analytics:read scope.
func (c *Controller) handleAnalyticsPrincipals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := tracer.Start(ctx, "handleAnalyticsPrincipals")
	defer span.End()

	filter, apiErr, parseErr := parseAnalyticsPrincipalsFilter(r.URL.Query())
	if apiErr != nil {
		api.ErrorResponse(ctx, w, c.logger, apiErr, parseErr)
		return
	}

	span.SetAttributes(
		observability.AnalyticsPrincipalsDaysKey.Int(filter.Days),
		observability.AnalyticsPrincipalsPageKey.Int(filter.Page),
	)

	resp, err := c.svc.AnalyticsPrincipals(ctx, filter)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// ─── GET /v1/analytics/principals/{id} ───────────────────────────────────────

// handleAnalyticsPrincipalByID handles GET /v1/analytics/principals/{id}.
//
// Returns the same response shape as GET /v1/analytics/me (time-series
// buckets, KPIs, cost-by-model) for the principal identified by {id}, scoped
// to the caller's tenant. Backs the admin drill-down from a Principals table
// row into that principal's full usage view. Requires the
// platform:analytics:read scope — the same gate already covering
// /v1/analytics and /v1/analytics/principals; no new scope or route-scope
// entry is needed since the prefix match on "/v1/analytics" already covers
// this path.
func (c *Controller) handleAnalyticsPrincipalByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := tracer.Start(ctx, "handleAnalyticsPrincipalByID")
	defer span.End()

	principalID := r.PathValue("id")
	if principalID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "principal id is required", nil), nil)
		return
	}

	filter, apiErr, parseErr := parseAnalyticsFilter(r.URL.Query())
	if apiErr != nil {
		api.ErrorResponse(ctx, w, c.logger, apiErr, parseErr)
		return
	}

	span.SetAttributes(
		observability.AnalyticsScopeKey.String("principal_by_id"),
		observability.AnalyticsDaysKey.Int(filter.Days),
		observability.AnalyticsGranularityKey.String(filter.Granularity),
	)

	resp, err := c.svc.AnalyticsPrincipalByID(ctx, principalID, filter)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// ─── shared parameter parsing ─────────────────────────────────────────────────

var validDays = []int{7, 30, 90}
var validSortOrders = []string{"asc", "desc"}
var validGranularities = []string{"daily", "weekly", "monthly"}

// parseAnalyticsPrincipalsFilter extracts and validates the query parameters
// for GET /v1/analytics/principals.
func parseAnalyticsPrincipalsFilter(q url.Values) (AnalyticsPrincipalsFilter, *api.APIError, error) {
	f := AnalyticsPrincipalsFilter{Days: 30, Page: 1, Limit: 20}

	if raw := q.Get("days"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || !slices.Contains(validDays, v) {
			return AnalyticsPrincipalsFilter{}, api.BadRequestError("invalid_param", "'days' must be one of: 7, 30, 90", nil), err
		}
		f.Days = v
	}

	if raw := q.Get("page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return AnalyticsPrincipalsFilter{}, api.BadRequestError("invalid_param", "'page' must be a positive integer", nil), err
		}
		f.Page = v
	}

	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > maxPrincipalsLimit {
			return AnalyticsPrincipalsFilter{}, api.BadRequestError("invalid_param", fmt.Sprintf("'limit' must be between 1 and %d", maxPrincipalsLimit), nil), err
		}
		f.Limit = v
	}

	if raw := q.Get("sort_by"); raw != "" {
		if _, ok := principalSortColumns[raw]; !ok {
			return AnalyticsPrincipalsFilter{}, api.BadRequestError("invalid_param", "'sort_by' must be one of: cost, tokens, prompt_tokens, completion_tokens, requests, error_rate, avg_latency, last_active", nil), nil
		}
		f.SortBy = raw
	}

	if raw := q.Get("order"); raw != "" {
		if !slices.Contains(validSortOrders, raw) {
			return AnalyticsPrincipalsFilter{}, api.BadRequestError("invalid_param", "'order' must be 'asc' or 'desc'", nil), nil
		}
		f.SortOrder = raw
	}

	return f, nil, nil
}

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

	// token_id narrows results to a specific PAT — valid on both the personal and tenant-wide paths.
	if raw := q.Get("token_id"); raw != "" {
		f.TokenID = &raw
	}

	return f, nil, nil
}
