package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Controller implements the orbit-agents HTTP API.
type Controller struct {
	agentTypes   *AgentTypeService
	runs         *RunService
	environments *EnvironmentService
	drafts       *DraftService
	verifier     *token.Verifier
	eval         policy.Evaluator
	routes       []routescopes.RouteScope
	basePath     string
	logger       logging.Logger
}

// NewController creates an orbit-agents HTTP controller.
//
// routes is the same RouteScope slice that is registered with policy-service
// at startup (see cmd/orbit-agents/main.go agentRoutes). It is also used at
// request time by ScopeEnforcementMiddleware to enforce route → required-scope
// bindings in-process. basePath must match the server's BasePath so the
// middleware can reconstruct full paths after http.StripPrefix. See docs/adr/2026-06-04-platform-scope-enforcement-middleware.md.
func NewController(
	agentTypes *AgentTypeService,
	runs *RunService,
	environments *EnvironmentService,
	drafts *DraftService,
	verifier *token.Verifier,
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
//     on the agent pod (see cmd/orbit-agents k8s.go: "actual auth is the
//     identity-proxy-issued JWT"). ScopeEnforcementMiddleware is intentionally
//     omitted: the route→scope table (agentRoutes) only covers the public
//     surface, and agent principals hold no route scopes — running it would
//     deny every callback with "no route match". Per-run authorization (binding
//     the calling agent's tenant + slug to the target run) is enforced in each
//     internal handler via runs.AuthorizeAgentCallback. See docs/adr/2026-06-04-orbit-agents-authorization-model.md + docs/adr/2026-06-04-platform-scope-enforcement-middleware.md.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	publicChain := mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.DelegationMiddleware(c.logger, c.verifier),
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
	mux.Handle("POST /api/v1/runs", c.handleStartRun)
	mux.Handle("GET /api/v1/runs", c.handleListRuns)
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
}

// handleHealth returns liveness status.
func (c *Controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"status": "ok"})
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

	at, err := c.agentTypes.CreateAgentType(ctx, req)
	if err != nil {
		if errors.Is(err, ErrSlugTaken) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "slug already taken for this tenant", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrInvalidConnectivity) || errors.Is(err, ErrInvalidIdentityMode) {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrIdentityModeWriteForbidden) {
			// 403 with the explicit reason: the message names the missing
			// mode-qualified scope and the identity model's stakes.
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

func (c *Controller) handleListAgentTypes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Tenant is derived from the verified platform claims; query-string is ignored.
	types, err := c.agentTypes.ListAgentTypes(ctx)
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

	at, err := c.agentTypes.GetAgentType(ctx, id)
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
// environment + connector auto-egress, combined) for an agent type. Read-scoped
// via the /api/v1/agent-types/ GET binding (agents:types:read).
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

	at, err := c.agentTypes.UpdateAgentType(ctx, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrInvalidConnectivity) || errors.Is(err, ErrInvalidIdentityMode) || errors.Is(err, ErrIdentityModeImmutable) {
			apiErr := api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrIdentityModeWriteForbidden) {
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
		if errors.Is(err, ErrIdentityModeWriteForbidden) {
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

	// Lift the caller's raw bearer into context: a delegated agent's run start
	// forwards it to POST /identity/delegation so identity mints only authority
	// the caller already holds (delegated-agent-identity ADR, Decision 3). The
	// token was already verified by AuthMiddleware; this carries the raw string.
	if bearer := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); bearer != "" {
		ctx = WithCallerBearer(ctx, bearer)
	}

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
			// broken service: orbit-triggers retries a refusal while a young
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

func (c *Controller) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Tenant is derived from the verified platform claims; query-string is ignored.
	agentTypeID := r.URL.Query().Get("agent_type_id")
	// limit <= 0 (including absent) means unbounded — see ListRuns. An
	// unparseable value must 400, not silently fall through to unbounded: that
	// would turn a caller's typo into the one response shape they didn't ask for.
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			apiErr := api.BadRequestError(api.ErrCodeInvalidField, "limit must be an integer", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, err)
			return
		}
		limit = parsed
	}

	runs, err := c.runs.ListRuns(ctx, agentTypeID, limit)
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to list runs", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"runs": runs})
}

func (c *Controller) handleGetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	run, err := c.runs.GetRun(ctx, id)
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

type sendMessageRequest struct {
	Content string `json:"content"`
}

func (c *Controller) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var req sendMessageRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.Content == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "content is required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	if err := c.runs.SendMessage(ctx, id, req.Content); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
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
		if errors.Is(err, ErrRunNotAcceptingMessages) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "run is not accepting messages", nil)
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
// paginated JSON page. The JSON form backs non-streaming readers such as the Orbit
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
//
//nolint:gocyclo // SSE handler with replay + dedup + live fanout; splitting into helpers would obscure the protocol flow
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
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.SequenceNo, ev.EventType, ev.Payload)
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
			payload := ev.Payload
			if payload == nil {
				payload = []byte("{}")
			}
			if ev.SequenceNo > 0 {
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.SequenceNo, ev.Type, payload)
			} else {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
			}
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
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

	// ahead marks a pre-provision, which declines to create a pod while the
	// connection registry is still loading. Absent means on-demand, so an older
	// session-manager keeps the behavior it had.
	ahead := r.URL.Query().Get("ahead") == "true"

	sandbox, err := c.runs.ProvisionSandbox(ctx, id, ahead)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrKubeconfigNotReady) {
			// 503, not 409: nothing about the run conflicts, the connection
			// registry has simply not loaded yet, and the next request
			// succeeds once it has.
			apiErr := api.ServiceUnavailableError(api.ErrCodeServiceUnavailable, "sandbox pre-provision deferred: connection registry not resolved", map[string]any{"run_id": id})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to provision sandbox", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	// exec_token goes only to the session-manager that owns the run: the route is
	// restricted to the agent pod by AuthorizationPolicy, and authorizeCallback
	// has already bound the caller to this run.
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
		"endpoint":   sandbox.Endpoint,
		"exec_token": sandbox.ExecToken,
	})
}
