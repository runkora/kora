package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
	"github.com/clear-route/orbit/internal/platform/brand"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// OpenCodeRunnerConfig holds settings for the OpenCode ACP subprocess.
type OpenCodeRunnerConfig struct {
	// BinaryPath is the path to the opencode binary. Defaults to "opencode".
	BinaryPath string

	// WrapperPath is the path to orbit-session-runner. When set, opencode is
	// invoked as: WrapperPath <workDir> <BinaryPath> [args...] so Landlock is
	// applied first. Empty invokes opencode directly, with no filesystem
	// restriction (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
	WrapperPath string

	// ShellPath is written into the OpenCode config as the "shell" key, so tool
	// commands run through orbit-shell in the run's sandbox rather than in this
	// container. Empty leaves OpenCode on the container's own shell.
	ShellPath string

	// ExtraEnv is appended to the subprocess environment (WORKSPACE and the
	// orbit-shell callback variables).
	ExtraEnv []string

	// SystemPrompt, when non-empty, reaches OpenCode as an instructions file
	// named in the OpenCode config. ACP has no field for it.
	SystemPrompt string

	// SessionManagerURL is where orbit-runtime-mcp calls back to provision a
	// sandbox. Always http://localhost:{port}, since it runs in this pod.
	SessionManagerURL string

	// ToolGatewayURL is the in-mesh base URL of the MCP tool-gateway. Empty
	// disables the governed entries.
	ToolGatewayURL string

	// Brand names the product as the provider OpenCode shows the agent.
	Brand brand.Brand

	// MCPServers are the governed MCP servers this agent holds tool grants on,
	// one remote entry each.
	MCPServers []string

	// Model is the agent type's model, served by the inference gateway. It is
	// written into the OpenCode config as the only model OpenCode can use.
	Model string

	// InferenceGatewayURL is written directly into the OpenCode config so OpenCode
	// routes all LLM calls through the kora inference gateway. Must be non-empty.
	InferenceGatewayURL string

	// Logger is used to emit structured log events.
	Logger logging.Logger

	// RunID is the run identifier for this session.
	RunID string

	// ResumeSessionID, when non-empty, instructs Start to issue session/load
	// instead of session/new, restoring the prior conversation by ID. Set by
	// NewResumeRunner. Per the ACP spec, session/load is only available when
	// the agent advertises the loadSession capability on initialize.
	ResumeSessionID string
}

// openCodeMsg is a JSON-RPC 2.0 message as used by the OpenCode ACP protocol.
// A message is:
//   - a client request       if ID != nil && Method != ""
//   - a server→client request if ID != nil && Method != "" (server also sends requests)
//   - a response             if ID != nil && Result or Error is set && Method == ""
//   - a notification         if ID == nil
type openCodeMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// OpenCodeRunner implements Runner for the OpenCode ACP subprocess.
//
// The subprocess is started with:
//
//	opencode acp --cwd <workDir>
//
// Communication is over JSON-RPC 2.0 JSONL on stdin/stdout.
// Token-level text streaming arrives via session/update notifications.
type OpenCodeRunner struct {
	cfg            OpenCodeRunnerConfig
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdinMu        sync.Mutex // guards all stdin writes (Send + permission responses)
	sessionID      string
	events         chan Event
	once           sync.Once
	textBuf        turnText
	thinkingBuf    strings.Builder
	pendingMu      sync.Mutex
	pending        map[int64]chan openCodeMsg // in-flight handshake requests
	pendingPrompts map[int64]bool             // in-flight session/prompt requests; a message sent mid-turn adds a second
	nextID         atomic.Int64
	toolCalls      map[string]*openCodeToolCall // tool calls seen this session, by toolCallId
	replaying      atomic.Bool                  // true while session/load replay is in progress; suppresses ephemeral events so the channel does not fill before drain() starts
}

// NewOpenCodeRunner creates a runner with the given configuration.
func NewOpenCodeRunner(cfg OpenCodeRunnerConfig) *OpenCodeRunner {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "opencode"
	}
	return &OpenCodeRunner{
		cfg: cfg,
		// 256 slots: during session/load replay, ephemeral events are suppressed
		// (see emit), so only durable events (tool_use + tool_result, 2 per tool call)
		// reach this channel before drain() starts consuming. 256 handles sessions
		// with up to 128 historical tool calls without blocking readLoop.
		events:         make(chan Event, 256),
		pending:        make(map[int64]chan openCodeMsg),
		pendingPrompts: make(map[int64]bool),
		toolCalls:      make(map[string]*openCodeToolCall),
	}
}

