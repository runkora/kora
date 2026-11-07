package sessionmanager

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// acpProgressEvery spaces tool_progress events; adapters send an update for
// every chunk of streamed output.
const (
	acpStatusCompleted = "completed"
	acpStatusFailed    = "failed"
)

const acpProgressEvery = 5 * time.Second

type acpToolCall struct {
	name       string
	parent     string
	recorded   bool // tool_use emitted
	done       bool // tool_result emitted
	startedAt  time.Time
	progressAt time.Time
}

// acpTranslator turns ACP session updates and prompt responses into kora
// events. Everything but the text buffer is used from the read loop only.
type acpTranslator struct {
	text      turnText
	thinking  strings.Builder
	tools     map[string]*acpToolCall
	emitFn    func(EventType, any)
	replaying atomic.Bool // session/load replay: nothing is emitted
	now       func() time.Time
	classify  func(stopReason string, rpcErr *acpRPCError) string
}

func newACPTranslator(emit func(EventType, any), classify func(string, *acpRPCError) string) *acpTranslator {
	return &acpTranslator{tools: make(map[string]*acpToolCall), emitFn: emit, now: time.Now, classify: classify}
}

func (t *acpTranslator) emit(typ EventType, payload any) {
	if t.replaying.Load() {
		return // replayed history was recorded when it happened
	}
	t.emitFn(typ, payload)
}

// reset drops buffered text, thinking and tool state (after a replay).
func (t *acpTranslator) reset() {
	t.text.Reset()
	t.thinking.Reset()
	t.tools = make(map[string]*acpToolCall)
}

type acpUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       json.RawMessage `json:"content"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Status        string          `json:"status"`
	RawInput      json.RawMessage `json:"rawInput"`
	RawOutput     json.RawMessage `json:"rawOutput"`
	Meta          struct {
		ClaudeCode struct {
			ToolName        string `json:"toolName"`
			ParentToolUseID string `json:"parentToolUseId"`
		} `json:"claudeCode"`
		ParentToolUseID string `json:"parentToolUseId"`
	} `json:"_meta"`
}

// handleUpdate translates one session/update notification's params.
func (t *acpTranslator) handleUpdate(params json.RawMessage) {
	var env struct {
		Update acpUpdate `json:"update"`
	}
	if json.Unmarshal(params, &env) != nil {
		return
	}
	u := env.Update
	switch u.SessionUpdate {
	case "agent_message_chunk":
		if s, ok := chunkText(u.Content); ok {
			t.text.WriteString(s)
			t.emit(EventTypeTextDelta, TextDeltaPayload{Delta: s})
		}
	case "agent_thought_chunk":
		if s, ok := chunkText(u.Content); ok {
			t.thinking.WriteString(s)
			t.emit(EventTypeThinkingDelta, ThinkingDeltaPayload{Delta: s})
		}
	case "tool_call", "tool_call_update":
		t.handleToolCall(u)
	default:
		// plan, usage_update, user_message_chunk, config updates: no kora event.
	}
}

func chunkText(content json.RawMessage) (string, bool) {
	var c struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &c) != nil {
		return "", false
	}
	return c.Text, true
}

// handleToolCall follows a tool call through its updates. The pending update
// only names the tool; tool_use is recorded at the first later update, whose
// rawInput is the real input.
func (t *acpTranslator) handleToolCall(u acpUpdate) {
	c, ok := t.tools[u.ToolCallID]
	if !ok {
		name := u.Title
		if n := u.Meta.ClaudeCode.ToolName; n != "" {
			name = n
		}
		parent := u.Meta.ClaudeCode.ParentToolUseID
		if parent == "" {
			parent = u.Meta.ParentToolUseID
		}
		c = &acpToolCall{name: name, parent: parent, startedAt: t.now()}
		t.tools[u.ToolCallID] = c
	}
	if u.Status == "pending" || c.done {
		return
	}
	if !c.recorded {
		c.recorded, c.progressAt = true, t.now()
		if t.text.Len() > 0 {
			t.emit(EventTypeTextBlock, TextBlockPayload{Content: t.text.String()})
			t.text.Reset()
		}
		in := u.RawInput
		if len(in) == 0 {
			in = json.RawMessage(`{}`)
		}
		t.emit(EventTypeToolUse, ToolUsePayload{ID: u.ToolCallID, Name: c.name, Input: in, ParentToolUseID: c.parent})
	}
	switch u.Status {
	case acpStatusCompleted, acpStatusFailed:
		c.done = true
		out, isErr := acpToolOutput(u.RawOutput, u.Content)
		t.emit(EventTypeToolResult, newToolResult(u.ToolCallID, out, isErr || u.Status == acpStatusFailed, c.parent))
	default:
		if t.now().Sub(c.progressAt) >= acpProgressEvery {
			c.progressAt = t.now()
			t.emit(EventTypeToolProgress, ToolProgressPayload{
				ToolUseID:       u.ToolCallID,
				ToolName:        c.name,
				ParentToolUseID: c.parent,
				ElapsedSeconds:  int(t.now().Sub(c.startedAt).Seconds()),
			})
		}
	}
}

// acpToolOutput is a tool's output as text, and whether it failed. ACP content
// blocks come first; rawOutput (with a shell exit code when it has one) is the
// fallback.
func acpToolOutput(rawOutput, content json.RawMessage) (string, bool) {
	var blocks []struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(content, &blocks) == nil && len(blocks) > 0 {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if b.Content.Text != "" {
				parts = append(parts, b.Content.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n"), false
		}
	}
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
	return unquoteRawOutput(rawOutput), false
}

// flushPartial emits the text of a cut-short turn, marked interrupted.
func (t *acpTranslator) flushPartial() {
	if t.text.Len() > 0 || t.thinking.Len() > 0 {
		t.emit(EventTypeMessageComplete, MessageCompletePayload{Text: t.text.String(), Thinking: t.thinking.String(), Interrupted: true})
	}
	t.text.Reset()
	t.thinking.Reset()
}

// forgetRunningCalls drops calls a cancel or error cut off: no final update comes.
func (t *acpTranslator) forgetRunningCalls() {
	for id, c := range t.tools {
		if c.recorded && !c.done {
			delete(t.tools, id)
		}
	}
}

// endTurn translates a session/prompt response.
func (t *acpTranslator) endTurn(stopReason string, rpcErr *acpRPCError) {
	switch {
	case rpcErr != nil || stopReason == "max_tokens" || stopReason == "max_turn_requests" || stopReason == "refusal":
		t.flushPartial()
		t.forgetRunningCalls()
		code := acpDefaultErrorCode
		if t.classify != nil {
			if c := t.classify(stopReason, rpcErr); c != "" {
				code = c
			}
		}
		msg := "agent stopped: " + stopReason
		if rpcErr != nil {
			msg = rpcErr.Error()
		}
		t.emit(EventTypeRunError, RunErrorPayload{Code: code, Message: msg})
	case stopReason == "cancelled":
		t.flushPartial()
		t.forgetRunningCalls()
		t.emit(EventTypeTurnStopped, TurnStoppedPayload{Reason: "user"})
	default:
		t.emit(EventTypeMessageComplete, MessageCompletePayload{Text: t.text.String(), Thinking: t.thinking.String()})
		t.text.Reset()
		t.thinking.Reset()
	}
	t.emit(EventTypeMessageStop, nil)
}
