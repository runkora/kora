package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
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

	// ExtraEnv is appended to the subprocess environment (e.g. ANTHROPIC_BASE_URL).
	ExtraEnv []string

	// Bare enables --bare mode (no CLAUDE.md discovery, no OAuth/keychain, no
	// auto-memory). Defaults to true — containers are the standard runtime.
	// Local development overrides to false via config/local.yaml to use OAuth.
	Bare bool
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
	if len(r.cfg.AllowedTools) > 0 {
		args = append(args, "--tools", strings.Join(r.cfg.AllowedTools, ","))
	} else {
		args = append(args, "--tools", "")
	}

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

	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("claude start: %w", err)
	}

	go r.transform(stdout)
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

// transform reads Claude Code's stream-json stdout and emits Orbit events.
// Runs in its own goroutine; closes r.events when stdout is exhausted.
func (r *ClaudeRunner) transform(stdout io.Reader) {
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
		r.dispatch(wrapper)
	}
}

// dispatch routes a parsed output wrapper to the appropriate handler.
func (r *ClaudeRunner) dispatch(w claudeOutputWrapper) {
	switch w.Type {
	case "stream_event":
		var ev claudeStreamEvent
		if err := json.Unmarshal(w.Event, &ev); err == nil {
			r.handleStreamEvent(ev)
		}
	case "assistant":
		r.handleAssistant(w.Message)
	case "user":
		r.handleUser(w.Message)
	case "result":
		if w.Subtype == "success" && w.StopReason == "end_turn" {
			r.emit(EventTypeMessageComplete, MessageCompletePayload{
				Text:     r.textBuf.String(),
				Thinking: r.thinkingBuf.String(),
			})
			r.textBuf.Reset()
			r.thinkingBuf.Reset()
		}
	}
}

// handleStreamEvent accumulates text and thinking deltas in memory.
// Deltas are not emitted individually; the turn's accumulated content is
// emitted as a single message_complete event when the result arrives.
func (r *ClaudeRunner) handleStreamEvent(ev claudeStreamEvent) {
	if ev.Type != "content_block_delta" || ev.Delta == nil {
		return
	}
	switch ev.Delta.DeltaType {
	case "text_delta":
		r.textBuf.WriteString(ev.Delta.Text)
	case "thinking_delta":
		r.thinkingBuf.WriteString(ev.Delta.Thinking)
		// input_json_delta: tool input arrives complete in the assistant message snapshot.
	}
}

// handleAssistant emits tool_use events from a completed assistant message.
func (r *ClaudeRunner) handleAssistant(msg *claudeMessage) {
	if msg == nil {
		return
	}
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
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
