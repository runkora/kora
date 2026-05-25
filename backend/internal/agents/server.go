package agents

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// Controller implements the orbit-agents HTTP API.
type Controller struct {
	svc    *Service
	logger logging.Logger
}

// NewController creates an orbit-agents HTTP controller.
func NewController(svc *Service, logger logging.Logger) *Controller {
	return &Controller{svc: svc, logger: logger}
}

func (c *Controller) GroupedBy() string                { return "" }
func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }

func (c *Controller) Boot(mux *api.Mux) {
	// Public API
	mux.Handle("GET /health", c.handleHealth)
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

func (c *Controller) handleCreateAgentType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateAgentTypeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.TenantID == "" || req.Name == "" || req.Slug == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id, name, and slug are required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	at, err := c.svc.CreateAgentType(ctx, req)
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
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id query parameter is required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	types, err := c.svc.ListAgentTypes(ctx, tenantID)
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

	at, err := c.svc.GetAgentType(ctx, id)
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

	at, err := c.svc.UpdateAgentType(ctx, req)
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

	if err := c.svc.DeleteAgentType(ctx, id); err != nil {
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
	if req.TenantID == "" || req.AgentTypeID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id and agent_type_id are required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	run, err := c.svc.StartRun(ctx, req)
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
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id query parameter is required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}
	agentTypeID := r.URL.Query().Get("agent_type_id")

	runs, err := c.svc.ListRuns(ctx, tenantID, agentTypeID)
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

	run, err := c.svc.GetRun(ctx, id)
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

	if err := c.svc.TerminateRun(ctx, id); err != nil {
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

	if err := c.svc.SendMessage(ctx, id, req.Content); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "run not found", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to send message", nil)
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

	if err := c.svc.TransitionRunState(ctx, id, req.From, req.To); err != nil {
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

	if _, err := c.svc.GetRun(ctx, id); err != nil {
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
	ch, cancel := c.svc.SubscribeRun(id)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Replay persisted events from DB.
	events, err := c.svc.ListRunEvents(ctx, id, afterSeqNo)
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

	c.svc.PublishEvent(ctx, id, ev)
	w.WriteHeader(http.StatusNoContent)
}

func (c *Controller) handleProvisionSandbox(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	endpoint, err := c.svc.ProvisionSandbox(ctx, id)
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
