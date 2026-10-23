package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/platform/usagewindow"
)

// Controller implements the kora-agents HTTP API.
type Controller struct {
	agentTypes   *AgentTypeService
	runs         *RunService
	environments *EnvironmentService
	drafts       *DraftService
	verifier     *token.Verifier
	blocklist    mw.JTIBlocklist
	eval         policy.Evaluator
	routes       []routescopes.RouteScope
	basePath     string
	logger       logging.Logger
}

// NewController creates a kora-agents HTTP controller.
//
// routes is the same RouteScope slice that is registered with policy-service
// at startup (see cmd/kora-agents/main.go agentRoutes). It is also used at
// request time by ScopeEnforcementMiddleware to enforce route → required-scope
// bindings in-process. basePath must match the server's BasePath so the
// middleware can reconstruct full paths after http.StripPrefix. See docs/adr/2026-06-04-platform-scope-enforcement-middleware.md.
// blocklist refuses a revoked delegation token before it expires.
func NewController(
	agentTypes *AgentTypeService,
	runs *RunService,
	environments *EnvironmentService,
	drafts *DraftService,
	verifier *token.Verifier,
	blocklist mw.JTIBlocklist,
	eval policy.Evaluator,
	routes []routescopes.RouteScope,
	basePath string,
	logger logging.Logger,
) *Controller {
	return &Controller{
		agentTypes:   agentTypes,
		runs:         runs,
		environments: environments,
		drafts:       drafts,
		verifier:     verifier,
		blocklist:    blocklist,
		eval:         eval,
		routes:       routes,
		basePath:     basePath,
		logger:       logger,
	}
}

func (c *Controller) GroupedBy() string { return "" }

// Middleware applies authentication to every route except the unauthenticated
// /health probe.
//
//   - Public /api/v1 routes get the full chain: JWT auth, delegation parsing,
//     and route→scope enforcement.
//   - Internal callback routes get JWT auth ONLY. The agent's platform token is
//     injected on every outbound request by the identity-proxy ext_proc sidecar
//     on the agent pod (see cmd/kora-agents k8s.go: "actual auth is the
//     identity-proxy-issued JWT"). ScopeEnforcementMiddleware is intentionally
//     omitted: the route→scope table (agentRoutes) only covers the public
//     surface, and agent principals hold no route scopes — running it would
//     deny every callback with "no route match". Per-run authorization (binding
//     the calling agent's tenant + slug to the target run) is enforced in each
//     internal handler via runs.AuthorizeAgentCallback. See docs/adr/2026-06-04-orbit-agents-authorization-model.md + docs/adr/2026-06-04-platform-scope-enforcement-middleware.md.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	publicChain := mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.DelegationMiddlewareWithBlocklist(c.logger, c.verifier, c.blocklist),
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)
	internalChain := mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
	)
	return func(next http.Handler) http.Handler {
		public := publicChain(next)
		internal := internalChain(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/health":
				next.ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, "/internal/"):
				internal.ServeHTTP(w, r)
			default:
				public.ServeHTTP(w, r)
			}
		})
	}
}