// Start validates config, prepares the run's OpenCode config, spawns the subprocess, and
// performs the initialize + session/new handshake synchronously.
func (r *OpenCodeRunner) Start(ctx context.Context, workDir string) error {
	env, err := r.prepare(workDir)
	if err != nil {
		return fmt.Errorf("opencode: %w", err)
	}

	bin, args := wrapExec(r.cfg.WrapperPath, workDir, r.cfg.BinaryPath, "acp", "--cwd", workDir)
	r.cmd = exec.CommandContext(ctx, bin, args...)
	r.cmd.Dir = workDir
	r.cmd.Env = env

	r.stdin, err = r.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("opencode stdin pipe: %w", err)
	}

	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("opencode stdout pipe: %w", err)
	}

	if err = r.cmd.Start(); err != nil {
		return fmt.Errorf("opencode start: %w", err)
	}

	r.cfg.Logger.Info(ctx, "OpenCode subprocess started",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "pid", Value: r.cmd.Process.Pid},
		logging.Field{Key: "model", Value: r.cfg.Model},
	)

	go r.readLoop(ctx, stdout)

	// cleanupProc kills and reaps the subprocess when Start returns an error
	// after the process has already been launched.
	cleanupProc := func() {
		r.cmd.Process.Kill() //nolint:errcheck
		_ = r.stdin.Close()
		_ = r.cmd.Wait()
	}

	// Synchronous initialize handshake — 30-second deadline.
	initResult, err := r.sendRPCRequest(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
		"clientInfo":         map[string]string{"name": "orbit-session-manager", "version": "1.0"},
	})
	if err != nil {
		cleanupProc()
		return fmt.Errorf("opencode initialize: %w", err)
	}

	// Resume path requires the agent to advertise loadSession. Fail-fast if a
	// future OpenCode upgrade flips it off rather than silently degrading.
	if r.cfg.ResumeSessionID != "" {
		var initParsed struct {
			AgentCapabilities struct {
				LoadSession bool `json:"loadSession"`
			} `json:"agentCapabilities"`
		}
		if err = json.Unmarshal(initResult, &initParsed); err != nil {
			return fmt.Errorf("opencode initialize result: %w", err)
		}
		if !initParsed.AgentCapabilities.LoadSession {
			return fmt.Errorf("opencode: agent does not advertise loadSession capability — cannot resume session %q", r.cfg.ResumeSessionID)
		}

		// session/load — replays the entire conversation history via session/update
		// notifications, which the existing handleSessionUpdate consumes.
		// Suppress ephemeral events (text_delta, thinking_delta) during replay so
		// the channel buffer cannot fill before drain() starts consuming: only durable
		// events (tool_use, tool_result) are emitted, and those are bounded by the
		// number of historical tool calls, which 256 slots handles comfortably.
		r.replaying.Store(true)
		if _, err = r.sendRPCRequest(ctx, "session/load", map[string]any{
			"sessionId":  r.cfg.ResumeSessionID,
			"cwd":        workDir,
			"mcpServers": []any{},
		}); err != nil {
			r.replaying.Store(false)
			cleanupProc()
			return fmt.Errorf("opencode session/load: %w", err)
		}
		r.replaying.Store(false)
		// Discard any text/thinking accumulated from replayed messages — they belong
		// to the prior session and must not leak into the first real turn's buffer.
		r.textBuf.Reset()
		r.thinkingBuf.Reset()
		r.sessionID = r.cfg.ResumeSessionID
		r.cfg.Logger.Info(ctx, "OpenCode session loaded",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "session_id", Value: r.sessionID},
		)
		return nil
	}

	// Fresh session path: session/new — 30-second deadline.
	result, err := r.sendRPCRequest(ctx, "session/new", map[string]any{
		"cwd":        workDir,
		"mcpServers": []any{},
	})
	if err != nil {
		cleanupProc()
		return fmt.Errorf("opencode session/new: %w", err)
	}
	var newResult struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &newResult); err != nil {
		return fmt.Errorf("opencode session/new result: %w", err)
	}
	r.sessionID = newResult.SessionID

	r.cfg.Logger.Info(ctx, "OpenCode session created",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "session_id", Value: r.sessionID},
	)

	return nil
}

