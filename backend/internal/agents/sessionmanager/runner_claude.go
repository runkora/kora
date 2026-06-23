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

	"github.com/clear-route/orbit/internal/agents/toolscopes"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ClaudeRunnerConfig holds settings for the Claude Code subprocess.
type ClaudeRunnerConfig struct {
	// BinaryPath is the path to the claude binary. Defaults to "claude".
	BinaryPath string

	// WrapperPath is the path to orbit-session-runner. When set, the subprocess
	// is invoked as: WrapperPath <workDir> <BinaryPath> [args...]
	// which applies Landlock filesystem restrictions before exec'ing claude.
	// When empty, BinaryPath is invoked directly (no filesystem restriction).
	WrapperPath string

	// Model overrides the default model (e.g. "claude-sonnet-4-6").
	// Empty means use Claude Code's default.
	Model string

	// AllowedTools is the set of Claude Code tool names the agent may use
	// (e.g. ["Bash", "Edit", "Read"]). Derived from the agent type's tool_scopes.
	AllowedTools []string

	// SystemPrompt is appended to Claude's default system prompt via
	// --append-system-prompt. Empty means no additional prompt is injected.
	SystemPrompt string

	// ExtraEnv is appended to the subprocess environment (e.g. ANTHROPIC_BASE_URL).
	ExtraEnv []string

	// Bare enables --bare mode (no CLAUDE.md discovery, no OAuth/keychain, no
	// auto-memory). Defaults to true — containers are the standard runtime.
	// Local development overrides to false via config/local.yaml to use OAuth.
	Bare bool

	// SessionManagerURL is the base URL of this session-manager, passed to
	// orbit-runtime-mcp so it can call back for provision_sandbox requests.
	// Always http://localhost:{port} since they run in the same pod.
	SessionManagerURL string

	// RunID is the run identifier for this session, passed to orbit-runtime-mcp.
	RunID string

	// ResumeSessionID, when non-empty, passes --resume <id> to Claude Code so
	// it continues from a previous session rather than starting fresh.
	ResumeSessionID string

	// Logger is used to emit structured log events from the subprocess lifecycle.
	Logger logging.Logger
}

// ClaudeRunner implements Runner for Claude Code's --input-format stream-json mode.
//
// The subprocess is started with:
//
//	claude --print --output-format stream-json --input-format stream-json
//	       --verbose --include-partial-messages --dangerously-skip-permissions
//	       [--resume ID] [--bare] [--model M] [--tools T,...]
//
// Each user message is sent as a JSON line on stdin. Stdout is parsed line by
// line: text and thinking deltas are accumulated in memory; one message_complete
// event is emitted per turn containing the full accumulated text. tool_use and
// tool_result events are emitted as they arrive.
type ClaudeRunner struct {
	cfg               ClaudeRunnerConfig
	cmd               *exec.Cmd
	stdin             io.WriteCloser
	events            chan Event
	once              sync.Once
	textBuf           strings.Builder // accumulates text_delta chunks; reset per turn
	thinkingBuf       strings.Builder // accumulates thinking_delta chunks; reset per turn
	textBlockEmitted  bool            // true if ≥1 text_block was emitted this turn
	budgetHit         bool            // true if a BUDGET_EXCEEDED rejection was seen this turn; reset per turn
	capturedSessionID string          // set once from the system/init event
	sessionIDMu       sync.Mutex      // protects capturedSessionID
	interrupted       atomic.Bool     // true after Interrupt() — suppresses error events
}

// NewClaudeRunner creates a runner with the given configuration.
func NewClaudeRunner(cfg ClaudeRunnerConfig) *ClaudeRunner {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "claude"
	}
	return &ClaudeRunner{
		cfg:    cfg,
		events: make(chan Event, 64),
	}
}

// Start spawns the Claude Code subprocess. workDir is the run's workspace directory.
// buildFlags returns the Claude Code CLI flags for this runner configuration.
// Extracted to keep Start() cyclomatic complexity within bounds.
func (r *ClaudeRunner) buildFlags() []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--dangerously-skip-permissions",
	}
	if r.cfg.ResumeSessionID != "" {
		args = append(args, "--resume", r.cfg.ResumeSessionID)
	}
	if r.cfg.Bare {
		args = append(args, "--bare")
	}
	if r.cfg.Model != "" {
		args = append(args, "--model", r.cfg.Model)
	}
	args = append(args, "--tools", scopesToClaudeTools(r.cfg.AllowedTools))
	if r.cfg.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", r.cfg.SystemPrompt)
	}
	return args
}