func (c *Controller) Boot(mux *api.Mux) {
	// Public API
	mux.Handle("GET /health", c.handleHealth)
	// Draft routes live on a SIBLING prefix, not under /agent-types: ServeMux
	// rejects "GET /api/v1/agent-types/drafts/{id}" as ambiguous against the
	// existing "GET /api/v1/agent-types/{id}/effective-access" (neither is more
	// specific — both match ".../drafts/effective-access"), and panics at boot.
	mux.Handle("POST /api/v1/agent-type-drafts", c.handleCreateDraft)
	mux.Handle("GET /api/v1/agent-type-drafts", c.handleListDrafts)
	mux.Handle("GET /api/v1/agent-type-drafts/{id}", c.handleGetDraft)
	mux.Handle("PATCH /api/v1/agent-type-drafts/{id}", c.handleUpdateDraft)
	mux.Handle("DELETE /api/v1/agent-type-drafts/{id}", c.handleDeleteDraft)
	mux.Handle("POST /api/v1/agent-types", c.handleCreateAgentType)
	mux.Handle("POST /api/v1/agent-types/effective-access", c.handleEffectiveAccessPreview)
	mux.Handle("GET /api/v1/agent-types", c.handleListAgentTypes)
	mux.Handle("GET /api/v1/agent-types/{id}", c.handleGetAgentType)
	mux.Handle("GET /api/v1/agent-types/{id}/effective-access", c.handleGetEffectiveAccess)
	mux.Handle("PATCH /api/v1/agent-types/{id}", c.handleUpdateAgentType)
	mux.Handle("DELETE /api/v1/agent-types/{id}", c.handleDeleteAgentType)
	mux.Handle("POST /api/v1/environments", c.handleCreateEnvironment)
	mux.Handle("GET /api/v1/environments", c.handleListEnvironments)
	mux.Handle("GET /api/v1/environments/{id}", c.handleGetEnvironment)
	mux.Handle("PUT /api/v1/environments/{id}", c.handleUpdateEnvironment)
	mux.Handle("DELETE /api/v1/environments/{id}", c.handleDeleteEnvironment)
	mux.Handle("GET /api/v1/sandbox-images", c.handleListSandboxImages)
	mux.Handle("GET /api/v1/environment-sizes", c.handleEnvironmentSizes)
	mux.Handle("POST /api/v1/runs", c.handleStartRun)
	mux.Handle("GET /api/v1/runs", c.handleListRuns)
	mux.Handle("GET /api/v1/runs/stats", c.handleTenantRunStats)
	mux.Handle("GET /api/v1/run-outcomes", c.handleRunOutcomes)
	mux.Handle("GET /api/v1/run-stats/{slug}", c.handleRunStats)
	mux.Handle("GET /api/v1/runs/{id}", c.handleGetRun)
	mux.Handle("DELETE /api/v1/runs/{id}", c.handleTerminateRun)
	mux.Handle("POST /api/v1/runs/{id}/messages", c.handleSendMessage)
	mux.Handle("POST /api/v1/runs/{id}/interrupt", c.handleInterruptRun)
	mux.Handle("GET /api/v1/runs/{id}/events", c.handleRunEvents)

	// Internal — session-manager callbacks (Istio AuthorizationPolicy restricts these)
	mux.Handle("POST /internal/runs/{id}/state", c.handleRunStateCallback)
	mux.Handle("POST /internal/runs/{id}/events", c.handlePushEvent)
	mux.Handle("POST /internal/runs/{id}/summary", c.handleSetRunSummary)
	mux.Handle("POST /internal/runs/{id}/provision-sandbox", c.handleProvisionSandbox)
	mux.Handle("GET /internal/runs/{id}/sandbox", c.handleGetRunSandbox)
	mux.Handle("GET /internal/runs/{id}/environments", c.handleListRunEnvironments)
}

// handleHealth returns liveness status.
func (c *Controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"status": "ok"})
}

