package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/token"
	"go.opentelemetry.io/otel/propagation"
)

// ControllerConfig holds defaults that the controller applies to new sessions.
type ControllerConfig struct {
	// ClaudeBinaryPath overrides the claude binary location.
	ClaudeBinaryPath string
	// WrapperPath is the path to orbit-session-runner. When set, every agent
	// subprocess runs through it, applying Landlock (docs/adr/2026-06-04-per-session-home-isolation.md).
	WrapperPath string
	// ShellPath is the orbit-shell proxy OpenCode is pointed at. Claude Code and
	// Codex resolve it from the image.
	ShellPath string
	// ClaudeBare enables --bare mode (required in containers).
	ClaudeBare bool
	// SelfPort is the port this session-manager listens on. orbit-runtime-mcp
	// uses it to call back for provision_sandbox requests.
	SelfPort int
	// WorkspacesBasePath is the base directory for run workspaces (e.g. /workspaces).
	// Used to set WORKSPACE env var for Claude and to write sandbox endpoint files.
	WorkspacesBasePath string

	// CodexBinaryPath overrides the codex binary location. Defaults to "codex".
	CodexBinaryPath string

	// OpenCodeBinaryPath overrides the opencode binary location. Defaults to "opencode".
	OpenCodeBinaryPath string

	// InferenceGatewayURL is the base URL of the Orbit inference gateway.
	// Injected as OPENAI_BASE_URL for Codex; written into opencode.json for OpenCode.
	InferenceGatewayURL string

	// ToolGatewayURL is the in-mesh base URL of the MCP tool-gateway, written
	// into the per-run MCP config so agents reach governed tools through it.
	ToolGatewayURL string

	// OTLPEndpoint is the platform collector Claude Code exports to. Empty
	// disables its telemetry.
	OTLPEndpoint string
}

// Controller implements the session-manager's internal HTTP API.
// orbit-agents calls this API to create sessions, send messages, and
// terminate runs. It is NOT exposed to external traffic.
// expectedCaller is the SYSTEM principal permitted to drive sessions. Only
// orbit-agents calls this API (internal/platform/clients/sessionmanager), so
// this is a constant rather than configuration: there is no deployment in which
// a different component should be able to start, message or kill a run, and a
// config key for it would only be a way to widen that by accident.
const expectedCaller = "orbit-agents"

type Controller struct {
	logger logging.Logger
	cfg    ControllerConfig
	// verifier validates the platform token on every session route. Always set:
	// there is no unauthenticated mode.
	verifier *token.Verifier
	registry *Registry
}

// NewController creates a session-manager HTTP controller.
//
// verifier is required. Authentication is not optional and has no off switch:
// local development runs in Kubernetes with a real identity service
// (docs/adr/2026-07-10-kubernetes-only-local-development.md), so there is no
// configuration in which this API should answer an unauthenticated caller.
func NewController(logger logging.Logger, cfg ControllerConfig, registry *Registry, verifier *token.Verifier) *Controller {
	return &Controller{
		logger:   logger,
		cfg:      cfg,
		verifier: verifier,
		registry: registry,
	}
}

func (c *Controller) GroupedBy() string { return "" }

// Middleware authenticates the session-control API.
//
// Three route classes, three postures:
//
//   - GET /health is open. The StatefulSet's readiness probe hits it
//     (internal/agents/k8s.go ensureStatefulSet) and the kubelet has no
//     platform token to present. It exposes liveness and a session count only.
//
//   - /internal/* is open at the application layer but restricted to the pod's
//     own loopback interface. Its only callers are orbit-runtime-mcp and
//     orbit-shell, which are child processes of the Claude subprocess inside
//     THIS container calling http://localhost:8090. They hold no platform
//     token and cannot obtain one: the call never leaves the pod, so the
//     identity-proxy ext_proc sidecar never sees it, and there is no inbound
//     request to forward a bearer from. Loopback is therefore the only
//     identity signal available. Off-pod traffic arrives through the Envoy
//     inbound listener with a non-loopback source (Istio uses 127.0.0.6 for
//     inbound passthrough, deliberately not matched here) and is rejected;
//     the mesh AuthorizationPolicy rejects it a layer earlier.
//
//   - Everything else — the session-control surface that can start, message,
//     interrupt, and kill any run — requires a valid platform token AND that
//     the token belongs to orbit-agents, the sole intended caller.
//
// There is no unauthenticated mode. The token check and the caller check are
// one decision, not two switches: requireExpectedCaller reads the identity the
// token check establishes, so it can never run without it.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	return func(next http.Handler) http.Handler {
		authed := mw.MiddlewareChain(
			mw.AuthMiddleware(c.verifier, c.logger),
			mw.NewMiddleware(c.requireExpectedCaller),
		)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/health":
				next.ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, "/internal/"):
				if !isLoopbackRequest(r) {
					ctx := r.Context()
					c.logger.Warn(ctx, "Rejected non-loopback call to session-manager internal route",
						logging.Field{Key: "path", Value: r.URL.Path},
						logging.Field{Key: "remote_addr", Value: r.RemoteAddr},
					)
					apiErr := api.ForbiddenError(api.ErrCodeForbidden, "internal routes are reachable only from within the agent pod", nil)
					api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
					return
				}
				next.ServeHTTP(w, r)
			default:
				authed.ServeHTTP(w, r)
			}
		})
	}
}