// buildExecCommand returns the command and arguments to use when starting the
// subprocess. When WrapperPath is set the wrapper is invoked as:
//
//	orbit-session-runner <workDir> <claude> [claude-args...]
//
// so it can apply Landlock before exec'ing claude. Without a wrapper, claude
// is invoked directly (used in tests and local macOS dev).
func (r *ClaudeRunner) buildExecCommand(workDir string, args []string) (cmd string, cmdArgs []string) {
	claudeBinary := r.cfg.BinaryPath
	if claudeBinary == "" {
		claudeBinary = "claude"
	}
	if r.cfg.WrapperPath != "" {
		return r.cfg.WrapperPath, append([]string{workDir, claudeBinary}, args...)
	}
	return claudeBinary, args
}

// buildRunEnv constructs the subprocess environment from the inherited base env
// plus ExtraEnv, with HOME and TMPDIR stripped from both sources then replaced
// by the per-run isolation values. Any key set in ExtraEnv is also stripped
// from the inherited env. Stripping is necessary because on Linux getenv()
// returns the first match — a pre-existing HOME=, TMPDIR=, or e.g.
// ANTHROPIC_BASE_URL= would shadow the per-run values if it appeared earlier
// in the slice. See docs/adr/2026-06-04-per-session-home-isolation.md.
func (r *ClaudeRunner) buildRunEnv(baseEnv []string, runHome, tmpDir string) []string {
	overridden := map[string]bool{"HOME": true, "TMPDIR": true}
	for _, e := range r.cfg.ExtraEnv {
		if k, _, ok := strings.Cut(e, "="); ok {
			overridden[k] = true
		}
	}

	env := make([]string, 0, len(baseEnv)+len(r.cfg.ExtraEnv)+2)
	for _, e := range baseEnv {
		if k, _, ok := strings.Cut(e, "="); ok && overridden[k] {
			continue
		}
		env = append(env, e)
	}
	for _, e := range r.cfg.ExtraEnv {
		if !strings.HasPrefix(e, "HOME=") && !strings.HasPrefix(e, "TMPDIR=") {
			env = append(env, e)
		}
	}
	return append(env, "HOME="+runHome, "TMPDIR="+tmpDir)
}

// isBudgetExceededMessage recognizes the inference gateway's 429
// BUDGET_EXCEEDED rejection surfaced through the agent runtime's error result.
func isBudgetExceededMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "budget_exceeded") || strings.Contains(lower, "budget exceeded")
}

// Start spawns the Claude Code subprocess. workDir is the run's workspace directory.
func (r *ClaudeRunner) Start(ctx context.Context, workDir string) error {
	args := r.buildFlags()

	mcpConfigPath, mcpErr := r.writeMCPConfig(workDir)
	if mcpErr != nil {
		return fmt.Errorf("write mcp config: %w", mcpErr)
	}
	args = append(args, "--mcp-config", mcpConfigPath)

	// Per-run home: isolates ~/.claude/ (session transcripts, memories) from
	// sibling runs on the same pod. See docs/adr/2026-06-04-per-session-home-isolation.md.
	runHome := filepath.Join(workDir, ".runhome")
	if err := os.MkdirAll(runHome, 0o700); err != nil {
		return fmt.Errorf("create run home: %w", err)
	}

	// Per-run tmp directory lives inside the workspace so it is covered by the
	// same Landlock rule as the rest of the run's files. See docs/adr/2026-06-04-per-session-home-isolation.md.
	tmpDir := filepath.Join(workDir, ".tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return fmt.Errorf("create run tmpdir: %w", err)
	}

	cmd, cmdArgs := r.buildExecCommand(workDir, args)
	r.cmd = exec.CommandContext(ctx, cmd, cmdArgs...)
	r.cmd.Dir = workDir
	r.cmd.Env = r.buildRunEnv(r.cmd.Environ(), runHome, tmpDir)

	var err error
	r.stdin, err = r.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("claude stdin pipe: %w", err)
	}

	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("claude stdout pipe: %w", err)
	}

	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("claude stderr pipe: %w", err)
	}

	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("claude start: %w", err)
	}

	r.cfg.Logger.Info(ctx, "Claude subprocess started",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "pid", Value: r.cmd.Process.Pid},
		logging.Field{Key: "model", Value: r.cfg.Model},
		logging.Field{Key: "bare", Value: r.cfg.Bare},
		logging.Field{Key: "tool_count", Value: len(r.cfg.AllowedTools)},
	)

	go r.logStderr(ctx, stderr)
	go r.transform(ctx, stdout)
	return nil
}

