package sessionmanager

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// acpProgressEvery spaces tool_progress events; adapters send an update for
// every chunk of streamed input or output.
const acpProgressEvery = 5 * time.Second

// acpMaxTurnMessages bounds the adapter messages kept for ClassifyError; the
// terminal error is among the latest.
const acpMaxTurnMessages = 64

type acpToolCall struct {
	name       string
	title      string
	parent     string
	input      any
	recorded   bool // tool_use emitted
	done       bool // tool_result emitted
	startedAt  time.Time
	progressAt time.Time
}

// acpTranslator turns ACP updates and prompt responses into kora events. The
// SDK's notification goroutine, the prompt goroutine and Start all use it, so
// its state is guarded by mu.
type acpTranslator struct {
	mu        sync.Mutex
	text      turnText
	thinking  strings.Builder
	tools     map[string]*acpToolCall
	messages  []acpAdapterMessage
	replaying bool
	emitFn    func(EventType, any)
	classify  func(acpTurnEnd) string
	now       func() time.Time
}

func newACPTranslator(emit func(EventType, any), classify func(acpTurnEnd) string) *acpTranslator {
	return &acpTranslator{tools: make(map[string]*acpToolCall), emitFn: emit, classify: classify, now: time.Now}
}

// emit drops events while a session/load replays history already recorded.
func (t *acpTranslator) emit(typ EventType, payload any) {
	if !t.replaying {
		t.emitFn(typ, payload)
	}
}

func (t *acpTranslator) setReplaying(on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.replaying = on
	if !on {
		t.text.Reset()
		t.thinking.Reset()
		t.tools = make(map[string]*acpToolCall)
		t.messages = nil
	}
}

// adapterMessage keeps msg for the turn's ClassifyError and emits its retry.
func (t *acpTranslator) adapterMessage(msg acpAdapterMessage, retry *APIRetryPayload) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages = append(t.messages, msg)
	if len(t.messages) > acpMaxTurnMessages {
		t.messages = t.messages[len(t.messages)-acpMaxTurnMessages:]
	}
	if retry != nil {
		t.emit(EventTypeAPIRetry, *retry)
	}
}

func (t *acpTranslator) handleUpdate(u acp.SessionUpdate) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case u.AgentMessageChunk != nil:
		if c := u.AgentMessageChunk.Content.Text; c != nil {
			t.text.WriteString(c.Text)
			t.emit(EventTypeTextDelta, TextDeltaPayload{Delta: c.Text})
		}
	case u.AgentThoughtChunk != nil:
		if c := u.AgentThoughtChunk.Content.Text; c != nil {
			t.thinking.WriteString(c.Text)
			t.emit(EventTypeThinkingDelta, ThinkingDeltaPayload{Delta: c.Text})
		}
	case u.ToolCall != nil:
		c := u.ToolCall
		status := c.Status
		if status == "" {
			status = acp.ToolCallStatusPending // the spec's default for a new call
		}
		t.toolCall(acp.ToolCallUpdate{ToolCallId: c.ToolCallId, Title: &c.Title, Status: &status, RawInput: c.RawInput, RawOutput: c.RawOutput, Content: c.Content, Meta: c.Meta})
	case u.ToolCallUpdate != nil:
		c := u.ToolCallUpdate
		t.toolCall(acp.ToolCallUpdate{ToolCallId: c.ToolCallId, Title: c.Title, Status: c.Status, RawInput: c.RawInput, RawOutput: c.RawOutput, Content: c.Content, Meta: c.Meta})
	}
}

// toolCall follows a tool call through its updates. Name, title and rawInput
// carry forward, since adapters stream the input over several updates after the
// pending one, and tool_use waits for the first update past pending that has an
// input or a running status.
func (t *acpTranslator) toolCall(u acp.ToolCallUpdate) {
	id := string(u.ToolCallId)
	c, ok := t.tools[id]
	if !ok {
		c = &acpToolCall{startedAt: t.now()}
		t.tools[id] = c
	}
	c.carry(u)
	var status acp.ToolCallStatus
	if u.Status != nil {
		status = *u.Status
	}
	if c.done || status == acp.ToolCallStatusPending {
		return
	}
	terminal := status == acp.ToolCallStatusCompleted || status == acp.ToolCallStatusFailed
	if !c.recorded {
		if !terminal && status != acp.ToolCallStatusInProgress && c.input == nil {
			return
		}
		t.recordToolUse(id, c)
	}
	switch {
	case terminal:
		c.done = true
		out, exitErr := acpToolOutput(u.Content, u.RawOutput)
		t.emit(EventTypeToolResult, newToolResult(id, out, exitErr || status == acp.ToolCallStatusFailed, c.parent))
	case t.now().Sub(c.progressAt) >= acpProgressEvery:
		c.progressAt = t.now()
		t.emit(EventTypeToolProgress, ToolProgressPayload{
			ToolUseID:       id,
			ToolName:        c.toolName(),
			ParentToolUseID: c.parent,
			ElapsedSeconds:  int(t.now().Sub(c.startedAt).Seconds()),
		})
	}
}

