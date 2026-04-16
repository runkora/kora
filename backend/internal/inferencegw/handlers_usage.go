package inferencegw

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
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

// Allowed values for each enum query parameter.
var (
	providerNames = []ProviderName{ProviderBedrock, ProviderVertexAI, ProviderAzure}
	apiDialects   = []APIDialect{DialectOpenAI, DialectAnthropic}
	usageStatuses = []UsageStatus{
		UsageStatusSuccess, UsageStatusProviderError, UsageStatusPolicyDenied,
		UsageStatusAuthError, UsageStatusTimeout, UsageStatusRoutingFailed,
	}
)

// usageParamBinder extracts a single query parameter from q and writes its
// value into f. It returns a non-nil *api.APIError on validation failure
// (always 400) and the underlying parse error for logging.
type usageParamBinder func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error)

// usageParamBinders is the declarative list of all query parameters accepted by
// GET /v1/usage. To add a new filter, append one entry here — no other code
// changes are required.
var usageParamBinders = []usageParamBinder{
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		if raw := q.Get("token_id"); raw != "" {
			v := raw
			f.TokenID = &v
		}
		return nil, nil
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		return parseEnumParam(q.Get("provider"), "'provider'", providerNames, &f.Provider), nil
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		return parseEnumParam(q.Get("api_dialect"), "'api_dialect'", apiDialects, &f.APIDialect), nil
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		return parseEnumParam(q.Get("status"), "'status'", usageStatuses, &f.Status), nil
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		v, apiErr, err := parseTimeParam(q.Get("from"), "'from'")
		f.From = v
		return apiErr, err
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		v, apiErr, err := parseTimeParam(q.Get("to"), "'to'")
		f.To = v
		return apiErr, err
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		v, apiErr, err := parsePosIntParam(q.Get("limit"), "'limit'", 50)
		f.Limit = v
		return apiErr, err
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		v, apiErr, err := parseNonNegIntParam(q.Get("offset"), "'offset'", 0)
		f.Offset = v
		return apiErr, err
	},
}

// parseUsageFilter extracts and validates all recognized query parameters from q,
// returning a populated InferenceUsageFilter with defaults applied.
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
	f := InferenceUsageFilter{}
	for _, bind := range usageParamBinders {
		if apiErr, err := bind(q, &f); apiErr != nil {
			return InferenceUsageFilter{}, apiErr, err
		}
	}
	return f, nil, nil
}

// parseEnumParam validates an enum query parameter against a set of allowed
// values. It writes the result into dest and returns a 400 APIError if the
// value is unrecognized.
func parseEnumParam[T ~string](raw, paramLabel string, allowed []T, dest **T) *api.APIError {
	if raw == "" {
		return nil
	}
	v := T(raw)
	if slices.Contains(allowed, v) {
		*dest = &v
		return nil
	}
	return api.BadRequestError("invalid_param", paramLabel+" must be one of: "+joinEnumValues(allowed), nil)
}

// joinEnumValues returns a comma-separated string of the allowed enum values
// for use in human-readable error messages.
func joinEnumValues[T ~string](values []T) string {
	strs := make([]string, len(values))
	for i, v := range values {
		strs[i] = string(v)
	}
	return strings.Join(strs, ", ")
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
