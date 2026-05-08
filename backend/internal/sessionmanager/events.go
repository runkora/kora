package sessionmanager

import "encoding/json"

// EventType identifies the kind of Orbit standard event.
type EventType string

const (
	EventTypeTextDelta     EventType = "text_delta"
	EventTypeThinkingDelta EventType = "thinking_delta"
	EventTypeToolUse       EventType = "tool_use"
	EventTypeToolResult    EventType = "tool_result"
	EventTypeMessageStop   EventType = "message_stop"
)

// Event is an Orbit standard event emitted by the transform layer.
// The Payload field is marshaled to the appropriate payload struct
// based on Type.
type Event struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// TextDeltaPayload carries a streaming text chunk.
type TextDeltaPayload struct {
	Delta string `json:"delta"`
}

// ThinkingDeltaPayload carries a streaming thinking (reasoning) chunk.
type ThinkingDeltaPayload struct {
	Delta string `json:"delta"`
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