// Send writes a user text message to Claude Code's stdin.
func (r *ClaudeRunner) Send(_ context.Context, text string) error {
	return r.writeInput(claudeInputMessage{
		Type: "user",
		Message: claudeInputMessageBody{
			Role:    "user",
			Content: []claudeInputContent{{Type: "text", Text: text}},
		},
	})
}

// SendToolResult injects a tool result for a pending AskFollowupQuestion (or any
// other human-in-the-loop tool). Claude Code resumes processing once received.
func (r *ClaudeRunner) SendToolResult(_ context.Context, toolUseID, content string) error {
	return r.writeInput(claudeInputMessage{
		Type: "user",
		Message: claudeInputMessageBody{
			Role: "user",
			Content: []claudeInputContent{{
				Type:      "tool_result",
				ToolUseID: toolUseID,
				Content:   content,
			}},
		},
	})
}

// writeMCPConfig writes the per-session MCP server config to the workspace and
// returns the path. Claude Code reads this via --mcp-config at startup.
func (r *ClaudeRunner) writeMCPConfig(workDir string) (string, error) {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"runtime": map[string]any{
				"type":    "stdio",
				"command": "orbit-runtime-mcp",
				"args":    []string{},
				"env": map[string]string{
					"ORBIT_SESSION_MANAGER_URL": r.cfg.SessionManagerURL,
					"ORBIT_RUN_ID":              r.cfg.RunID,
				},
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	path := filepath.Join(workDir, ".mcp-config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (r *ClaudeRunner) writeInput(msg claudeInputMessage) error {
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal input: %w", err)
	}
	_, err = r.stdin.Write(append(line, '\n'))
	return err
}

// Events returns the channel of Orbit standard events.
func (r *ClaudeRunner) Events() <-chan Event {
	return r.events
}

// Interrupt sends SIGINT to the Claude subprocess, interrupting the current
// in-progress turn. Claude Code handles SIGINT by stopping the active inference
// and returning to its input loop, leaving the subprocess alive for the next message.
func (r *ClaudeRunner) Interrupt() error {
	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	r.interrupted.Store(true)
	return r.cmd.Process.Signal(os.Interrupt)
}

// SessionID returns the Claude session ID captured from the system/init event.
func (r *ClaudeRunner) SessionID() string {
	r.sessionIDMu.Lock()
	defer r.sessionIDMu.Unlock()
	return r.capturedSessionID
}

// NewResumeRunner returns a new runner configured to resume this session.
// Returns nil if no session ID has been captured yet.
func (r *ClaudeRunner) NewResumeRunner() Runner {
	id := r.SessionID()
	if id == "" {
		return nil
	}
	cfg := r.cfg
	cfg.ResumeSessionID = id
	return NewClaudeRunner(cfg)
}

// Close sends EOF to stdin and waits for the subprocess to exit.
func (r *ClaudeRunner) Close() error {
	var firstErr error
	r.once.Do(func() {
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

// logStderr reads Claude's stderr and logs each line as an error.
func (r *ClaudeRunner) logStderr(ctx context.Context, stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		r.cfg.Logger.Error(ctx, "Claude subprocess stderr",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "line", Value: line},
		)
	}
	if err := scanner.Err(); err != nil {
		r.cfg.Logger.Error(ctx, "Claude stderr scan error",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// transform reads Claude Code's stream-json stdout and emits Orbit events.
// Runs in its own goroutine; closes r.events when stdout is exhausted.
func (r *ClaudeRunner) transform(ctx context.Context, stdout io.Reader) {
	defer close(r.events)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var wrapper claudeOutputWrapper
		if err := json.Unmarshal(line, &wrapper); err != nil {
			continue
		}
		// Defense-in-depth: if a runtime ever retries the gateway's 403
		// BUDGET_EXCEEDED (or surfaces it through a system event), sniff the
		// raw line — handleResult uses the flag to classify the truncated
		// turn as budget_exhausted instead of letting the session idle or
		// fail. The primary detection path is the error result: 403 is
		// non-retryable, so the runtime fails the turn immediately with the
		// budget message in w.Result.
		if wrapper.Type == "system" && isBudgetExceededMessage(string(line)) {
			r.budgetHit = true
		}
		r.dispatch(ctx, wrapper)
	}

	if err := scanner.Err(); err != nil {
		r.cfg.Logger.Error(ctx, "Claude stdout scan error",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}

	r.cfg.Logger.Info(ctx, "Claude subprocess exited",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
	)
}

// dispatch routes a parsed output wrapper to the appropriate handler.
func (r *ClaudeRunner) dispatch(ctx context.Context, w claudeOutputWrapper) {
	switch w.Type {
	case "stream_event":
		var ev claudeStreamEvent
		if err := json.Unmarshal(w.Event, &ev); err == nil {
			r.handleStreamEvent(ev)
		}
	case "assistant":
		r.handleAssistant(ctx, w.Message)
	case "user":
		r.handleUser(w.Message)
	case "system":
		if w.Subtype == "init" && w.SessionID != "" {
			r.sessionIDMu.Lock()
			r.capturedSessionID = w.SessionID
			r.sessionIDMu.Unlock()
		}
		r.cfg.Logger.Info(ctx, "Claude system event",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "subtype", Value: w.Subtype},
			logging.Field{Key: "session_id", Value: w.SessionID},
		)
	case "result":
		r.handleResult(ctx, w)
	default:
		if w.Type != "" {
			r.cfg.Logger.Info(ctx, "Unhandled Claude output type",
				logging.Field{Key: "run_id", Value: r.cfg.RunID},
				logging.Field{Key: "type", Value: w.Type},
				logging.Field{Key: "subtype", Value: w.Subtype},
			)
		}
	}
}

// handleResult processes a "result" wrapper (the final event Claude emits per turn).
// A success result flushes accumulated text as message_complete + message_stop.
// A non-success result emits run_error unless the runner was explicitly interrupted,
// in which case the error is suppressed because the runner will restart via --resume.
func (r *ClaudeRunner) handleResult(ctx context.Context, w claudeOutputWrapper) {
	// Turn boundary: consume the budget flag set by the readLoop sniffer and
	// fold in the result text — Claude Code reports a turn killed by the
	// gateway's 403 BUDGET_EXCEEDED as subtype=success with the API error in
	// w.Result ("API Error: 403 budget exceeded …"), so the result text is
	// the primary signal. A turn that finishes naturally (end_turn) must not
	// poison the next turn's classification.
	budgetHit := r.budgetHit || isBudgetExceededMessage(w.Result)
	r.budgetHit = false

	if w.Subtype != "success" {
		if r.interrupted.Load() {
			return // SIGINT caused the non-success exit — runner will restart via --resume
		}
		msg := w.Result
		if msg == "" {
			msg = fmt.Sprintf("Claude exited with subtype=%q stop_reason=%q", w.Subtype, w.StopReason)
		}
		r.cfg.Logger.Error(ctx, "Claude error result",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "subtype", Value: w.Subtype},
			logging.Field{Key: "stop_reason", Value: w.StopReason},
			logging.Field{Key: "result", Value: msg},
		)
		code := "claude_error"
		if budgetHit || isBudgetExceededMessage(msg) {
			// The inference gateway rejected the run's LLM calls with 403
			// BUDGET_EXCEEDED — a budget ceiling is met, not a fault.
			code = "budget_exhausted"
		}
		r.emit(EventTypeRunError, RunErrorPayload{Code: code, Message: msg})
		return
	}

	// Fallback: a runtime that swallowed budget rejections mid-turn and still
	// reported the truncated turn as a success (stop_sequence or similar).
	// Without this check the truncated turn looks complete and the session
	// idles, stranding a run that can no longer make any LLM call.
	if budgetHit && w.StopReason != "end_turn" {
		if r.interrupted.Load() {
			return // SIGINT mid-storm — runner restarts via --resume; next turn reclassifies
		}
		r.emitBudgetExhaustedTurn(ctx, w)
		return
	}

	textLen := r.textBuf.Len()
	r.cfg.Logger.Info(ctx, "Claude turn complete",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "stop_reason", Value: w.StopReason},
		logging.Field{Key: "text_len", Value: textLen},
		logging.Field{Key: "thinking_len", Value: r.thinkingBuf.Len()},
	)
	// An empty turn with a non-standard stop reason signals a silent failure
	// (e.g. inference gateway returning 5xx after exhausted retries).
	// textBlockEmitted=true means text was already flushed before tool calls,
	// so the turn has content even if textBuf is now empty.
	if textLen == 0 && !r.textBlockEmitted && w.StopReason != "end_turn" {
		if r.interrupted.Load() {
			return // SIGINT caused the empty turn — runner will restart via --resume
		}
		msg := w.Result
		if msg == "" {
			msg = fmt.Sprintf("Agent produced an empty response (stop_reason=%q) — the inference gateway may have returned an error", w.StopReason)
		}
		r.cfg.Logger.Error(ctx, "Claude empty turn — possible inference error",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "stop_reason", Value: w.StopReason},
			logging.Field{Key: "result", Value: msg},
		)
		r.emit(EventTypeRunError, RunErrorPayload{Code: "empty_response", Message: msg})
		return
	}
	r.emit(EventTypeMessageComplete, MessageCompletePayload{
		Text:     r.textBuf.String(),
		Thinking: r.thinkingBuf.String(),
	})
	r.textBuf.Reset()
	r.thinkingBuf.Reset()
	r.textBlockEmitted = false
	r.emit(EventTypeMessageStop, nil)
}

// emitBudgetExhaustedTurn ends a budget-truncated turn: flushes any content the
// turn produced before the rejections (so it survives in the transcript), then
// emits run_error budget_exhausted. message_stop is deliberately NOT emitted —
// it would arm the idle timer and the session must tear down instead (the
// drain loop closes it on this run_error and transitions the run).
func (r *ClaudeRunner) emitBudgetExhaustedTurn(ctx context.Context, w claudeOutputWrapper) {
	r.cfg.Logger.Error(ctx, "Claude turn truncated by budget rejection",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "stop_reason", Value: w.StopReason},
		logging.Field{Key: "text_len", Value: r.textBuf.Len()},
	)
	if r.textBuf.Len() > 0 || r.thinkingBuf.Len() > 0 {
		r.emit(EventTypeMessageComplete, MessageCompletePayload{
			Text:     r.textBuf.String(),
			Thinking: r.thinkingBuf.String(),
		})
		r.textBuf.Reset()
		r.thinkingBuf.Reset()
		r.textBlockEmitted = false
	}
	r.emit(EventTypeRunError, RunErrorPayload{
		Code:    "budget_exhausted",
		Message: "The run's LLM calls were rejected with BUDGET_EXCEEDED — a budget ceiling was reached and the turn was cut short after retries.",
	})
}