// withCallerBearerFrom lifts r's bearer into ctx for calls made on the caller's behalf.
// AuthMiddleware has already verified the token; this carries the raw string.
func withCallerBearerFrom(ctx context.Context, r *http.Request) context.Context {
	return WithCallerBearer(ctx, strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
}

// ── Agent Types ───────────────────────────────────────────────────────────────

func (c *Controller) handleCreateAgentType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateAgentTypeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	// Tenant is derived from the verified platform claims by the service layer;
	// any request-body tenant_id is ignored. See docs/adr/2026-06-04-orbit-agents-authorization-model.md.
	// The model is required: a session runs only the agent type's own model.
	if req.Name == "" || req.Slug == "" || req.ModelID == nil || *req.ModelID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "name, slug, and model_id are required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	at, err := c.agentTypes.CreateAgentType(withCallerBearerFrom(ctx, r), req)
	if err != nil {
		if errors.Is(err, ErrSlugTaken) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "slug already taken for this tenant", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if invalidAgentTypeField(err) {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrIdentityModeWriteForbidden) || errors.Is(err, ErrScopeCeiling) {
			// 403 with the explicit reason: the message names the missing
			// scopes and, for a mode refusal, the identity model's stakes.
			apiErr := api.ForbiddenError(api.ErrCodeForbidden, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to create agent type", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, at)
}

// includeEffectiveAccess is the include value that adds each listed agent type's effective connectors.
const includeEffectiveAccess = "effective_access"

// splitIDs splits a comma-separated id list, dropping blanks.
func splitIDs(raw string) []string {
	var ids []string
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (c *Controller) handleListAgentTypes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Tenant is derived from the verified platform claims; query-string is ignored.
	// The bearer lets can_edit resolve the caller's scopes for the ceiling check.
	query := r.URL.Query()
	withAccess := false
	switch include := query.Get("include"); include {
	case "":
	case includeEffectiveAccess:
		withAccess = true
	default:
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "include must be effective_access", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}
	var (
		types []AgentType
		err   error
	)
	switch {
	case query.Has("ids"):
		types, err = c.agentTypes.ListAgentTypesByIDs(withCallerBearerFrom(ctx, r), splitIDs(query.Get("ids")), withAccess)
	case withAccess:
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "include=effective_access needs ids", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	default:
		types, err = c.agentTypes.ListAgentTypes(withCallerBearerFrom(ctx, r))
	}
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to list agent types", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"agent_types": types})
}

func (c *Controller) handleGetAgentType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	at, err := c.agentTypes.GetAgentType(withCallerBearerFrom(ctx, r), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to get agent type", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, at)
}

// handleGetEffectiveAccess returns the resolved access view (egress posture +
// environment + connector auto-egress, combined) for an agent type.
// AgentTypeService.GetEffectiveAccess runs the agent-type read check.
func (c *Controller) handleGetEffectiveAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	access, err := c.agentTypes.GetEffectiveAccess(ctx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to resolve effective access", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, access)
}

// handleEffectiveAccessPreview resolves the access view for a draft (unsaved)
// connectivity config — backs the live preview as the user edits.
// Authenticated-only via the /api/v1/agent-types POST binding (the binding's
// real scope check is the mode-qualified create in the service layer, which a
// preview never reaches); nothing is persisted, and the preview reveals only
// what the caller can already resolve for their own tenant.
func (c *Controller) handleEffectiveAccessPreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req EffectiveAccessPreviewRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	access, err := c.agentTypes.PreviewEffectiveAccess(ctx, req)
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to preview effective access", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, access)
}

func (c *Controller) handleUpdateAgentType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var req UpdateAgentTypeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	req.ID = id
	if req.ModelID != nil && *req.ModelID == "" {
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "model_id cannot be cleared", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	at, err := c.agentTypes.UpdateAgentType(withCallerBearerFrom(ctx, r), req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if invalidAgentTypeField(err) || errors.Is(err, ErrIdentityModeImmutable) {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrIdentityModeWriteForbidden) || errors.Is(err, ErrScopeCeiling) {
			apiErr := api.ForbiddenError(api.ErrCodeForbidden, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to update agent type", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, at)
}

func (c *Controller) handleDeleteAgentType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if err := c.agentTypes.DeleteAgentType(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found or not in active state", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrIdentityModeWriteForbidden) || errors.Is(err, ErrScopeCeiling) {
			apiErr := api.ForbiddenError(api.ErrCodeForbidden, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to delete agent type", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ── Runs ──────────────────────────────────────────────────────────────────────

func (c *Controller) handleStartRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req StartRunRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	// Tenant and created_by are derived from the verified platform claims;
	// request-body values for tenant_id are ignored.
	if req.AgentTypeID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "agent_type_id is required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	// A delegated agent's run start forwards the caller's bearer to POST
	// /identity/delegation (delegated-agent-identity ADR, Decision 3).
	ctx = withCallerBearerFrom(ctx, r)

	run, err := c.runs.StartRun(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		case errors.Is(err, ErrDelegatedRunPrincipal):
			apiErr := api.ForbiddenError(api.ErrCodeForbidden,
				"this agent uses the delegated identity model: runs can only be started by a person (HUMAN or PAT principal)", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		case errors.Is(err, ErrTriggerAttributionRefused):
			apiErr := api.ForbiddenError(api.ErrCodeForbidden,
				"only a service principal may record a run as started by a trigger", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		case errors.Is(err, ErrTriggerAttributionIncomplete):
			apiErr := api.ValidationError(api.ErrCodeMissingField,
				"trigger_id is required when attributing a run to a trigger", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		case errors.Is(err, ErrDelegationUnavailable):
			apiErr := api.InternalError(api.ErrCodeInternalError,
				"this agent uses the delegated identity model and no delegation could be minted; the run was not started", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		case errors.Is(err, ErrForbidden):
			// A refused start is the caller's answer, not a server fault. It
			// also has to arrive as 403 for the caller to tell it apart from a
			// broken service: kora-triggers retries a refusal while a young
			// grant propagates, and cannot do so if the refusal reads as 500.
			apiErr := api.ForbiddenError(api.ErrCodeForbidden,
				"not permitted to start runs for this agent", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		default:
			apiErr := api.InternalError(api.ErrCodeInternalError, "failed to start run", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		}
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, run)
}

// handleListRuns handles GET /api/v1/runs?agent_type_id=&state=&started_by=&created_after=&created_before=&limit=&cursor=, clamping limit to maxRunsLimit.
// agent_type_id and state may repeat; started_by=trigger keeps runs a trigger started.
func (c *Controller) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	// Tenant is derived from the verified platform claims; query-string is ignored.
	p := RunListQuery{Limit: defaultRunsLimit}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, "limit must be a positive integer", map[string]any{"field": "limit"})
			api.ErrorResponse(ctx, w, c.logger, apiErr, err)
			return
		}
		p.Limit = min(n, maxRunsLimit)
	}
	var err error
	if p.After, err = decodeRunCursor(q.Get("cursor")); err != nil {
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "cursor is not a valid pagination cursor", map[string]any{"field": "cursor"})
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	for _, b := range []struct {
		field string
		dst   *time.Time
	}{{"created_after", &p.CreatedAfter}, {"created_before", &p.CreatedBefore}} {
		raw := q.Get(b.field)
		if raw == "" {
			continue
		}
		t, perr := time.Parse(time.RFC3339, raw)
		if perr != nil {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, b.field+" must be an RFC 3339 timestamp", map[string]any{"field": b.field})
			api.ErrorResponse(ctx, w, c.logger, apiErr, perr)
			return
		}
		*b.dst = t
	}
	// Repeated state keys add to the list.
	if p.States, err = parseRunStates(strings.Join(q["state"], ",")); err != nil {
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "state must be a comma-separated list of run states", map[string]any{"field": "state"})
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	switch q.Get("started_by") {
	case "":
	case startedByTrigger:
		p.StartedByTrigger = true
	default:
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "started_by must be trigger", map[string]any{"field": "started_by"})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}
	agentTypeIDs := slices.DeleteFunc(slices.Clone(q["agent_type_id"]), func(id string) bool { return id == "" })

	page, err := c.runs.ListRuns(ctx, agentTypeIDs, p)
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to list runs", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	runs := page.Runs
	if runs == nil {
		runs = []Run{}
	}
	var next *string
	if page.NextCursor != "" {
		next = &page.NextCursor
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"runs": runs, "next_cursor": next, "limit": p.Limit})
}

// parsePeriod reads the period query, writing the 400 and reporting false when it is invalid.
func (c *Controller) parsePeriod(w http.ResponseWriter, r *http.Request) (usagewindow.Period, bool) {
	period, err := usagewindow.Parse(r.URL.Query().Get("period"))
	if err != nil {
		apiErr := api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil)
		api.ErrorResponse(r.Context(), w, c.logger, apiErr, err)
		return "", false
	}
	return period, true
}

// handleRunOutcomes handles GET /api/v1/run-outcomes?period=24h|7d|30d|90d.
func (c *Controller) handleRunOutcomes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period, ok := c.parsePeriod(w, r)
	if !ok {
		return
	}
	window := usagewindow.For(period, time.Now())
	out, err := c.runs.RunOutcomes(ctx, window.Start, window.End)
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to count run outcomes", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}

// handleRunStats handles GET /api/v1/run-stats/{slug}?period=24h|7d|30d|90d.
func (c *Controller) handleRunStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period, ok := c.parsePeriod(w, r)
	if !ok {
		return
	}
	out, err := c.runs.AgentRunStats(ctx, r.PathValue("slug"), period, time.Now().UTC())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to get run stats", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}

// handleTenantRunStats handles GET /api/v1/runs/stats?period=24h|7d|30d|90d: the
// run stats of every agent type whose runs the caller may read.
func (c *Controller) handleTenantRunStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period, ok := c.parsePeriod(w, r)
	if !ok {
		return
	}
	out, err := c.runs.TenantRunStats(ctx, period, time.Now().UTC())
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to get run stats", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}

func (c *Controller) handleGetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	run, err := c.runs.GetRunWithUsage(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to get run", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, run)
}

func (c *Controller) handleTerminateRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if err := c.runs.TerminateRun(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to terminate run", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

func (c *Controller) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var req SendMessageRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.Content == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "content is required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	if err := c.runs.SendMessage(ctx, id, req); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrTriggerAttributionRefused) {
			apiErr := api.ForbiddenError(api.ErrCodeForbidden,
				"only a service principal may send a message as a trigger", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrTriggerAttributionIncomplete) {
			apiErr := api.ValidationError(api.ErrCodeMissingField,
				"trigger_id is required when attributing a message to a trigger", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrDelegatedRunSteer) {
			// 403 with the explicit reason, not 404: visibility already admitted
			// the caller (a tenant admin), so existence is not being protected.
			apiErr := api.ForbiddenError(api.ErrCodeForbidden, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		// The reason detail tells a caller that can wait for a starting run
		// apart from one that should give up on a closed run.
		if errors.Is(err, ErrRunStarting) {
			apiErr := api.ConflictError(api.ErrCodeConflict, "run is starting", map[string]any{"reason": "run_starting"})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrRunNotAcceptingMessages) {
			apiErr := api.ConflictError(api.ErrCodeConflict, "run is not accepting messages", map[string]any{"reason": "run_closed"})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to send message", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.NoBody(w, http.StatusAccepted)
}

func (c *Controller) handleInterruptRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if err := c.runs.InterruptRun(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrDelegatedRunSteer) {
			apiErr := api.ForbiddenError(api.ErrCodeForbidden, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrRunNotAcceptingMessages) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "run is not active", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to interrupt run", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.NoBody(w, http.StatusAccepted)
}

// ── Internal callbacks ────────────────────────────────────────────────────────

type runStateCallbackRequest struct {
	From RunState `json:"from"`
	To   RunState `json:"to"`
}

// authorizeCallback binds the agent principal carried by the (already
// JWT-authenticated) internal request to the target run before the handler
// proceeds. It writes the appropriate error response and returns false on deny.
// See RunService.AuthorizeAgentCallback and docs/adr/2026-06-04-orbit-agents-authorization-model.md.
func (c *Controller) authorizeCallback(ctx context.Context, w http.ResponseWriter, id string) bool {
	switch err := c.runs.AuthorizeAgentCallback(ctx, id); {
	case err == nil:
		return true
	case errors.Is(err, ErrNotFound):
		apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
	case errors.Is(err, ErrForbidden):
		apiErr := api.ForbiddenError(api.ErrCodeForbidden, "caller is not authorized for this run", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
	default:
		apiErr := api.InternalError(api.ErrCodeInternalError, "callback authorization failed", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
	}
	return false
}

func (c *Controller) handleRunStateCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if !c.authorizeCallback(ctx, w, id) {
		return
	}

	var req runStateCallbackRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if err := c.runs.TransitionRunState(ctx, id, req.From, req.To); err != nil {
		if errors.Is(err, ErrConflict) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "state transition conflict", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to transition run state", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// handleStreamEvents serves a Server-Sent Events stream for a run.
//
// On connect: subscribes to the in-memory PubSub first (to avoid a gap),
// run-event read bounds: a long run accumulates hundreds of events, so the JSON
// read is always paginated and capped.
const (
	defaultRunEventLimit = 50
	maxRunEventLimit     = 200
)

// runEventDTO is the JSON shape of a recorded run event (RunEvent has no json tags
// because the SSE path formats events by hand).
type runEventDTO struct {
	SequenceNo int64           `json:"sequence_no"`
	EventType  string          `json:"event_type"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}

// handleRunEvents serves a run's events as Server-Sent Events (the default, for the
// live UI stream) or, when the caller asks for application/json, as a single
// paginated JSON page. The JSON form backs non-streaming readers such as the kora
// MCP runs.events tool; the SSE behavior is unchanged.
func (c *Controller) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		c.handleListEvents(w, r)
		return
	}
	c.handleStreamEvents(w, r)
}

// handleListEvents returns one capped, cursor-paginated page of a run's recorded
// events as JSON: events after `after_seq` (default 0), at most `limit` (default 50,
// max 200). The response carries `next_seq` (advance the cursor) and `has_more` so a
// caller can tail a long conversation without ever pulling the whole transcript.
func (c *Controller) handleListEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	// Existence + tenant scoping (GetRun returns ErrNotFound on tenant mismatch).
	if _, err := c.runs.GetRun(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get run", nil), err)
		return
	}

	afterSeq := queryInt(r, "after_seq", 0)
	limit := queryInt(r, "limit", defaultRunEventLimit)
	if limit <= 0 {
		limit = defaultRunEventLimit
	}
	if limit > maxRunEventLimit {
		limit = maxRunEventLimit
	}

	events, err := c.runs.ListRunEvents(ctx, id, int64(afterSeq))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to list run events", nil), err)
		return
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	out := make([]runEventDTO, len(events))
	nextSeq := int64(afterSeq)
	for i, ev := range events {
		out[i] = runEventDTO{SequenceNo: ev.SequenceNo, EventType: ev.EventType, Payload: ev.Payload, CreatedAt: ev.CreatedAt}
		nextSeq = ev.SequenceNo
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
		"events":   out,
		"next_seq": nextSeq,
		"has_more": hasMore,
	})
}

// queryInt reads an integer query parameter, falling back to def when absent or
// malformed.
func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// replays persisted events from run_events (honoring Last-Event-ID), then
// streams live pushed events until the client disconnects.
func (c *Controller) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if _, err := c.runs.GetRun(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to get run", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		apiErr := api.InternalError(api.ErrCodeInternalError, "streaming not supported", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	var afterSeqNo int64
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
		if n, err := strconv.ParseInt(lastID, 10, 64); err == nil {
			afterSeqNo = n
		}
	}

	// Register subscriber BEFORE replaying from DB — events pushed during replay
	// are buffered in the channel and will be emitted after the replay loop.
	ch, cancel := c.runs.SubscribeRun(id)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Replay persisted events from DB.
	events, err := c.runs.ListRunEvents(ctx, id, afterSeqNo)
	if err != nil {
		c.logger.Error(ctx, "Failed to list run events for SSE replay",
			logging.Field{Key: "run_id", Value: id},
			logging.Field{Key: "error", Value: err.Error()},
		)
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to list run events", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	var lastReplayedSeqNo int64
	for _, ev := range events {
		writeSSE(w, ev.SequenceNo, ev.EventType, ev.CreatedAt, ev.Payload)
		if ev.SequenceNo > lastReplayedSeqNo {
			lastReplayedSeqNo = ev.SequenceNo
		}
	}
	flusher.Flush()

	// Stream live events from the push channel.
	// Durable events (SequenceNo > 0) that arrived in the channel while the DB
	// replay was in progress are skipped if their sequence number was already
	// sent, preventing double delivery.
	for {
		select {
		case ev, open := <-ch:
			if !open {
				return
			}
			if ev.SequenceNo > 0 && ev.SequenceNo <= lastReplayedSeqNo {
				continue
			}
			writeSSE(w, ev.SequenceNo, ev.Type, ev.At, ev.Payload)
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// writeSSE writes one event. data is an envelope: "at" is when kora-agents
// recorded the event (or received it, for streaming-only types) and "payload"
// is the event as stored, so clients never stamp events with their own clock.
func writeSSE(w io.Writer, seqNo int64, eventType string, at time.Time, payload json.RawMessage) {
	if len(bytes.TrimSpace(payload)) == 0 {
		payload = json.RawMessage("{}")
	}
	if seqNo > 0 {
		fmt.Fprintf(w, "id: %d\n", seqNo)
	}
	fmt.Fprintf(w, "event: %s\ndata: {\"at\":%q,\"payload\":%s}\n\n", eventType, at.UTC().Format(time.RFC3339Nano), payload)
}

// handlePushEvent receives an event pushed by a session-manager pod, records the
// durable ones, and fans out to any live SSE subscribers for that run.
func (c *Controller) handlePushEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if !c.authorizeCallback(ctx, w, id) {
		return
	}

	var ev PushedEvent
	if !api.DecodeJSON(ctx, r, w, c.logger, &ev) {
		return
	}

	if err := c.runs.PublishEvent(ctx, id, ev); err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to record run event", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// setRunSummaryRequest is the body for POST /internal/runs/{id}/summary.
type setRunSummaryRequest struct {
	Summary string `json:"summary"`
}

// handleSetRunSummary receives the LLM-generated task summary from a
// session-manager pod, which generates it from the run's initial message over
// the agent's own authorized inference path. The write is set-once and
// best-effort.
func (c *Controller) handleSetRunSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if !c.authorizeCallback(ctx, w, id) {
		return
	}

	var req setRunSummaryRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if err := c.runs.SetRunSummary(ctx, id, req.Summary); err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to set run summary", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// handleGetRunSandbox reports the run's sandbox without creating one, for a
// session-manager whose own memory has none. 404 means the run has no sandbox,
// which is the answer that legitimately routes pre-sandbox work locally.
func (c *Controller) handleGetRunSandbox(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if !c.authorizeCallback(ctx, w, id) {
		return
	}

	sandbox, err := c.runs.GetSandbox(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNoSandbox) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "no sandbox for this run", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to look up the sandbox", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	// exec_token goes only to the session-manager that owns the run, which
	// authorizeCallback has already bound to this run.
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
		"endpoint":   sandbox.Endpoint,
		"exec_token": sandbox.ExecToken,
	})
}

func (c *Controller) handleProvisionSandbox(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if !c.authorizeCallback(ctx, w, id) {
		return
	}

	// The body names the environment. An older session-manager sends none.
	var req sandboxEnvironmentRequest
	if err := api.DecodeBody(r, &req); err != nil && !errors.Is(err, io.EOF) {
		api.ErrorResponse(ctx, w, c.logger, api.ClassifyDecodeError(err), err)
		return
	}
	sandbox, err := c.runs.ProvisionSandbox(ctx, id, req.Environment, req.Replace)
	if err != nil {
		if c.writeEnvironmentChoiceError(ctx, w, err) {
			return
		}
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if ue, ok := errors.AsType[*unschedulableError](err); ok {
			// No node can take the pod yet. The session-manager retries and passes the scheduler's reason on.
			api.ErrorResponse(ctx, w, c.logger, api.ServiceUnavailableError(sessionmanager.ErrCodeSandboxUnschedulable, ue.reason, nil), nil)
			return
		}
		if errors.Is(err, errSandboxImagePending) {
			// The operator has not read the image yet. The session-manager retries.
			api.ErrorResponse(ctx, w, c.logger, api.ServiceUnavailableError(api.ErrCodeServiceUnavailable, err.Error(), nil), nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to provision sandbox", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	// exec_token goes only to the session-manager that owns the run: the route is
	// restricted to the agent pod by AuthorizationPolicy, and authorizeCallback
	// has already bound the caller to this run.
	api.JSON(ctx, w, c.logger, http.StatusOK, sandbox)
}

// writeEnvironmentChoiceError answers a refused environment choice, or a sandbox
// too small for its work, with a 409 whose message the agent reads. It reports
// whether err was one.
func (c *Controller) writeEnvironmentChoiceError(ctx context.Context, w http.ResponseWriter, err error) bool {
	for _, target := range []error{ErrEnvironmentNotAttached, ErrEnvironmentSwitch, ErrEnvironmentRequired, ErrSandboxImageUnavailable, ErrSandboxOutOfMemory} {
		if errors.Is(err, target) {
			api.ErrorResponse(ctx, w, c.logger, api.ConflictError(api.ErrCodeConflict, err.Error(), nil), nil)
			return true
		}
	}
	return false
}

// handleListRunEnvironments lists the environments the run's agent may use.
func (c *Controller) handleListRunEnvironments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if !c.authorizeCallback(ctx, w, id) {
		return
	}
	envs, err := c.runs.ListRunEnvironments(ctx, id)
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to list environments", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"environments": envs})
}

// sandboxEnvironmentRequest names the environment of a sandbox. Replace
// switches a live sandbox to it.
type sandboxEnvironmentRequest struct {
	Environment string `json:"environment"`
	Replace     bool   `json:"replace"`
}

// invalidAgentTypeField reports an agent-type write the request itself got wrong. The handler answers it with a 400.
func invalidAgentTypeField(err error) bool {
	for _, target := range []error{ErrInvalidConnectivity, ErrInvalidConnector, ErrInvalidIdentityMode, ErrInvalidGuardrails, ErrInvalidEnvironment, ErrInvalidSlug} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
