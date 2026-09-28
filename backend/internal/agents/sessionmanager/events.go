package sessionmanager

import "encoding/json"

// EventType identifies the kind of Orbit standard event.
type EventType string

const (
	// Persistence events — recorded in run_events by orbit-agents.
	EventTypeMessageComplete EventType = "message_complete"
	EventTypeTextBlock       EventType = "text_block"
	EventTypeToolUse         EventType = "tool_use"
	EventTypeToolResult      EventType = "tool_result"
	EventTypeRunError        EventType = "run_error"
	// EventTypeGuardrailWarning records that the loop guard warned the agent
	// (docs/adr/2026-09-29-agent-run-guardrails.md).
	EventTypeGuardrailWarning EventType = "guardrail_warning"

	// Streaming-only events — emitted via HTTPNotifier and fanned out over SSE.
	// Not written to run_events (docs/adr/2026-05-11-agent-event-streaming-protocol.md); consumed by orbit-agents SSE subscribers.
	EventTypeTextDelta     EventType = "text_delta"
	EventTypeThinkingDelta EventType = "thinking_delta"
	EventTypeMessageStop   EventType = "message_stop"
)

// Event is an Orbit standard event emitted by the transform layer. No sequence
// number: orbit-agents assigns one when it records the event, and this pod
// reaches no database.
type Event struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// MessageCompletePayload carries the trailing text and thinking for one completed
// assistant turn. Text before tool calls is stored in text_block events; this
// payload carries only the text segment that followed the last tool call (or the
// full text when no tools were used). Also serves as the turn-commit signal.
type MessageCompletePayload struct {
	Text     string `json:"text"`
	Thinking string `json:"thinking,omitempty"`
}

// TextBlockPayload carries a segment of assistant text that preceded a tool call.
// Persisted to run_events so replay can reconstruct the original interleaved order
// of text and tool events. Not pushed to live SSE subscribers — live clients
// already receive the same content incrementally via text_delta events.
type TextBlockPayload struct {
	Content string `json:"content"`
}

// ToolUsePayload carries a completed tool call (id, name, and full input).
type ToolUsePayload struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResultPayload carries the result of a tool invocation.
type ToolResultPayload struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

// RunErrorPayload carries a fatal error that terminated or prevented a run.
// Persisted to run_events so it survives reconnection and page reload.
type RunErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// GuardrailWarningPayload records a loop guard warning: which loop, how many
// calls it had run to, and the tool involved.
type GuardrailWarningPayload struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	Tool  string `json:"tool"`
}

// TextDeltaPayload carries one chunk of streamed assistant text.
// Push-only — never persisted to run_events (docs/adr/2026-05-11-agent-event-streaming-protocol.md).
type TextDeltaPayload struct {
	Delta string `json:"delta"`
}

// ThinkingDeltaPayload carries one chunk of streamed assistant reasoning.
// Push-only — never persisted to run_events (docs/adr/2026-05-11-agent-event-streaming-protocol.md).
type ThinkingDeltaPayload struct {
	Delta string `json:"delta"`
}

// newEvent marshals a typed payload into an Event.
func newEvent(t EventType, payload any) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: t, Payload: raw}, nil
}