// Send delivers a user message as a session/prompt request.
// Returns immediately — token-level deltas flow through the Events() channel.
func (r *OpenCodeRunner) Send(_ context.Context, text string) error {
	id := r.nextID.Add(1)

	r.pendingMu.Lock()
	r.pendingPrompts[id] = true
	r.pendingMu.Unlock()

	msg := openCodeMsg{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/prompt",
		Params: mustMarshal(map[string]any{
			"sessionId": r.sessionID,
			"prompt":    []map[string]string{{"type": "text", "text": text}},
		}),
	}
	r.stdinMu.Lock()
	defer r.stdinMu.Unlock()
	return r.writeJSONL(msg)
}

// SendToolResult is not supported — OpenCode handles tool execution internally.
func (r *OpenCodeRunner) SendToolResult(_ context.Context, _, _ string) error {
	return ErrNotSupported
}

// Events returns the channel of kora standard events.
func (r *OpenCodeRunner) Events() <-chan Event {
	return r.events
}

// Interrupt stops the in-progress prompt with ACP's session/cancel: the prompt
// then answers with its canceled stop reason and the process stays alive, as a
// Codex turn/interrupt does. With no prompt in flight it falls back to SIGINT,
// and the session layer resumes with session/load.
func (r *OpenCodeRunner) Interrupt() error {
	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	r.pendingMu.Lock()
	inFlight := len(r.pendingPrompts) > 0
	r.pendingMu.Unlock()
	if !inFlight {
		return r.cmd.Process.Signal(os.Interrupt)
	}
	r.stdinMu.Lock()
	defer r.stdinMu.Unlock()
	return r.writeJSONL(openCodeMsg{
		JSONRPC: "2.0",
		Method:  "session/cancel",
		Params:  mustMarshal(map[string]string{"sessionId": r.sessionID}),
	})
}

// SessionID returns the OpenCode session ID captured from the session/new
// response (or carried in via ResumeSessionID for a resumed runner). Empty
// until Start has completed.
func (r *OpenCodeRunner) SessionID() string {
	return r.sessionID
}

// NewResumeRunner creates a runner configured to resume this OpenCode session
// via session/load. Returns nil if no session ID has been captured yet (e.g.
// Start failed before session/new completed).
func (r *OpenCodeRunner) NewResumeRunner() Runner {
	if r.sessionID == "" {
		return nil
	}
	cfg := r.cfg
	cfg.ResumeSessionID = r.sessionID
	return NewOpenCodeRunner(cfg)
}