// carry keeps the first title, the name, the parent and the latest non-empty
// input of a call. The first title names the tool; OpenCode retitles a shell
// call with its command once the input arrives.
func (c *acpToolCall) carry(u acp.ToolCallUpdate) {
	if u.Title != nil && c.title == "" {
		c.title = *u.Title
	}
	claude, _ := u.Meta["claudeCode"].(map[string]any)
	if n, _ := claude["toolName"].(string); n != "" {
		c.name = n
	}
	if p, _ := claude["parentToolUseId"].(string); p != "" {
		c.parent = p
	}
	if hasACPInput(u.RawInput) {
		c.input = u.RawInput
	}
}

func (t *acpTranslator) recordToolUse(id string, c *acpToolCall) {
	c.recorded, c.progressAt = true, t.now()
	if t.text.Len() > 0 {
		t.emit(EventTypeTextBlock, TextBlockPayload{Content: t.text.String()})
		t.text.Reset()
	}
	input := json.RawMessage(`{}`)
	if c.input != nil {
		if b, err := json.Marshal(c.input); err == nil {
			input = b
		}
	}
	t.emit(EventTypeToolUse, ToolUsePayload{ID: id, Name: c.toolName(), Input: input, ParentToolUseID: c.parent})
}

func (c *acpToolCall) toolName() string {
	if c.name != "" {
		return c.name
	}
	return c.title
}

// hasACPInput is false for a missing or empty rawInput, which Claude sends on
// the pending update before the real input streams in.
func hasACPInput(v any) bool {
	if m, ok := v.(map[string]any); ok {
		return len(m) > 0
	}
	return v != nil
}

// acpToolOutput is a tool's output as text, and whether a shell command exited
// non-zero. OpenCode's rawOutput wins over content blocks, as in the legacy runner.
func acpToolOutput(content []acp.ToolCallContent, raw any) (string, bool) {
	if out, failed, ok := openCodeToolOutput(raw); ok {
		return out, failed
	}
	var parts []string
	for _, c := range content {
		if c.Content != nil && c.Content.Content.Text != nil {
			parts = append(parts, c.Content.Content.Text.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n"), false
	}
	switch v := raw.(type) {
	case nil:
		return "", false
	case string:
		return v, false
	case []any:
		for _, e := range v {
			if m, ok := e.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n"), false
		}
	}
	b, _ := json.Marshal(raw)
	return string(b), false
}

// openCodeToolOutput reads OpenCode's rawOutput {output, metadata: {exit}}; ok
// is false for any other shape.
func openCodeToolOutput(raw any) (out string, failed, ok bool) {
	m, _ := raw.(map[string]any)
	out, ok = m["output"].(string)
	if !ok {
		return "", false, false
	}
	meta, _ := m["metadata"].(map[string]any)
	if exit, isNum := meta["exit"].(float64); isNum && exit != 0 {
		return fmt.Sprintf("Exit code %d\n%s", int(exit), out), true, true
	}
	return out, false, true
}

// flushPartial emits the text of a cut-short turn, marked interrupted.
func (t *acpTranslator) flushPartial() {
	if t.text.Len() > 0 || t.thinking.Len() > 0 {
		t.emit(EventTypeMessageComplete, MessageCompletePayload{Text: t.text.String(), Thinking: t.thinking.String(), Interrupted: true})
	}
	t.text.Reset()
	t.thinking.Reset()
}

// pruneTools drops finished calls at the end of a turn. A turn cut short
// closes its running calls too, so a late update for one emits nothing.
func (t *acpTranslator) pruneTools(cutShort bool) {
	for id, c := range t.tools {
		switch {
		case c.done:
			delete(t.tools, id)
		case cutShort:
			c.done = true
		}
	}
}

// endTurn translates a session/prompt response.
func (t *acpTranslator) endTurn(stop acp.StopReason, rpcErr *acp.RequestError) {
	t.mu.Lock()
	defer t.mu.Unlock()
	turn := acpTurnEnd{StopReason: stop, RPCErr: rpcErr, Messages: t.messages}
	t.messages = nil
	code := t.classify(turn)
	switch {
	case rpcErr == nil && stop == acp.StopReasonCancelled:
		t.flushPartial()
		t.pruneTools(true)
		t.emit(EventTypeTurnStopped, TurnStoppedPayload{Reason: "user"})
	case rpcErr != nil || code != "" || stop != acp.StopReasonEndTurn:
		t.flushPartial()
		t.pruneTools(true)
		if code == "" {
			code = acpDefaultErrorCode
		}
		msg := "agent stopped: " + string(stop)
		if rpcErr != nil {
			msg = fmt.Sprintf("agent error %d: %s", rpcErr.Code, rpcErr.Message)
		}
		t.emit(EventTypeRunError, RunErrorPayload{Code: code, Message: msg})
	default:
		t.emit(EventTypeMessageComplete, MessageCompletePayload{Text: t.text.String(), Thinking: t.thinking.String()})
		t.text.Reset()
		t.thinking.Reset()
		t.pruneTools(false)
	}
	t.emit(EventTypeMessageStop, nil)
}
