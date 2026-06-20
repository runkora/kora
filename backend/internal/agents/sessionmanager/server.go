package sessionmanager

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// ControllerConfig holds defaults that the controller applies to new sessions.
type ControllerConfig struct {
	// ClaudeBinaryPath overrides the claude binary location.
	ClaudeBinaryPath string
	// ClaudeWrapperPath is the path to orbit-session-runner. When set, it wraps
	// the claude subprocess to apply Landlock filesystem restrictions (docs/adr/2026-06-04-per-session-home-isolation.md).
	ClaudeWrapperPath string
	// ClaudeDefaultModel is used when the session request omits a model.
	ClaudeDefaultModel string
	// ClaudeBare enables --bare mode (required in containers).
	ClaudeBare bool
	// SelfPort is the port this session-manager listens on. orbit-runtime-mcp
	// uses it to call back for provision_sandbox requests.
	SelfPort int
	// WorkspacesBasePath is the base directory for run workspaces (e.g. /workspaces).
	// Used to set WORKSPACE env var for Claude and to write sandbox endpoint files.
	WorkspacesBasePath string
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
	mux.Handle("POST /session/{runID}/tool-results", c.handleSendToolResult)
	mux.Handle("POST /session/{runID}/interrupt", c.handleInterruptSession)
	mux.Handle("DELETE /session/{runID}", c.handleCloseSession)
	mux.Handle("POST /internal/session/{runID}/provision-sandbox", c.handleProvisionSandbox)
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
	SystemPrompt string   `json:"system_prompt"`
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
	extraEnv := []string{fmt.Sprintf("WORKSPACE=%s", filepath.Join(c.cfg.WorkspacesBasePath, req.RunID))}
	// Run identity travels as a base-URL path prefix: point this subprocess at
	// {gateway}/run/{run_id} so the gateway can attribute and enforce per-run
	// budgets. Transparent to the agent runtime; the inherited pod-level value
	// is stripped in buildRunEnv. See docs/adr/2026-06-10-hierarchical-budget-enforcement.md.
	var inferenceURL string
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		inferenceURL = fmt.Sprintf("%s/run/%s", strings.TrimRight(base, "/"), req.RunID)
		extraEnv = append(extraEnv, "ANTHROPIC_BASE_URL="+inferenceURL)
	}
	runner := NewClaudeRunner(ClaudeRunnerConfig{
		BinaryPath:        c.cfg.ClaudeBinaryPath,
		WrapperPath:       c.cfg.ClaudeWrapperPath,
		Model:             model,
		AllowedTools:      req.AllowedTools,
		SystemPrompt:      req.SystemPrompt,
		Bare:              c.cfg.ClaudeBare,
		SessionManagerURL: fmt.Sprintf("http://localhost:%d", c.cfg.SelfPort),
		RunID:             req.RunID,
		ExtraEnv:          extraEnv,
		Logger:            c.logger,
	})

	cfg := SessionConfig{
		RunID:        req.RunID,
		AgentTypeID:  req.AgentTypeID,
		TenantID:     req.TenantID,
		Runner:       runner,
		Model:        model,
		InferenceURL: inferenceURL,
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

// SendToolResultRequest is the body for POST /session/{runID}/tool-results.
type SendToolResultRequest struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// handleSendToolResult injects a tool result for a pending AskFollowupQuestion.
// orbit-agents calls this when the user has answered a question Claude asked.
func (c *Controller) handleSendToolResult(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := r.PathValue("runID")

	var req SendToolResultRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.ToolUseID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tool_use_id is required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	if err := c.registry.SendToolResult(ctx, runID, req.ToolUseID, req.Content); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "session not found", map[string]any{"run_id": runID})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to send tool result", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// handleInterruptSession sends SIGINT to the subprocess, stopping the current
// in-progress turn. The session and subprocess remain alive.
func (c *Controller) handleInterruptSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := r.PathValue("runID")

	if err := c.registry.Interrupt(ctx, runID); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "session not found", map[string]any{"run_id": runID})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to interrupt session", nil)
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

// handleProvisionSandbox is called by orbit-runtime-mcp (running inside Claude)
// to provision a sandbox pod. It delegates to orbit-agents then writes the
// gRPC endpoint to disk so orbit-shell can route Bash commands to the sandbox.
func (c *Controller) handleProvisionSandbox(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := r.PathValue("runID")

	endpoint, err := c.registry.ProvisionSandbox(ctx, runID)
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to provision sandbox", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"endpoint": endpoint})
}
