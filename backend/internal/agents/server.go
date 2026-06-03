package agents

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Controller implements the orbit-agents HTTP API.
type Controller struct {
	agentTypes *AgentTypeService
	runs       *RunService
	verifier   *token.Verifier
	eval       policy.Evaluator
	routes     []routescopes.RouteScope
	basePath   string
	logger     logging.Logger
}

// NewController creates an orbit-agents HTTP controller.
//
// routes is the same RouteScope slice that is registered with policy-service
// at startup (see cmd/orbit-agents/main.go agentRoutes). It is also used at
// request time by ScopeEnforcementMiddleware to enforce route → required-scope
// bindings in-process. basePath must match the server's BasePath so the
// middleware can reconstruct full paths after http.StripPrefix. See ADR-0064.
func NewController(
	agentTypes *AgentTypeService,
	runs *RunService,
	verifier *token.Verifier,
	eval policy.Evaluator,
	routes []routescopes.RouteScope,
	basePath string,
	logger logging.Logger,
) *Controller {
	return &Controller{
		agentTypes: agentTypes,
		runs:       runs,
		verifier:   verifier,
		eval:       eval,
		routes:     routes,
		basePath:   basePath,
		logger:     logger,
	}
}

func (c *Controller) GroupedBy() string { return "" }

// Middleware applies JWT auth and scope enforcement to public routes but
// bypasses internal callback endpoints, which are protected by Istio
// AuthorizationPolicy at the mTLS layer (session-manager pod's SPIFFE
// identity), and the unauthenticated /health probe. See ADR-0063 + ADR-0064.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	authChain := mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.DelegationMiddleware(c.logger, c.verifier),
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)
	return func(next http.Handler) http.Handler {
		authed := authChain(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/internal/") || r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			authed.ServeHTTP(w, r)
		})
	}
}

func (c *Controller) Boot(mux *api.Mux) {
	// Public API
	mux.Handle("GET /health", c.handleHealth)
	mux.Handle("GET /api/v1/agent-types/tool-scopes", c.handleListToolScopes)
	mux.Handle("POST /api/v1/agent-types", c.handleCreateAgentType)
	mux.Handle("GET /api/v1/agent-types", c.handleListAgentTypes)
	mux.Handle("GET /api/v1/agent-types/{id}", c.handleGetAgentType)
	mux.Handle("PATCH /api/v1/agent-types/{id}", c.handleUpdateAgentType)
	mux.Handle("DELETE /api/v1/agent-types/{id}", c.handleDeleteAgentType)
	mux.Handle("POST /api/v1/runs", c.handleStartRun)
	mux.Handle("GET /api/v1/runs", c.handleListRuns)
	mux.Handle("GET /api/v1/runs/{id}", c.handleGetRun)
	mux.Handle("DELETE /api/v1/runs/{id}", c.handleTerminateRun)
	mux.Handle("POST /api/v1/runs/{id}/messages", c.handleSendMessage)
	mux.Handle("POST /api/v1/runs/{id}/interrupt", c.handleInterruptRun)
	mux.Handle("GET /api/v1/runs/{id}/events", c.handleStreamEvents)

	// Internal — session-manager callbacks (Istio AuthorizationPolicy restricts these)
	mux.Handle("POST /internal/runs/{id}/state", c.handleRunStateCallback)
	mux.Handle("POST /internal/runs/{id}/events", c.handlePushEvent)
	mux.Handle("POST /internal/runs/{id}/provision-sandbox", c.handleProvisionSandbox)
}

// handleHealth returns liveness status.
func (c *Controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"status": "ok"})
}

// ── Agent Types ───────────────────────────────────────────────────────────────

func (c *Controller) handleListToolScopes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"tool_scopes": SelectableToolScopes})
}

func (c *Controller) handleCreateAgentType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateAgentTypeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	// Tenant is derived from the verified platform claims by the service layer;
	// any request-body tenant_id is ignored. See ADR-0063.
	if req.Name == "" || req.Slug == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "name and slug are required", nil)
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

func (c *Controller) handleUpdateAgentType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var req UpdateAgentTypeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	req.ID = id

	at, err := c.agentTypes.UpdateAgentType(ctx, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil)
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
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to delete agent type", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

	run, err := c.runs.StartRun(ctx, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to start run", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, run)
}

func (c *Controller) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Tenant is derived from the verified platform claims; query-string is ignored.
	agentTypeID := r.URL.Query().Get("agent_type_id")

	runs, err := c.runs.ListRuns(ctx, agentTypeID)
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

	w.WriteHeader(http.StatusNoContent)
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
		if errors.Is(err, ErrRunNotAcceptingMessages) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "run is not accepting messages", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to send message", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
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
		if errors.Is(err, ErrRunNotAcceptingMessages) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "run is not active", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to interrupt run", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// ── Internal callbacks ────────────────────────────────────────────────────────

type runStateCallbackRequest struct {
	From RunState `json:"from"`
	To   RunState `json:"to"`
}

func (c *Controller) handleRunStateCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

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

	w.WriteHeader(http.StatusNoContent)
}

// handleStreamEvents serves a Server-Sent Events stream for a run.
//
// On connect: subscribes to the in-memory PubSub first (to avoid a gap),
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
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
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

// handlePushEvent receives an event pushed by a session-manager pod and fans
// it out to any live SSE subscribers for that run.
func (c *Controller) handlePushEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var ev PushedEvent
	if !api.DecodeJSON(ctx, r, w, c.logger, &ev) {
		return
	}

	c.runs.PublishEvent(ctx, id, ev)
	w.WriteHeader(http.StatusNoContent)
}

func (c *Controller) handleProvisionSandbox(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	endpoint, err := c.runs.ProvisionSandbox(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to provision sandbox", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"endpoint": endpoint})
}