// Close sends session/close, closes stdin, and waits for the subprocess to exit.
func (r *OpenCodeRunner) Close() error {
	var firstErr error
	r.once.Do(func() {
		// Best-effort: send session/close before EOF.
		r.stdinMu.Lock()
		_ = r.writeJSONL(openCodeMsg{
			JSONRPC: "2.0",
			Method:  "session/close",
			Params:  mustMarshal(map[string]string{"sessionId": r.sessionID}),
		})
		r.stdinMu.Unlock()

		if err := r.stdin.Close(); err != nil {
			firstErr = err
		}
		if err := r.cmd.Wait(); err != nil && !isExitKilled(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	})
	return firstErr
}

// openCodeOfflineEnv turns off OpenCode's own calls home: the models.dev
// catalog, update checks, sharing, provider auth plugins and LSP downloads.
var openCodeOfflineEnv = []string{
	"OPENCODE_DISABLE_MODELS_FETCH=1",
	"OPENCODE_DISABLE_AUTOUPDATE=1",
	"OPENCODE_DISABLE_SHARE=1",
	"OPENCODE_DISABLE_DEFAULT_PLUGINS=1",
	"OPENCODE_DISABLE_LSP_DOWNLOAD=1",
}

// prepare validates the config, writes the run's OpenCode files and returns the
// subprocess environment.
func (r *OpenCodeRunner) prepare(workDir string) ([]string, error) {
	if r.cfg.InferenceGatewayURL == "" {
		return nil, errors.New("InferenceGatewayURL must not be empty")
	}
	// Without a model OpenCode falls back to its own hosted free models.
	if r.cfg.Model == "" {
		return nil, errors.New("model must not be empty")
	}
	config, err := r.openCodeConfig(workDir)
	if err != nil {
		return nil, fmt.Errorf("build config: %w", err)
	}
	if err = lockOpenCodeGlobalConfig(workDir); err != nil {
		return nil, err
	}
	homeEnv, err := runHomeDirs(workDir)
	if err != nil {
		return nil, err
	}
	// The platform's values last, so ExtraEnv cannot override them. HOME and
	// TMPDIR keep the agent on the only directory Landlock makes writable.
	env := append(append([]string{}, r.cfg.ExtraEnv...), openCodeOfflineEnv...)
	env = append(env, "OPENCODE_CONFIG_CONTENT="+string(config))
	return withRunEnv(os.Environ(), append(env, homeEnv...)), nil
}

// lockOpenCodeGlobalConfig makes OpenCode's global config directory read-only.
// At start OpenCode installs @opencode-ai/plugin from npm into every config
// directory it can write, and skips one it cannot.
func lockOpenCodeGlobalConfig(workDir string) error {
	dir := filepath.Join(runfs.HomeDir(workDir), ".config", "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create global config dir: %w", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		return fmt.Errorf("lock global config dir: %w", err)
	}
	return nil
}

// openCodeProvider is the only provider OpenCode may load: the inference gateway.
// It is the provider's table key and the model id prefix, not the product name.
const openCodeProvider = "orbit"

// openCodeConfig is the run's OpenCode config. The agent type's model is the
// only model on offer, served by the inference gateway, and every other provider
// (OpenCode's hosted free models included) is switched off. It reaches OpenCode
// as OPENCODE_CONFIG_CONTENT, which is merged after every config file, so an
// opencode.json or .opencode/ directory in the workspace cannot override it.
func (r *OpenCodeRunner) openCodeConfig(workDir string) ([]byte, error) {
	gatewayURL := strings.TrimRight(r.cfg.InferenceGatewayURL, "/")
	model := openCodeProvider + "/" + r.cfg.Model
	cfg := map[string]any{
		"enabled_providers": []string{openCodeProvider},
		"model":             model,
		"small_model":       model,
		"provider": map[string]any{
			openCodeProvider: map[string]any{
				"name": r.cfg.Brand.ProductName(),
				// Chat completions serves every gateway model; /messages serves only Anthropic's.
				"npm": "@ai-sdk/openai-compatible",
				"options": map[string]string{
					"baseURL": fmt.Sprintf("%s/run/%s/v1", gatewayURL, r.cfg.RunID),
					// Placeholder: the identity-proxy sidecar replaces the Authorization header.
					"apiKey": "orbit-managed",
				},
				"models": map[string]any{r.cfg.Model: map[string]any{}},
			},
		},
		"share":      "disabled",
		"autoupdate": false,
		// Both fetch from this container, outside the sandbox's egress policy.
		"permission": map[string]string{"webfetch": "deny", "websearch": "deny"},
		"mcp":        r.mcpConfig(),
	}
	if r.cfg.SystemPrompt != "" {
		// In the run's config dir, which the agent can read but not change.
		path := filepath.Join(runfs.ConfigDir(workDir), "instructions.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(r.cfg.SystemPrompt), 0o600); err != nil {
			return nil, err
		}
		cfg["instructions"] = []string{path}
	}
	// "shell" is the binary OpenCode's shell tool runs commands with. Pointing it
	// at orbit-shell routes them to the run's sandbox.
	if r.cfg.ShellPath != "" {
		cfg["shell"] = r.cfg.ShellPath
	}
	return json.Marshal(cfg)
}

// mcpConfig is the "mcp" block of the OpenCode config: the runtime server the agent
// provisions its sandbox through, plus one remote entry per MCP server it holds
// tool grants on.
//
// Without the runtime server an OpenCode run cannot ask for a sandbox; without
// the remote entries its mcp_tools grants do nothing.
func (r *OpenCodeRunner) mcpConfig() map[string]any {
	mcp := map[string]any{
		"runtime": map[string]any{
			"type":    "local",
			"command": []string{"orbit-runtime-mcp"},
			"enabled": true,
			"environment": map[string]string{
				"ORBIT_SESSION_MANAGER_URL": r.cfg.SessionManagerURL,
				"ORBIT_RUN_ID":              r.cfg.RunID,
			},
		},
	}
	// The agent calls the gateway unauthenticated; the identity-proxy sidecar
	// injects the platform token on the way out (docs/adr/2026-06-29-mcp-tool-gateway.md).
	if url := strings.TrimRight(r.cfg.ToolGatewayURL, "/"); url != "" {
		for _, server := range r.cfg.MCPServers {
			mcp[server] = map[string]any{
				"type":    "remote",
				"url":     url + "/v1/mcp/" + server,
				"enabled": true,
			}
		}
	}
	return mcp
}

// sendRPCRequest sends a JSON-RPC 2.0 request, registers a pending channel,
// and blocks until the response arrives or ctx expires (30-second deadline).
func (r *OpenCodeRunner) sendRPCRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	id := r.nextID.Add(1)
	ch := make(chan openCodeMsg, 1)
	r.pendingMu.Lock()
	r.pending[id] = ch
	r.pendingMu.Unlock()

	msg := openCodeMsg{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  mustMarshal(params),
	}
	r.stdinMu.Lock()
	writeErr := r.writeJSONL(msg)
	r.stdinMu.Unlock()

	if writeErr != nil {
		r.pendingMu.Lock()
		delete(r.pending, id)
		r.pendingMu.Unlock()
		return nil, fmt.Errorf("write rpc request %q: %w", method, writeErr)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("opencode rpc %q error: %s", method, resp.Error)
		}
		return resp.Result, nil
	case <-reqCtx.Done():
		r.pendingMu.Lock()
		delete(r.pending, id)
		r.pendingMu.Unlock()
		return nil, fmt.Errorf("opencode rpc %q timeout: %w", method, reqCtx.Err())
	}
}

