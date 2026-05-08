package sessionmanager

import "encoding/json"

// EventType identifies the kind of Orbit standard event.
type EventType string

const (
	// Persistence events — written to run_events by the session-manager.
	EventTypeMessageComplete EventType = "message_complete"
	EventTypeToolUse         EventType = "tool_use"
	EventTypeToolResult      EventType = "tool_result"

	// Streaming-only events — emitted on the live SSE path once orbit-agents
	// implements a direct streaming subscription to the session-manager.
	// Not written to run_events; kept here so the types are defined ahead of time.
	EventTypeTextDelta     EventType = "text_delta"
	EventTypeThinkingDelta EventType = "thinking_delta"
	EventTypeMessageStop   EventType = "message_stop"
)

// Event is an Orbit standard event emitted by the transform layer.
type Event struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
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

// newEvent marshals a typed payload into an Event.
func newEvent(t EventType, payload any) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: t, Payload: raw}, nil
}