// handleStreamEvent accumulates text and thinking deltas for message_complete
// and also emits them immediately as push-only streaming events (docs/adr/2026-05-11-agent-event-streaming-protocol.md).
func (r *ClaudeRunner) handleStreamEvent(ev claudeStreamEvent) {
	if ev.Type != "content_block_delta" || ev.Delta == nil {
		return
	}
	switch ev.Delta.DeltaType {
	case "text_delta":
		r.textBuf.WriteString(ev.Delta.Text)
		r.emit(EventTypeTextDelta, TextDeltaPayload{Delta: ev.Delta.Text})
	case "thinking_delta":
		r.thinkingBuf.WriteString(ev.Delta.Thinking)
		r.emit(EventTypeThinkingDelta, ThinkingDeltaPayload{Delta: ev.Delta.Thinking})
		// input_json_delta: tool input arrives complete in the assistant message snapshot.
	}
}

// handleAssistant emits tool_use events from a completed assistant message.
// With --include-partial-messages Claude emits many snapshots of the same message;
// partial snapshots contain only text (no complete tool blocks yet), so we skip
// them. The final snapshot is detected by the presence of a tool_use block.
//
// Before the first tool_use, any accumulated text in textBuf is flushed as a
// durable text_block event. This preserves the original interleaved order during
// replay without re-streaming individual character deltas.
func (r *ClaudeRunner) handleAssistant(ctx context.Context, msg *claudeMessage) {
	if msg == nil {
		return
	}

	// Skip partial snapshots that carry only text — they have no complete tool blocks.
	hasTools := false
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			hasTools = true
			break
		}
	}
	if !hasTools {
		return
	}

	// Flush the text accumulated before these tool calls as a text_block event.
	if r.textBuf.Len() > 0 {
		r.emit(EventTypeTextBlock, TextBlockPayload{Content: r.textBuf.String()})
		r.textBuf.Reset()
		r.textBlockEmitted = true
	}

	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			r.cfg.Logger.Info(ctx, "Tool call",
				logging.Field{Key: "run_id", Value: r.cfg.RunID},
				logging.Field{Key: "tool", Value: block.Name},
				logging.Field{Key: "tool_use_id", Value: block.ID},
			)
			r.emit(EventTypeToolUse, ToolUsePayload{ID: block.ID, Name: block.Name, Input: block.Input})
		}
	}
}