// requireExpectedCaller rejects any authenticated principal that is not the
// orbit-agents control plane.
//
// AuthMiddleware alone is not enough. Every in-mesh component can obtain a
// platform token, and the platform-wide convention is that a SYSTEM principal
// is unrestricted by scope enforcement
// (internal/platform/middleware/scope_enforce.go). Without a caller check, any
// component's token would unlock every session route on every agent pod in
// every tenant. The same reasoning drives the orbit-triggers
// AuthorizationPolicy (charts/orbit/templates/triggers/authorizationpolicy.yaml).
func (c *Controller) requireExpectedCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		claims, ok := token.ClaimsFromContext(ctx)
		if !ok || claims.Orbit.PrincipalType != token.PrincipalTypeSystem || claims.Orbit.DisplayName != expectedCaller {
			principalType, displayName := "", ""
			if claims != nil {
				principalType, displayName = string(claims.Orbit.PrincipalType), claims.Orbit.DisplayName
			}
			c.logger.Warn(ctx, "Rejected session-manager call from unexpected principal",
				logging.Field{Key: "path", Value: r.URL.Path},
				logging.Field{Key: "principal_type", Value: principalType},
				logging.Field{Key: "display_name", Value: displayName},
			)
			apiErr := api.ForbiddenError(api.ErrCodeForbidden, "session-manager accepts requests only from the orbit-agents control plane", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackRequest reports whether the request originated on this pod's
// loopback interface.
//
// Only 127.0.0.1 and ::1 count. Istio's inbound passthrough sources requests
// from 127.0.0.6, which IS a loopback address by net.IP.IsLoopback but means
// "arrived from off-pod through Envoy" — exactly the traffic these routes must
// refuse — so the check is an explicit match, not IsLoopback.
func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr == netip.MustParseAddr("127.0.0.1") || addr == netip.MustParseAddr("::1")
}

func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /health", c.handleHealth)
	mux.Handle("POST /session", c.handleCreateSession)
	mux.Handle("POST /session/{runID}/messages", c.handleSendMessage)
	mux.Handle("POST /session/{runID}/tool-results", c.handleSendToolResult)
	mux.Handle("POST /session/{runID}/interrupt", c.handleInterruptSession)
	mux.Handle("DELETE /session/{runID}", c.handleCloseSession)
	mux.Handle("POST /internal/session/{runID}/provision-sandbox", c.handleProvisionSandbox)
	mux.Handle("GET /internal/session/{runID}/sandbox", c.handleGetSandbox)
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
//
// Note: there is an independent CreateSessionRequest in
// internal/platform/clients/sessionmanager/client.go. The two structs are kept
// separate because the client package cannot import the server package (the
// dependency direction would be backwards).
type CreateSessionRequest struct {
	RunID        string `json:"run_id"`
	AgentTypeID  string `json:"agent_type_id"`
	TenantID     string `json:"tenant_id"`
	Framework    string `json:"framework"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
	// MCPServers are the distinct MCP servers the agent has tool grants on; one
	// governed endpoint (/v1/mcp/<server>) is provisioned per entry.
	MCPServers []string `json:"mcp_servers"`
	// ProvisionSandbox provisions this run's sandbox immediately instead of
	// waiting for the agent to ask for one.
	ProvisionSandbox bool `json:"provision_sandbox"`
}

// handleCreateSession starts an agent subprocess for the given run.
// The runner type is selected based on the Framework field.
// validRunID admits the UUIDs orbit-agents mints and nothing that could leave
// the workspaces directory: the run ID becomes a path segment under
// WorkspacesBasePath, so a separator or a dot-only segment would let a caller
// place a run's files, and its Landlock scope, outside its own workspace.
var validRunID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func (c *Controller) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateSessionRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	// The model comes from the agent type in the platform; there is no fallback.
	if req.RunID == "" || req.AgentTypeID == "" || req.TenantID == "" || req.Model == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "run_id, agent_type_id, tenant_id, and model are required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}
	if !validRunID.MatchString(req.RunID) {
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "run_id must be a single path-safe segment", map[string]any{"field": "run_id"})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	runToken, ok := c.prepareRun(w, r, req)
	if !ok {
		return
	}

	var runner Runner
	var inferenceURL string
	switch req.Framework {
	case "claude-code", "":
		// Empty framework also routes to claude-code: rows that pre-date
		// service.CreateAgentType's empty→claude-code defaulting still carry
		// the migration's NOT NULL DEFAULT '' value.
		extraEnv := c.runEnv(req.RunID, runToken)
		// Run identity travels as a base-URL path prefix: point this subprocess at
		// {gateway}/run/{run_id} so the gateway can attribute and enforce per-run
		// budgets. Transparent to the agent runtime; the inherited pod-level value
		// is stripped in buildRunEnv. See docs/adr/2026-06-10-hierarchical-budget-enforcement.md.
		if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
			inferenceURL = fmt.Sprintf("%s/run/%s", strings.TrimRight(base, "/"), req.RunID)
			extraEnv = append(extraEnv, "ANTHROPIC_BASE_URL="+inferenceURL)
		}
		extraEnv = append(extraEnv, claudeTelemetryEnv(ctx, c.cfg.OTLPEndpoint, req.RunID, req.TenantID)...)
		runner = NewClaudeRunner(ClaudeRunnerConfig{
			BinaryPath:        c.cfg.ClaudeBinaryPath,
			WrapperPath:       c.cfg.WrapperPath,
			Model:             req.Model,
			SystemPrompt:      req.SystemPrompt,
			Bare:              c.cfg.ClaudeBare,
			SessionManagerURL: fmt.Sprintf("http://localhost:%d", c.cfg.SelfPort),
			ToolGatewayURL:    toolGatewayURLForRun(c.cfg.ToolGatewayURL, req.RunID),
			MCPServers:        req.MCPServers,
			RunID:             req.RunID,
			ExtraEnv:          extraEnv,
			Logger:            c.logger,
		})
	case "codex":
		inferenceURL = inferenceURLForRun(c.cfg.InferenceGatewayURL, req.RunID)
		runner = NewCodexRunner(CodexRunnerConfig{
			BinaryPath:          c.cfg.CodexBinaryPath,
			WrapperPath:         c.cfg.WrapperPath,
			Model:               req.Model,
			InferenceGatewayURL: c.cfg.InferenceGatewayURL,
			Logger:              c.logger,
			RunID:               req.RunID,
			SystemPrompt:        req.SystemPrompt,
			ExtraEnv:            c.runEnv(req.RunID, runToken),
			SessionManagerURL:   fmt.Sprintf("http://localhost:%d", c.cfg.SelfPort),
			ToolGatewayURL:      toolGatewayURLForRun(c.cfg.ToolGatewayURL, req.RunID),
			MCPServers:          req.MCPServers,
		})
	case "opencode":
		inferenceURL = inferenceURLForRun(c.cfg.InferenceGatewayURL, req.RunID)
		runner = NewOpenCodeRunner(OpenCodeRunnerConfig{
			BinaryPath:          c.cfg.OpenCodeBinaryPath,
			WrapperPath:         c.cfg.WrapperPath,
			ShellPath:           c.cfg.ShellPath,
			Model:               req.Model,
			InferenceGatewayURL: c.cfg.InferenceGatewayURL,
			Logger:              c.logger,
			RunID:               req.RunID,
			SystemPrompt:        req.SystemPrompt,
			ExtraEnv:            c.runEnv(req.RunID, runToken),
			SessionManagerURL:   fmt.Sprintf("http://localhost:%d", c.cfg.SelfPort),
			ToolGatewayURL:      toolGatewayURLForRun(c.cfg.ToolGatewayURL, req.RunID),
			MCPServers:          req.MCPServers,
		})
	default:
		apiErr := api.ValidationError(api.ErrCodeInvalidField, fmt.Sprintf("unsupported framework %q", req.Framework), nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	cfg := SessionConfig{
		RunID:        req.RunID,
		AgentTypeID:  req.AgentTypeID,
		TenantID:     req.TenantID,
		Runner:       runner,
		Model:        req.Model,
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

	if req.ProvisionSandbox {
		c.provisionSandboxAhead(ctx, req.RunID)
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, map[string]any{"run_id": req.RunID})
}

// provisionSandboxAhead provisions the run's sandbox up front, so the first
// command does not pay the pod's cold start.
//
// Runs in the background: provisioning waits on pod readiness and the egress
// canary, which takes minutes, and the session is created either way. A failure
// is logged only; the agent can still request one itself. The context is
// detached from the request, which ends with this handler.
func (c *Controller) provisionSandboxAhead(ctx context.Context, runID string) {
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		if _, err := c.registry.ProvisionSandbox(bgCtx, runID, true); err != nil {
			c.logger.Warn(bgCtx, "Pre-provisioning the sandbox failed; the agent can still request one",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}()
}

// prepareRun rejects a request no runner can serve and returns the run's token,
// which the runner's environment carries to the agent. Returns false and writes
// the response when the run cannot start.
//
// The framework is checked before the token is written, so a request that
// cannot produce a session leaves nothing behind for the run.
func (c *Controller) prepareRun(w http.ResponseWriter, r *http.Request, req CreateSessionRequest) (string, bool) {
	ctx := r.Context()
	switch req.Framework {
	// Empty means claude-code, for rows predating the defaulting in
	// service.CreateAgentType.
	case "claude-code", "", "codex", "opencode":
	default:
		apiErr := api.ValidationError(api.ErrCodeInvalidField, fmt.Sprintf("unsupported framework %q", req.Framework), nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return "", false
	}

	runToken, err := mintRunToken(c.cfg.WorkspacesBasePath, req.RunID)
	if err != nil {
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to prepare the run", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return "", false
	}
	return runToken, true
}

// runEnv is the per-run environment every agent subprocess needs.
//
// WORKSPACE is where orbit-shell reads the sandbox descriptor. The URL and run
// ID let it call back here, which it needs to replace a sandbox whose pod has
// gone. ORBIT_RUN_TOKEN is how that call proves which run it is: loopback only
// proves the caller is in this pod, which every run here satisfies.
// orbit-shell is a child of the agent and inherits this environment.
//
// The install environment matches the sandbox's, so installs and caches land
// under the run's home either way. Its PATH carries the sandbox toolchains
// because Claude Code and Codex replay the agent's PATH in sandbox commands.
func (c *Controller) runEnv(runID, runToken string) []string {
	workspace := filepath.Join(c.cfg.WorkspacesBasePath, runID)
	env := []string{
		"WORKSPACE=" + workspace,
		fmt.Sprintf("ORBIT_SESSION_MANAGER_URL=http://localhost:%d", c.cfg.SelfPort),
		"ORBIT_RUN_ID=" + runID,
		"ORBIT_RUN_TOKEN=" + runToken,
	}
	return append(env, runfs.InstallEnv(runfs.HomeDir(workspace), os.Getenv("PATH"))...)
}

// requireRunToken binds the caller to the run in the path. Returns false and
// writes the response when it cannot.
//
// The run ID is validated first because it becomes a path segment when the
// token is read. handleCreateSession validates the one it is given; this is
// the same guard for an ID that arrives from the URL.
func (c *Controller) requireRunToken(w http.ResponseWriter, r *http.Request, runID string) bool {
	ctx := r.Context()
	if !validRunID.MatchString(runID) {
		c.logger.Warn(ctx, "Rejected internal call with an unusable run id",
			logging.Field{Key: "path", Value: r.URL.Path},
		)
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "run_id must be a single path-safe segment", map[string]any{"field": "run_id"})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return false
	}
	if verifyRunToken(c.cfg.WorkspacesBasePath, runID, r.Header.Get(runTokenHeader)) {
		return true
	}
	c.logger.Warn(ctx, "Rejected internal call without this run's token",
		logging.Field{Key: "path", Value: r.URL.Path},
		logging.Field{Key: "run_id", Value: runID},
	)
	apiErr := api.ForbiddenError(api.ErrCodeForbidden, "caller is not this run", map[string]any{"run_id": runID})
	api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
	return false
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

	api.NoBody(w, http.StatusAccepted)
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

	api.NoBody(w, http.StatusAccepted)
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

	api.NoBody(w, http.StatusAccepted)
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

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// handleProvisionSandbox is called by orbit-runtime-mcp (running inside Claude)
// to provision a sandbox pod. It delegates to orbit-agents then writes the
// gRPC endpoint to disk so orbit-shell can route Bash commands to the sandbox.
func (c *Controller) handleProvisionSandbox(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := r.PathValue("runID")
	if !c.requireRunToken(w, r, runID) {
		return
	}

	endpoint, err := c.registry.ProvisionSandbox(ctx, runID, false)
	if err != nil {
		// A run this pod does not host is a caller aimed at the wrong session,
		// which a 500 would bury among ordinary Kubernetes failures.
		if errors.Is(err, ErrSessionNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "session not found", map[string]any{"run_id": runID})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to provision sandbox", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"endpoint": endpoint})
}

// handleGetSandbox reports this run's sandbox, or 404 if it has none.
//
// orbit-shell asks when the workspace descriptor is missing, which the agent can
// arrange by deleting it. 404 is the only answer that sends it to a local shell.
func (c *Controller) handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := r.PathValue("runID")
	if !c.requireRunToken(w, r, runID) {
		return
	}

	sandbox, ok, err := c.registry.Sandbox(ctx, runID)
	if err != nil {
		// Not 404: orbit-shell reads that as permission to run the command
		// locally. A lookup we could not complete must stop the command, not
		// relax where it runs.
		apiErr := api.ServiceUnavailableError(api.ErrCodeServiceUnavailable, "could not determine this run's sandbox", map[string]any{"run_id": runID})
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}
	if !ok {
		apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "no sandbox for this run", map[string]any{"run_id": runID})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
		"endpoint":   sandbox.Endpoint,
		"exec_token": sandbox.ExecToken,
	})
}

// inferenceURLForRun builds the per-run inference base URL used for
// summarization and per-run budget attribution.
func inferenceURLForRun(gatewayURL, runID string) string {
	return fmt.Sprintf("%s/run/%s", strings.TrimRight(gatewayURL, "/"), runID)
}

// toolGatewayURLForRun builds the per-run tool-gateway base URL written into
// the per-run MCP config. Run identity travels as the same /run/{run_id} path
// prefix the inference gateway uses, so MCP traffic carries run identity for
// delegation binding and attribution (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 4). An empty gateway URL stays empty — no governed MCP entries.
func toolGatewayURLForRun(gatewayURL, runID string) string {
	if gatewayURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/run/%s", strings.TrimRight(gatewayURL, "/"), runID)
}

// claudeTelemetryEnv returns the environment that has Claude Code export its
// traces, metrics and events to the platform collector, with every turn a child
// of the span in ctx. Empty when no endpoint is configured, matching the
// platform services. See docs/adr/2026-09-18-agent-runtime-trace-propagation.md.
func claudeTelemetryEnv(ctx context.Context, endpoint, runID, tenantID string) []string {
	if endpoint == "" {
		return nil
	}
	env := []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY=1",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1",
		"OTEL_TRACES_EXPORTER=otlp",
		"OTEL_METRICS_EXPORTER=otlp",
		"OTEL_LOGS_EXPORTER=otlp",
		"OTEL_EXPORTER_OTLP_PROTOCOL=grpc",
		"OTEL_EXPORTER_OTLP_ENDPOINT=" + endpoint,
		// ANTHROPIC_BASE_URL points at the inference gateway, and Claude Code
		// only sends traceparent to a non-Anthropic base URL when told to.
		"CLAUDE_CODE_PROPAGATE_TRACEPARENT=1",
		"OTEL_RESOURCE_ATTRIBUTES=orbit.run_id=" + runID + ",orbit.tenant_id=" + tenantID,
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	for _, key := range carrier.Keys() {
		env = append(env, strings.ToUpper(key)+"="+carrier.Get(key))
	}
	return env
}