// writeJSONL marshals msg and writes it as a newline-delimited JSON line to stdin.
// Caller must hold stdinMu.
func (r *OpenCodeRunner) writeJSONL(msg any) error {
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal jsonl: %w", err)
	}
	_, err = r.stdin.Write(append(line, '\n'))
	return err
}

// readLoop reads the OpenCode ACP subprocess's stdout and dispatches each message.
// Runs in its own goroutine; closes r.events when stdout is exhausted.
func (r *OpenCodeRunner) readLoop(ctx context.Context, stdout io.Reader) {
	defer close(r.events)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg openCodeMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			r.cfg.Logger.Warn(ctx, "OpenCode: malformed JSONL line",
				logging.Field{Key: "run_id", Value: r.cfg.RunID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			continue
		}

		switch {
		case msg.ID != nil && msg.Method != "":
			// Server→client request (e.g. permission approval).
			r.handleServerRequest(ctx, msg)
		case msg.ID != nil && msg.Method == "":
			// Response to one of our requests.
			r.dispatchResponse(ctx, msg)
		default:
			// Unsolicited notification.
			r.handleNotification(ctx, msg)
		}
	}
}

// handleServerRequest processes a request sent by the OpenCode server.
// Currently handles session/request_permission by always approving.
func (r *OpenCodeRunner) handleServerRequest(ctx context.Context, req openCodeMsg) {
	switch req.Method {
	case "session/request_permission":
		resp := openCodeMsg{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshal(map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": "always"}}),
		}
		r.stdinMu.Lock()
		if err := r.writeJSONL(resp); err != nil {
			r.cfg.Logger.Warn(ctx, "OpenCode: failed to write permission response",
				logging.Field{Key: "run_id", Value: r.cfg.RunID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		r.stdinMu.Unlock()
	default:
		r.cfg.Logger.Warn(ctx, "OpenCode: unknown server request",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "method", Value: req.Method},
		)
	}
}

// dispatchResponse routes a response either to a pending handshake channel or
// handles it as a turn-complete signal for session/prompt.
func (r *OpenCodeRunner) dispatchResponse(ctx context.Context, resp openCodeMsg) {
	r.pendingMu.Lock()
	ch, isHandshake := r.pending[*resp.ID]
	isPrompt := r.pendingPrompts[*resp.ID]
	delete(r.pendingPrompts, *resp.ID)
	if isHandshake {
		delete(r.pending, *resp.ID)
	}
	r.pendingMu.Unlock()

	switch {
	case isHandshake:
		ch <- resp
	case isPrompt:
		r.pendingMu.Lock()
		stillRunning := len(r.pendingPrompts) > 0
		r.pendingMu.Unlock()
		r.endTurn(resp, stillRunning)

		// Log usage if present.
		var result struct {
			Usage struct {
				InputTokens  int `json:"inputTokens"`
				OutputTokens int `json:"outputTokens"`
			} `json:"usage"`
		}
		if resp.Result != nil {
			_ = json.Unmarshal(resp.Result, &result)
		}
		r.cfg.Logger.Info(ctx, "OpenCode turn completed",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "input_tokens", Value: result.Usage.InputTokens},
			logging.Field{Key: "output_tokens", Value: result.Usage.OutputTokens},
		)
	}
}

// endTurn records the end of a prompt. An error fails the turn and a
// canceled prompt is a user stop; either way the process keeps its session,
// so the turn ends with message_stop and the run idles.
//
// A message sent mid-turn is a second prompt, and OpenCode answers the two in
// either order. The turn ends when the last prompt in flight answers; an
// earlier answer only records its error, if it has one.
func (r *OpenCodeRunner) endTurn(resp openCodeMsg, stillRunning bool) {
	var result struct {
		StopReason string `json:"stopReason"`
	}
	if resp.Result != nil {
		_ = json.Unmarshal(resp.Result, &result)
	}
	if stillRunning {
		if resp.Error != nil {
			r.emit(EventTypeRunError, RunErrorPayload{Code: "runner_error", Message: string(resp.Error)})
		}
		return
	}
	switch {
	case resp.Error != nil:
		r.flushPartial()
		r.emit(EventTypeRunError, RunErrorPayload{Code: "runner_error", Message: string(resp.Error)})
	case result.StopReason == "cancelled":
		r.flushPartial()
		r.stopRunningCalls()
		r.emit(EventTypeTurnStopped, TurnStoppedPayload{Reason: "user"})
	default:
		r.emit(EventTypeMessageComplete, MessageCompletePayload{Text: r.textBuf.String(), Thinking: r.thinkingBuf.String()})
		r.textBuf.Reset()
		r.thinkingBuf.Reset()
	}
	r.emit(EventTypeMessageStop, nil)
}

// flushPartial emits the text a cut-short turn had produced, marked interrupted.
func (r *OpenCodeRunner) flushPartial() {
	if r.textBuf.Len() > 0 || r.thinkingBuf.Len() > 0 {
		r.emit(EventTypeMessageComplete, MessageCompletePayload{
			Text:        r.textBuf.String(),
			Thinking:    r.thinkingBuf.String(),
			Interrupted: true,
		})
	}
	r.textBuf.Reset()
	r.thinkingBuf.Reset()
}

// stopRunningCalls forgets calls the cancel cut off. OpenCode sends no final
// update for them, so they would otherwise wait for a result forever.
func (r *OpenCodeRunner) stopRunningCalls() {
	for id, c := range r.toolCalls {
		if c.recorded && !c.done {
			delete(r.toolCalls, id)
		}
	}
}

// handleNotification processes session/update and other notifications.
func (r *OpenCodeRunner) handleNotification(ctx context.Context, msg openCodeMsg) {
	switch msg.Method {
	case "session/update":
		r.handleSessionUpdate(ctx, msg.Params)
	default:
		// Other notifications (e.g. session/created) — ignore.
	}
}

// handleSessionUpdate dispatches a session/update notification's update payload.
func (r *OpenCodeRunner) handleSessionUpdate(_ context.Context, params json.RawMessage) {
	var envelope struct {
		Update struct {
			SessionUpdate string          `json:"sessionUpdate"`
			Content       json.RawMessage `json:"content"`
			ToolCallID    string          `json:"toolCallId"`
			Title         string          `json:"title"`
			Status        string          `json:"status"`
			RawInput      json.RawMessage `json:"rawInput"`
			RawOutput     json.RawMessage `json:"rawOutput"`
		} `json:"update"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return
	}
	u := envelope.Update

	switch u.SessionUpdate {
	case "agent_message_chunk":
		var content struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(u.Content, &content); err != nil {
			return
		}
		r.textBuf.WriteString(content.Text)
		r.emit(EventTypeTextDelta, TextDeltaPayload{Delta: content.Text})

	case "agent_thought_chunk":
		var content struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(u.Content, &content); err != nil {
			return
		}
		r.thinkingBuf.WriteString(content.Text)
		r.emit(EventTypeThinkingDelta, ThinkingDeltaPayload{Delta: content.Text})

	case "tool_call", "tool_call_update":
		r.handleToolCall(u.ToolCallID, u.Title, u.Status, u.RawInput, u.RawOutput, u.Content)

	case "usage_update":
		// No kora event — usage is captured from the session/prompt response.

	default:
		// Ignore: user_message_chunk, plan, config_option_update, etc.
	}
}

// openCodeToolCall tracks one ACP tool call across its updates.
type openCodeToolCall struct {
	name       string // the pending update's title, which is the tool's name
	recorded   bool   // tool_use emitted
	done       bool   // tool_result emitted
	startedAt  time.Time
	progressAt time.Time
}

// openCodeProgressEvery spaces tool_progress events; OpenCode sends an update
// for every chunk of streamed output.
const openCodeProgressEvery = 5 * time.Second

// handleToolCall follows a tool call through its ACP updates. The pending
// update names the tool but carries no arguments yet, so tool_use is recorded
// at the first later update, whose rawInput is the tool's real input. OpenCode
// does not forward a subagent's own calls (the task tool runs a child session
// whose updates never reach this connection), so there is no parent to carry.
func (r *OpenCodeRunner) handleToolCall(id, title, status string, rawInput, rawOutput, content json.RawMessage) {
	c, ok := r.toolCalls[id]
	if !ok {
		c = &openCodeToolCall{name: title, startedAt: time.Now()}
		r.toolCalls[id] = c
	}
	if status == "pending" || c.done {
		return
	}
	if !c.recorded {
		c.recorded, c.progressAt = true, time.Now()
		if r.textBuf.Len() > 0 {
			// Text written before a call is kept in order, as the Claude runner does.
			r.emit(EventTypeTextBlock, TextBlockPayload{Content: r.textBuf.String()})
			r.textBuf.Reset()
		}
		if len(rawInput) == 0 {
			rawInput = json.RawMessage(`{}`)
		}
		r.emit(EventTypeToolUse, ToolUsePayload{ID: id, Name: c.name, Input: rawInput})
	}
	switch status {
	case "completed", "failed":
		c.done = true
		out, isError := openCodeResult(rawOutput, content)
		r.emit(EventTypeToolResult, newToolResult(id, out, isError || status == "failed", ""))
	default:
		if time.Since(c.progressAt) >= openCodeProgressEvery {
			c.progressAt = time.Now()
			r.emit(EventTypeToolProgress, ToolProgressPayload{
				ToolUseID:      id,
				ToolName:       c.name,
				ElapsedSeconds: int(time.Since(c.startedAt).Seconds()),
			})
		}
	}
}

// openCodeResult is a tool's output as text, and whether it failed. OpenCode's
// rawOutput carries the output and, for shell commands, the exit code; the
// ACP content blocks are the fallback.
func openCodeResult(rawOutput, content json.RawMessage) (string, bool) {
	var out struct {
		Output   *string `json:"output"`
		Metadata struct {
			Exit *int `json:"exit"`
		} `json:"metadata"`
	}
	if json.Unmarshal(rawOutput, &out) == nil && out.Output != nil {
		if out.Metadata.Exit != nil && *out.Metadata.Exit != 0 {
			return fmt.Sprintf("Exit code %d\n%s", *out.Metadata.Exit, *out.Output), true
		}
		return *out.Output, false
	}
	var blocks []struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(content, &blocks) == nil && len(blocks) > 0 {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			parts = append(parts, b.Content.Text)
		}
		return strings.Join(parts, "\n"), false
	}
	return unquoteRawOutput(rawOutput), false
}

// emit sends a kora event to the events channel. Nothing is emitted during
// session/load replay: the replayed history was recorded when it happened, and
// re-emitting it would duplicate every call in the run's events.
func (r *OpenCodeRunner) emit(t EventType, payload any) {
	if r.replaying.Load() {
		return
	}
	if payload == nil {
		r.events <- Event{Type: t}
		return
	}
	ev, err := newEvent(t, payload)
	if err != nil {
		return
	}
	r.events <- ev
}

// unquoteRawOutput converts a json.RawMessage tool output to a plain string.
// OpenCode sends rawOutput as a JSON-encoded value: a JSON string like "\"hello\""
// would become hello, and a structured JSON object is returned as its JSON text.
func unquoteRawOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// TakeText hands over the text streamed so far; see Runner.
func (r *OpenCodeRunner) TakeText() string { return r.textBuf.take() }
