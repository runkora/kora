package inferencegw

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ─── GET /v1/usage ───────────────────────────────────────────────────────────

const (
	defaultUsageLimit = 20
	maxUsageLimit     = 200
)

// listUsageResponse is the envelope returned by GET /v1/usage.
// HasNextPage indicates whether more rows follow the current page.
// NextCursor is an opaque token the client passes as ?cursor= on the next
// request; it is nil when HasNextPage is false.
type listUsageResponse struct {
	Data        []InferenceUsageRecord `json:"data"`
	HasNextPage bool                   `json:"has_next_page"`
	NextCursor  *string                `json:"next_cursor"`
	Limit       int                    `json:"limit"`
}

// Allowed values for each enum query parameter.
var (
	apiDialects = []APIDialect{DialectOpenAI, DialectAnthropic, DialectResponses}
	// usageStatuses is derived from the declared constants rather than
	// re-listed: every status the gateway can write is a status a caller may
	// filter on, so the two can never drift apart.
	usageStatuses = AllUsageStatuses()
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
		if raw := q.Get("model_id"); raw != "" {
			v := raw
			f.ModelID = &v
		}
		return nil, nil
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		if raw := q.Get("model_family"); raw != "" {
			v := raw
			f.ModelFamily = &v
		}
		return nil, nil
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		if raw := q.Get("provider"); raw != "" {
			p := ProviderName(raw)
			if !isValidProvider(p) {
				return api.BadRequestError("invalid_param", "'provider' must be a known provider (bedrock, bedrock:<alias>, vertexai, azure)", nil), nil
			}
			f.Provider = &p
		}
		return nil, nil
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
		v, apiErr, err := parsePosIntParam(q.Get("limit"), "'limit'", defaultUsageLimit)
		if apiErr != nil {
			return apiErr, err
		}
		if v > maxUsageLimit {
			return api.BadRequestError("invalid_param",
				fmt.Sprintf("'limit' must be between 1 and %d", maxUsageLimit), nil), nil
		}
		f.Limit = v
		return nil, nil
	},
	func(q url.Values, f *InferenceUsageFilter) (*api.APIError, error) {
		return parseCursorParam(q.Get("cursor"), f)
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
//	token_id     — filter to a specific PAT (UUID string)
//	model_id     — filter by model ID (e.g. "claude-sonnet-4-5-20250929-v1")
//	model_family — filter by model family (e.g. "claude")
//	provider     — filter by provider name  (bedrock | vertexai | azure)
//	api_dialect  — filter by API dialect    (openai | anthropic)
//	status       — filter by status         (any value in AllUsageStatuses)
//	from         — lower bound on created_at (RFC3339)
//	to           — upper bound on created_at (RFC3339)
//	limit        — page size, default 20
//	cursor       — opaque pagination cursor returned by the previous response
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

// parseCursorParam decodes an opaque cursor string of the form
// "<RFC3339Nano>|<id>" and writes CursorTime and CursorID into f.
// Returns a 400 APIError when the cursor is present but malformed.
func parseCursorParam(raw string, f *InferenceUsageFilter) (*api.APIError, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.SplitN(raw, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		err := fmt.Errorf("cursor %q: expected <timestamp>|<id>", raw)
		return api.BadRequestError("invalid_param", "'cursor' is not a valid pagination cursor", nil), err
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return api.BadRequestError("invalid_param", "'cursor' is not a valid pagination cursor", nil), err
	}
	id := parts[1]
	f.CursorTime = &t
	f.CursorID = &id
	return nil, nil
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

// handleGetPersonalBudget handles GET /v1/usage/budget.
// Returns the caller's current budget status: spend limit, current spend, and whether the limit is reached.
func (c *Controller) handleGetPersonalBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	status, err := c.svc.GetPersonalBudget(ctx, claims.Orbit.TenantID, claims.Subject)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get budget status", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, status)
}

// handleGetRunUsage handles GET /v1/usage/run/{runID}.
// Returns the spend and token total recorded against a single run, scoped to
// the caller's verified tenant — powers the run chat usage meter. Any
// authenticated caller in the tenant may read it (the run chat view is already
// tenant-scoped); the query filters by the verified tenant, so a caller can
// never read another tenant's run usage.
func (c *Controller) handleGetRunUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	runID := r.PathValue("runID")
	if runID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "run id is required", nil), nil)
		return
	}

	usage, err := c.svc.GetRunUsage(ctx, claims.Orbit.TenantID, runID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get run usage", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, usage)
}

// handleGetAgentTypeUsage handles GET /v1/usage/agent-type/{agentTypeID}.
// Returns the token breakdown and spend recorded against the agent type's
// SPIFFE principal over the last 7 days, scoped to the caller's verified
// tenant. Any authenticated tenant member may read it (the agent overview
// page is already tenant-scoped); the query filters by tenant_id so no
// cross-tenant leakage is possible.
func (c *Controller) handleGetAgentTypeUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	agentTypeID := r.PathValue("agentTypeID")
	if agentTypeID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "agent type id is required", nil), nil)
		return
	}

	usage, err := c.svc.GetAgentTypeUsage(ctx, claims.Orbit.TenantID, agentTypeID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get agent type usage", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, usage)
}

// handleListAgentTypeUsage handles GET /v1/usage/agent-types. Returns the
// 7-day token breakdown and spend per agent type for the caller's verified
// tenant, powering the total + per-agent cost on the agents list page. Scoped
// to the caller's tenant; the query filters by tenant_id so no cross-tenant
// leakage is possible.
func (c *Controller) handleListAgentTypeUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	usages, err := c.svc.ListAgentTypeUsage(ctx, claims.Orbit.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to list agent type usage", nil), err)
		return
	}
	if usages == nil {
		usages = []AgentTypeUsage{}
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": usages})
}

// handleUsageStats handles GET /v1/usage/stats.
// Returns per-PAT aggregates (request_count, last_used) for the authenticated principal.
func (c *Controller) handleUsageStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := c.svc.UsageStats(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	if stats == nil {
		stats = []TokenUsageStat{}
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": stats})
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

	records, hasNextPage, err := c.svc.ListUsage(ctx, filter)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	// Return an empty slice rather than null when there are no records.
	if records == nil {
		records = []InferenceUsageRecord{}
	}

	var nextCursor *string
	if hasNextPage && len(records) > 0 {
		last := records[len(records)-1]
		s := last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID
		nextCursor = &s
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, listUsageResponse{
		Data:        records,
		HasNextPage: hasNextPage,
		NextCursor:  nextCursor,
		Limit:       filter.Limit,
	})
}
