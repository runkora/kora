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

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ClaudeRunnerConfig holds settings for the Claude Code subprocess.
type ClaudeRunnerConfig struct {
	// BinaryPath is the path to the claude binary. Defaults to "claude".
	BinaryPath string

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

	// Logger is used to emit structured log events from the subprocess lifecycle.
	Logger logging.Logger
}

// ClaudeRunner implements Runner for Claude Code's --input-format stream-json mode.
//
// The subprocess is started with:
//
//	claude --print --output-format stream-json --input-format stream-json
//	       --verbose --include-partial-messages --dangerously-skip-permissions
//	       --no-session-persistence --bare [--model M] [--tools T,...]
//
// Each user message is sent as a JSON line on stdin. Stdout is parsed line by
// line: text and thinking deltas are accumulated in memory; one message_complete
// event is emitted per turn containing the full accumulated text. tool_use and
// tool_result events are emitted as they arrive.
type ClaudeRunner struct {
	cfg         ClaudeRunnerConfig
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	events      chan Event
	once        sync.Once
	textBuf     strings.Builder // accumulates text_delta chunks; reset per turn
	thinkingBuf strings.Builder // accumulates thinking_delta chunks; reset per turn
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
func (r *ClaudeRunner) Start(ctx context.Context, workDir string) error {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--dangerously-skip-permissions",
		"--no-session-persistence",
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

	mcpConfigPath, mcpErr := r.writeMCPConfig(workDir)
	if mcpErr != nil {
		return fmt.Errorf("write mcp config: %w", mcpErr)
	}
	args = append(args, "--mcp-config", mcpConfigPath)

	r.cmd = exec.CommandContext(ctx, r.cfg.BinaryPath, args...)
	r.cmd.Dir = workDir
	r.cmd.Env = append(r.cmd.Environ(), r.cfg.ExtraEnv...)

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
		// Carries startup messages and tool initialisation info from Claude Code.
		r.cfg.Logger.Info(ctx, "Claude system event",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "subtype", Value: w.Subtype},
		)
	case "result":
		if w.Subtype == "success" {
			textLen := r.textBuf.Len()
			r.cfg.Logger.Info(ctx, "Claude turn complete",
				logging.Field{Key: "run_id", Value: r.cfg.RunID},
				logging.Field{Key: "stop_reason", Value: w.StopReason},
				logging.Field{Key: "text_len", Value: textLen},
				logging.Field{Key: "thinking_len", Value: r.thinkingBuf.Len()},
			)
			// An empty turn with a non-standard stop reason signals a silent failure
			// (e.g. inference gateway returning 5xx after exhausted retries).
			if textLen == 0 && w.StopReason != "end_turn" {
				msg := w.Result
				if msg == "" {
					msg = fmt.Sprintf("Agent produced an empty response (stop_reason=%q) — the inference gateway may have returned an error", w.StopReason)
				}
				r.cfg.Logger.Error(ctx, "Claude empty turn — possible inference error",
					logging.Field{Key: "run_id", Value: r.cfg.RunID},
					logging.Field{Key: "stop_reason", Value: w.StopReason},
					logging.Field{Key: "result", Value: msg},
				)
				r.emit(EventTypeRunError, RunErrorPayload{
					Code:    "empty_response",
					Message: msg,
				})
				return
			}
			r.emit(EventTypeMessageComplete, MessageCompletePayload{
				Text:     r.textBuf.String(),
				Thinking: r.thinkingBuf.String(),
			})
			r.textBuf.Reset()
			r.thinkingBuf.Reset()
			r.emit(EventTypeMessageStop, nil)
		} else {
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
			r.emit(EventTypeRunError, RunErrorPayload{
				Code:    "claude_error",
				Message: msg,
			})
		}
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

// handleStreamEvent accumulates text and thinking deltas for message_complete
// and also emits them immediately as push-only streaming events (ADR-0054).
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
func (r *ClaudeRunner) handleAssistant(ctx context.Context, msg *claudeMessage) {
	if msg == nil {
		return
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

// scopesToClaudeTools converts Orbit tool scope names to Claude Code built-in
// tool names for the --tools flag. Unrecognized scopes are ignored.
// lsp, monitoring, scheduling, and meta tools are always included regardless
// of the configured scopes. An empty selectable result still gets these auto tools.
// AskUserQuestion is intentionally excluded: Claude Code auto-rejects it in
// headless stream-json mode; use a custom MCP tool for human-in-the-loop instead.
func scopesToClaudeTools(scopes []string) string {
	scopeMap := map[string][]string{
		// Selectable scopes — operator configures these per agent type.
		"shell":      {"Bash"},
		"filesystem": {"Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "LS"},
		"web":        {"WebFetch", "WebSearch"},
		"agents":     {"Agent", "SendMessage"},
		"tasks":      {"TaskCreate", "TaskGet", "TaskList", "TaskStop", "TaskUpdate", "TodoWrite"},
		"mcp":        {"ListMcpResourcesTool", "ReadMcpResourceTool"},
		// Auto scopes — always injected, not shown in the UI.
		"lsp":        {"LSP"},
		"monitoring": {"Monitor"},
		"scheduling": {"CronCreate", "CronDelete", "CronList"},
		"meta":       {"ToolSearch", "Skill"},
	}
	// Always include auto scopes so the full scope list is selectable + auto.
	autoScopes := []string{"lsp", "monitoring", "scheduling", "meta"}
	all := make([]string, len(scopes)+len(autoScopes))
	copy(all, scopes)
	copy(all[len(scopes):], autoScopes)

	seen := map[string]bool{}
	var tools []string
	for _, scope := range all {
		for _, t := range scopeMap[scope] {
			if !seen[t] {
				seen[t] = true
				tools = append(tools, t)
			}
		}
	}
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
	Result     string          `json:"result"`  // error message when Subtype == "error"
	Event      json.RawMessage `json:"event"`   // when Type == "stream_event"
	Message    *claudeMessage  `json:"message"` // when Type == "assistant" | "user"
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