// handleUser emits tool_result events from a user message carrying tool results.
func (r *ClaudeRunner) handleUser(msg *claudeMessage) {
	if msg == nil {
		return
	}
	for _, block := range msg.Content {
		if block.Type == "tool_result" {
			r.emit(EventTypeToolResult, ToolResultPayload{
				ToolUseID: block.ToolUseID,
				Content:   block.Content,
				IsError:   block.IsError,
			})
		}
	}
}

func (r *ClaudeRunner) emit(t EventType, payload any) {
	ev, err := newEvent(t, payload)
	if err != nil {
		return
	}
	r.events <- ev
}

// isExitKilled returns true for process terminations that are normal shutdown.
func isExitKilled(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "signal: killed") ||
		strings.Contains(s, "signal: terminated") ||
		strings.Contains(s, "context canceled")
}

// scopesToClaudeTools converts Orbit tool scope names to the Claude Code --tools
// flag value. The scope→tool mapping (and the always-on auto scopes) live in the
// shared toolscopes package so this and the agents API stay in lockstep.
func scopesToClaudeTools(scopes []string) string {
	tools := toolscopes.ClaudeTools(scopes)
	if len(tools) == 0 {
		return "default"
	}
	return strings.Join(tools, ",")
}

// ── Claude Code JSON wire types ──────────────────────────────────────────────

