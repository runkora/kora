package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// OpenCodeRunnerConfig holds settings for the OpenCode ACP subprocess.
type OpenCodeRunnerConfig struct {
	// BinaryPath is the path to the opencode binary. Defaults to "opencode".
	BinaryPath string

	// WrapperPath is the path to orbit-session-runner. When set, opencode is
	// invoked as: WrapperPath <workDir> <BinaryPath> [args...] so Landlock and a
	// PID namespace are applied first. Empty invokes opencode directly, with no
	// filesystem restriction (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
	WrapperPath string

	// ShellPath is written into opencode.json as the "shell" key, so tool
	// commands run through orbit-shell in the run's sandbox rather than in this
	// container. Empty leaves OpenCode on the container's own shell.
	ShellPath string

	// ExtraEnv is appended to the subprocess environment (WORKSPACE and the
	// orbit-shell callback variables).
	ExtraEnv []string

	// Model is stored for future use. OpenCode model selection is configured
	// via opencode.json provider options.
	Model string

	// InferenceGatewayURL is written directly into opencode.json so OpenCode
	// routes all LLM calls through the Orbit inference gateway. Must be non-empty.
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
	cfg             OpenCodeRunnerConfig
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	stdinMu         sync.Mutex // guards all stdin writes (Send + permission responses)
	sessionID       string
	events          chan Event
	once            sync.Once
	textBuf         strings.Builder
	thinkingBuf     strings.Builder
	pendingMu       sync.Mutex
	pending         map[int64]chan openCodeMsg // in-flight handshake requests
	pendingPromptID int64                      // ID of the in-flight session/prompt; 0 if none
	nextID          atomic.Int64
	seenToolUse     map[string]bool // deduplicates tool_use events per toolCallId; never cleared — assumes OpenCode generates unique IDs per tool call for the lifetime of a session
	replaying       atomic.Bool     // true while session/load replay is in progress; suppresses ephemeral events so the channel does not fill before drain() starts
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
		events:      make(chan Event, 256),
		pending:     make(map[int64]chan openCodeMsg),
		seenToolUse: make(map[string]bool),
	}
}

// Start validates config, writes opencode.json, spawns the subprocess, and
// performs the initialize + session/new handshake synchronously.
func (r *OpenCodeRunner) Start(ctx context.Context, workDir string) error {
	if r.cfg.InferenceGatewayURL == "" {
		return fmt.Errorf("opencode: InferenceGatewayURL must not be empty")
	}

	if err := r.writeOpenCodeConfig(workDir); err != nil {
		return fmt.Errorf("opencode: write config: %w", err)
	}

	bin, args := wrapExec(r.cfg.WrapperPath, workDir, r.cfg.BinaryPath, "acp", "--cwd", workDir)
	r.cmd = exec.CommandContext(ctx, bin, args...)
	r.cmd.Dir = workDir
	r.cmd.Env = append(os.Environ(), r.cfg.ExtraEnv...)

	var err error
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
	r.pendingPromptID = id
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

// Events returns the channel of Orbit standard events.
func (r *OpenCodeRunner) Events() <-chan Event {
	return r.events
}

// Interrupt sends SIGINT to the OpenCode subprocess to abort the in-progress
// prompt. ACP also defines a `cancel` notification, but SIGINT matches the
// cross-runner convention; the session layer then calls NewResumeRunner to
// restart with session/load.
func (r *OpenCodeRunner) Interrupt() error {
	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	return r.cmd.Process.Signal(os.Interrupt)
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

// writeOpenCodeConfig writes opencode.json to workDir with provider baseURLs
// pointing to the Orbit inference gateway, and the shell OpenCode runs tool
// commands through.
func (r *OpenCodeRunner) writeOpenCodeConfig(workDir string) error {
	gatewayURL := strings.TrimRight(r.cfg.InferenceGatewayURL, "/")
	perRunURL := fmt.Sprintf("%s/run/%s", gatewayURL, r.cfg.RunID)
	cfg := map[string]any{
		"provider": map[string]any{
			"anthropic": map[string]any{
				"options": map[string]string{"baseURL": perRunURL},
			},
			"openai": map[string]any{
				"options": map[string]string{"baseURL": perRunURL},
			},
		},
	}
	// OpenCode's "shell" key takes the binary its shell tool runs commands with,
	// so pointing it at orbit-shell puts tool execution in the run's sandbox
	// (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
	if r.cfg.ShellPath != "" {
		cfg["shell"] = r.cfg.ShellPath
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "opencode.json"), data, 0o600)
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
	isPrompt := (r.pendingPromptID == *resp.ID)
	if isPrompt {
		r.pendingPromptID = 0
	}
	if isHandshake {
		delete(r.pending, *resp.ID)
	}
	r.pendingMu.Unlock()

	switch {
	case isHandshake:
		ch <- resp
	case isPrompt:
		// JSON-RPC error response from session/prompt — surface as a run failure
		// and close the channel so the registry marks the run as failed.
		if resp.Error != nil {
			r.textBuf.Reset()
			r.thinkingBuf.Reset()
			r.emit(EventTypeRunError, RunErrorPayload{
				Code:    "runner_error",
				Message: string(resp.Error),
			})
			r.stdin.Close() //nolint:errcheck
			return
		}

		// Turn complete — emit accumulated text and stop signal.
		text := r.textBuf.String()
		thinking := r.thinkingBuf.String()
		r.textBuf.Reset()
		r.thinkingBuf.Reset()

		r.emit(EventTypeMessageComplete, MessageCompletePayload{Text: text, Thinking: thinking})
		r.emit(EventTypeMessageStop, nil)

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

	case "tool_call":
		switch u.Status {
		case "pending", "in_progress":
			if r.seenToolUse[u.ToolCallID] {
				return
			}
			r.seenToolUse[u.ToolCallID] = true
			r.emit(EventTypeToolUse, ToolUsePayload{
				ID:    u.ToolCallID,
				Name:  u.Title,
				Input: u.RawInput,
			})
		case "completed":
			if !r.seenToolUse[u.ToolCallID] {
				r.seenToolUse[u.ToolCallID] = true
				r.emit(EventTypeToolUse, ToolUsePayload{
					ID:    u.ToolCallID,
					Name:  u.Title,
					Input: u.RawInput,
				})
			}
			r.emit(EventTypeToolResult, ToolResultPayload{
				ToolUseID: u.ToolCallID,
				Content:   unquoteRawOutput(u.RawOutput),
			})
		case "failed":
			if !r.seenToolUse[u.ToolCallID] {
				r.seenToolUse[u.ToolCallID] = true
				r.emit(EventTypeToolUse, ToolUsePayload{
					ID:    u.ToolCallID,
					Name:  u.Title,
					Input: u.RawInput,
				})
			}
			r.emit(EventTypeToolResult, ToolResultPayload{
				ToolUseID: u.ToolCallID,
				Content:   unquoteRawOutput(u.RawOutput),
				IsError:   true,
			})
		}

	case "usage_update":
		// No Orbit event — usage is captured from the session/prompt response.

	default:
		// Ignore: user_message_chunk, plan, config_option_update, etc.
	}
}

// emit sends an Orbit event to the events channel.
// During session/load replay (r.replaying == true) ephemeral events are
// dropped: they cannot be consumed until drain() starts, and replay history
// can be arbitrarily large. Durable events (tool_use, tool_result) are always
// emitted so the resumed session's event log is complete.
func (r *OpenCodeRunner) emit(t EventType, payload any) {
	if r.replaying.Load() && !isDurableEvent(t) {
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
