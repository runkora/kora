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

	filter.TokenID = parseTokenID(q.Get("token_id"))

	if v, apiErr := parseProviderParam(q.Get("provider")); apiErr != nil {
		return InferenceUsageFilter{}, apiErr, nil
	} else {
		filter.Provider = v
	}

	if v, apiErr := parseDialectParam(q.Get("api_dialect")); apiErr != nil {
		return InferenceUsageFilter{}, apiErr, nil
	} else {
		filter.APIDialect = v
	}

	if v, apiErr := parseStatusParam(q.Get("status")); apiErr != nil {
		return InferenceUsageFilter{}, apiErr, nil
	} else {
		filter.Status = v
	}

	if v, apiErr, err := parseTimeParam(q.Get("from"), "'from'"); apiErr != nil {
		return InferenceUsageFilter{}, apiErr, err
	} else {
		filter.From = v
	}

	if v, apiErr, err := parseTimeParam(q.Get("to"), "'to'"); apiErr != nil {
		return InferenceUsageFilter{}, apiErr, err
	} else {
		filter.To = v
	}

	if v, apiErr, err := parsePosIntParam(q.Get("limit"), "'limit'", 50); apiErr != nil {
		return InferenceUsageFilter{}, apiErr, err
	} else {
		filter.Limit = v
	}

	if v, apiErr, err := parseNonNegIntParam(q.Get("offset"), "'offset'", 0); apiErr != nil {
		return InferenceUsageFilter{}, apiErr, err
	} else {
		filter.Offset = v
	}

	return filter, nil, nil
}

// parseTokenID returns a pointer to raw, or nil when raw is empty.
func parseTokenID(raw string) *string {
	if raw == "" {
		return nil
	}
	v := raw
	return &v
}

// parseProviderParam validates the provider query parameter.
func parseProviderParam(raw string) (*ProviderName, *api.APIError) {
	if raw == "" {
		return nil, nil
	}
	v := ProviderName(raw)
	switch v {
	case ProviderBedrock, ProviderVertexAI, ProviderAzure:
		return &v, nil
	default:
		return nil, api.BadRequestError("invalid_param", "'provider' must be one of: bedrock, vertexai, azure", nil)
	}
}

// parseDialectParam validates the api_dialect query parameter.
func parseDialectParam(raw string) (*APIDialect, *api.APIError) {
	if raw == "" {
		return nil, nil
	}
	v := APIDialect(raw)
	switch v {
	case DialectOpenAI, DialectAnthropic:
		return &v, nil
	default:
		return nil, api.BadRequestError("invalid_param", "'api_dialect' must be one of: openai, anthropic", nil)
	}
}

// parseStatusParam validates the status query parameter.
func parseStatusParam(raw string) (*UsageStatus, *api.APIError) {
	if raw == "" {
		return nil, nil
	}
	v := UsageStatus(raw)
	switch v {
	case UsageStatusSuccess, UsageStatusProviderError, UsageStatusPolicyDenied,
		UsageStatusAuthError, UsageStatusTimeout, UsageStatusRoutingFailed:
		return &v, nil
	default:
		return nil, api.BadRequestError("invalid_param", "'status' must be one of: success, provider_error, policy_denied, auth_error, timeout, routing_failed", nil)
	}
}

// parseTimeParam parses an RFC3339 timestamp. paramLabel is used in error messages.
func parseTimeParam(raw, paramLabel string) (*time.Time, *api.APIError, error) {
	if raw == "" {
		return nil, nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, api.BadRequestError("invalid_param", paramLabel+" must be a valid RFC3339 timestamp", nil), err
	}
	return &t, nil, nil
}

// parsePosIntParam parses a strictly positive integer. defaultVal is returned when raw is empty.
func parsePosIntParam(raw, paramLabel string, defaultVal int) (int, *api.APIError, error) {
	if raw == "" {
		return defaultVal, nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, api.BadRequestError("invalid_param", paramLabel+" must be a positive integer", nil), err
	}
	return v, nil, nil
}

// parseNonNegIntParam parses a non-negative integer. defaultVal is returned when raw is empty.
func parseNonNegIntParam(raw, paramLabel string, defaultVal int) (int, *api.APIError, error) {
	if raw == "" {
		return defaultVal, nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, api.BadRequestError("invalid_param", paramLabel+" must be a non-negative integer", nil), err
	}
	return v, nil, nil
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