type claudeInputMessage struct {
	Type    string                 `json:"type"`
	Message claudeInputMessageBody `json:"message"`
}

type claudeInputMessageBody struct {
	Role    string               `json:"role"`
	Content []claudeInputContent `json:"content"`
}

type claudeInputContent struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

// claudeOutputWrapper is the top-level envelope for every stdout line.
type claudeOutputWrapper struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	StopReason string          `json:"stop_reason"`
	Result     string          `json:"result"`     // error message when Subtype == "error"
	SessionID  string          `json:"session_id"` // present on system/init
	Event      json.RawMessage `json:"event"`      // when Type == "stream_event"
	Message    *claudeMessage  `json:"message"`    // when Type == "assistant" | "user"
}

// claudeStreamEvent is the inner event inside a "stream_event" wrapper.
type claudeStreamEvent struct {
	Type  string              `json:"type"`
	Delta *claudeContentDelta `json:"delta"` // content_block_delta
}

type claudeContentDelta struct {
	DeltaType string `json:"type"`
	Text      string `json:"text"`     // text_delta
	Thinking  string `json:"thinking"` // thinking_delta
}

// claudeMessage is the message body inside "assistant" and "user" wrappers.
type claudeMessage struct {
	Content []claudeContentItem `json:"content"`
}

type claudeContentItem struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   string          `json:"content"`
	IsError   bool            `json:"is_error"`
}
