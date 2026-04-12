package inferencegw

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
)

// ─── GET /v1/usage ───────────────────────────────────────────────────────────

// listUsageResponse is the envelope returned by GET /v1/usage.
// Total reflects the full result count before pagination, enabling clients
// to render page controls without a separate count round-trip.
type listUsageResponse struct {
	Data   []InferenceUsageRecord `json:"data"`
	Total  int                    `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

// parseUsageFilter extracts and validates all recognized query parameters from
// q, returning a populated InferenceUsageFilter with defaults applied.
//
// On any validation failure it returns a non-nil *api.APIError (always 400) and
// the underlying parse error for logging; both are nil on success.
//
// Query parameters:
//
//	token_id    — filter to a specific PAT (UUID string)
//	provider    — filter by provider name  (bedrock | vertexai | azure)
//	api_dialect — filter by API dialect    (openai | anthropic)
//	status      — filter by status         (success | provider_error | policy_denied | auth_error | timeout | routing_failed)
//	from        — lower bound on created_at (RFC3339)
//	to          — upper bound on created_at (RFC3339)
//	limit       — page size, default 50
//	offset      — zero-based page offset, default 0
func parseUsageFilter(q url.Values) (InferenceUsageFilter, *api.APIError, error) {
	filter := InferenceUsageFilter{Limit: 50}

	if raw := q.Get("token_id"); raw != "" {
		v := raw
		filter.TokenID = &v
	}
	if raw := q.Get("provider"); raw != "" {
		v := ProviderName(raw)
		filter.Provider = &v
	}
	if raw := q.Get("api_dialect"); raw != "" {
		v := APIDialect(raw)
		filter.APIDialect = &v
	}
	if raw := q.Get("status"); raw != "" {
		v := UsageStatus(raw)
		filter.Status = &v
	}
	if raw := q.Get("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return InferenceUsageFilter{}, api.BadRequestError("invalid_param", "'from' must be a valid RFC3339 timestamp", nil), err
		}
		filter.From = &t
	}
	if raw := q.Get("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return InferenceUsageFilter{}, api.BadRequestError("invalid_param", "'to' must be a valid RFC3339 timestamp", nil), err
		}
		filter.To = &t
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return InferenceUsageFilter{}, api.BadRequestError("invalid_param", "'limit' must be a positive integer", nil), err
		}
		filter.Limit = v
	}
	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return InferenceUsageFilter{}, api.BadRequestError("invalid_param", "'offset' must be a non-negative integer", nil), err
		}
		filter.Offset = v
	}

	return filter, nil, nil
}

// handleListUsage handles GET /v1/usage.
//
// TenantID and PrincipalID are always sourced from the verified JWT claims;
// they cannot be overridden by query parameters.
func (c *Controller) handleListUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter, apiErr, parseErr := parseUsageFilter(r.URL.Query())
	if apiErr != nil {
		api.ErrorResponse(ctx, w, c.logger, apiErr, parseErr)
		return
	}

	records, total, err := c.svc.ListUsage(ctx, filter)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	// Return an empty slice rather than null when there are no records.
	if records == nil {
		records = []InferenceUsageRecord{}
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, listUsageResponse{
		Data:   records,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}
