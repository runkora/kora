package sessionmanager

import (
	"errors"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// ControllerConfig holds defaults that the controller applies to new sessions.
type ControllerConfig struct {
	// ClaudeBinaryPath overrides the claude binary location.
	ClaudeBinaryPath string
	// ClaudeDefaultModel is used when the session request omits a model.
	ClaudeDefaultModel string
	// ClaudeBare enables --bare mode (required in containers).
	ClaudeBare bool
}

// Controller implements the session-manager's internal HTTP API.
// orbit-agents calls this API to create sessions, send messages, and
// terminate runs. It is NOT exposed to external traffic.
type Controller struct {
	logger   logging.Logger
	cfg      ControllerConfig
	registry *Registry
}

// NewController creates a session-manager HTTP controller.
func NewController(logger logging.Logger, cfg ControllerConfig, registry *Registry) *Controller {
	return &Controller{
		logger:   logger,
		cfg:      cfg,
		registry: registry,
	}
}

func (c *Controller) GroupedBy() string { return "" }

func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }

func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /health", c.handleHealth)
	mux.Handle("POST /session", c.handleCreateSession)
	mux.Handle("POST /session/{runID}/messages", c.handleSendMessage)
	mux.Handle("DELETE /session/{runID}", c.handleCloseSession)
}

// handleHealth returns liveness + active session count.
func (c *Controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
		"status":          "ok",
		"active_sessions": c.registry.ActiveCount(),
	})
}

// CreateSessionRequest is the body for POST /session.
type CreateSessionRequest struct {
	RunID        string   `json:"run_id"`
	AgentTypeID  string   `json:"agent_type_id"`
	TenantID     string   `json:"tenant_id"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools"`
}

// handleCreateSession starts a new Claude Code subprocess for the given run.
func (c *Controller) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateSessionRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.RunID == "" || req.AgentTypeID == "" || req.TenantID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "run_id, agent_type_id, and tenant_id are required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	model := req.Model
	if model == "" {
		model = c.cfg.ClaudeDefaultModel
	}
	runner := NewClaudeRunner(ClaudeRunnerConfig{
		BinaryPath:   c.cfg.ClaudeBinaryPath,
		Model:        model,
		AllowedTools: req.AllowedTools,
		Bare:         c.cfg.ClaudeBare,
	})

	cfg := SessionConfig{
		RunID:       req.RunID,
		AgentTypeID: req.AgentTypeID,
		TenantID:    req.TenantID,
		Runner:      runner,
	}

	if err := c.registry.Create(ctx, cfg); err != nil {
		if errors.Is(err, ErrSessionExists) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "session already exists", map[string]any{"run_id": req.RunID})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to create session", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, map[string]any{"run_id": req.RunID})
}

// SendMessageRequest is the body for POST /session/{runID}/messages.
type SendMessageRequest struct {
	Content string `json:"content"`
}

// handleSendMessage delivers a user message to an active session.
func (c *Controller) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := r.PathValue("runID")

	var req SendMessageRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.Content == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "content is required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	if err := c.registry.Send(ctx, runID, req.Content); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "session not found", map[string]any{"run_id": runID})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to send message", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// handleCloseSession terminates a session and cleans up the subprocess.
func (c *Controller) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := r.PathValue("runID")

	if err := c.registry.Close(ctx, runID); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "session not found", map[string]any{"run_id": runID})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to close session", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
