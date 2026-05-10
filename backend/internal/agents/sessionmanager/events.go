package sessionmanager

import "encoding/json"

// EventType identifies the kind of Orbit standard event.
type EventType string

const (
	// Persistence events — written to run_events by the session-manager.
	EventTypeMessageComplete EventType = "message_complete"
	EventTypeToolUse         EventType = "tool_use"
	EventTypeToolResult      EventType = "tool_result"

	// Streaming-only events — emitted via HTTPNotifier and fanned out over SSE.
	// Not written to run_events (ADR-0054); consumed by orbit-agents SSE subscribers.
	EventTypeTextDelta     EventType = "text_delta"
	EventTypeThinkingDelta EventType = "thinking_delta"
	EventTypeMessageStop   EventType = "message_stop"
)

// Event is an Orbit standard event emitted by the transform layer.
type Event struct {
	Type       EventType       `json:"type"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	SequenceNo int64           `json:"sequence_no,omitempty"` // set for durable events after DB write
}

// MessageCompletePayload carries the full accumulated text and thinking for one
// completed assistant turn. Written once per turn, replacing the N text_delta
// and thinking_delta events that would otherwise be stored individually.
type MessageCompletePayload struct {
	Text     string `json:"text"`
	Thinking string `json:"thinking,omitempty"`
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

// TextDeltaPayload carries one chunk of streamed assistant text.
// Push-only — never persisted to run_events (ADR-0054).
type TextDeltaPayload struct {
	Delta string `json:"delta"`
}

// ThinkingDeltaPayload carries one chunk of streamed assistant reasoning.
// Push-only — never persisted to run_events (ADR-0054).
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
