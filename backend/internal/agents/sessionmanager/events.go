package sessionmanager

import (
	"encoding/json"
	"strings"
)

// EventType identifies the kind of kora standard event.
type EventType string

const (
	// Persistence events — recorded in run_events by orbit-agents.
	EventTypeMessageComplete EventType = "message_complete"
	EventTypeTextBlock       EventType = "text_block"
	EventTypeToolUse         EventType = "tool_use"
	EventTypeToolResult      EventType = "tool_result"
	EventTypeRunError        EventType = "run_error"
	EventTypeAPIRetry        EventType = "api_retry"
	EventTypeSandboxStatus   EventType = "sandbox_status"
	EventTypeTurnStopped     EventType = "turn_stopped"
	// EventTypeGuardrailWarning records that the loop guard warned the agent
	// (docs/adr/2026-09-29-agent-run-guardrails.md).
	EventTypeGuardrailWarning EventType = "guardrail_warning"

	// Streaming-only events — emitted via HTTPNotifier and fanned out over SSE.
	// Not written to run_events (docs/adr/2026-05-11-agent-event-streaming-protocol.md); consumed by orbit-agents SSE subscribers.
	EventTypeTextDelta     EventType = "text_delta"
	EventTypeThinkingDelta EventType = "thinking_delta"
	EventTypeMessageStop   EventType = "message_stop"
	EventTypeToolProgress  EventType = "tool_progress"
)

// Event is a kora standard event emitted by the transform layer. No sequence
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
//
// Interrupted marks a turn that did not finish (the user stopped it, or it ended
// in an error), so no consumer reads the partial text as a finished answer.
type MessageCompletePayload struct {
	Text        string `json:"text"`
	Thinking    string `json:"thinking,omitempty"`
	Interrupted bool   `json:"interrupted,omitempty"`
}

// TextBlockPayload carries a segment of assistant text that preceded a tool call.
// Persisted to run_events so replay can reconstruct the original interleaved order
// of text and tool events. Not pushed to live SSE subscribers — live clients
// already receive the same content incrementally via text_delta events.
type TextBlockPayload struct {
	Content string `json:"content"`
}

// ToolUsePayload carries a completed tool call (id, name, and full input).
// ParentToolUseID names the Agent call a subagent's call belongs to.
type ToolUsePayload struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Input           json.RawMessage `json:"input"`
	ParentToolUseID string          `json:"parent_tool_use_id,omitempty"`
}

// ToolResultPayload carries the result of a tool invocation. Content is always
// text: array results are flattened so every consumer reads one shape. Content
// past maxToolResultBytes is cut; Truncated and OriginalBytes say by how much.
type ToolResultPayload struct {
	ToolUseID       string `json:"tool_use_id"`
	Content         string `json:"content"`
	IsError         bool   `json:"is_error"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
	Truncated       bool   `json:"truncated,omitempty"`
	OriginalBytes   int    `json:"original_bytes,omitempty"`
}

// maxToolResultBytes caps a recorded tool result. Subagent and MCP results run
// past a megabyte, and the event log is a record for people, not a data store.
const maxToolResultBytes = 64 * 1024

// newToolResult builds a tool_result, cutting content past maxToolResultBytes on
// a UTF-8 boundary. Every runner records results through it.
func newToolResult(toolUseID, content string, isError bool, parentToolUseID string) ToolResultPayload {
	p := ToolResultPayload{ToolUseID: toolUseID, Content: content, IsError: isError, ParentToolUseID: parentToolUseID}
	if len(content) > maxToolResultBytes {
		p.Content = strings.ToValidUTF8(content[:maxToolResultBytes], "")
		p.Truncated = true
		p.OriginalBytes = len(content)
	}
	return p
}

// APIRetryPayload records a model provider error the agent runtime is retrying.
// Error is the runtime's error class (e.g. authentication_failed, rate_limit).
type APIRetryPayload struct {
	Attempt      int    `json:"attempt"`
	MaxRetries   int    `json:"max_retries"`
	RetryDelayMs int    `json:"retry_delay_ms"`
	ErrorStatus  *int   `json:"error_status"`
	Error        string `json:"error"`
}

// ToolProgressPayload is a heartbeat for a tool that is still running.
// Streaming-only: it carries nothing the later tool_result does not.
type ToolProgressPayload struct {
	ToolUseID       string `json:"tool_use_id"`
	ToolName        string `json:"tool_name"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
	ElapsedSeconds  int    `json:"elapsed_seconds"`
}

// Sandbox provisioning phases reported in SandboxStatusPayload.
const (
	SandboxPhaseProvisioning  = "provisioning"
	SandboxPhasePodReady      = "pod_ready"
	SandboxPhaseEgressReady   = "egress_ready"
	SandboxPhaseEgressSkipped = "egress_skipped"
	SandboxPhaseFailed        = "failed"
)

// SandboxStatusPayload reports progress of a fresh sandbox provision, so the
// minutes spent scheduling the pod and settling egress are visible.
// egress_skipped means the sandbox was handed over without a passed egress
// check; Message says why.
type SandboxStatusPayload struct {
	Phase     string `json:"phase"`
	ElapsedMs int64  `json:"elapsed_ms"`
	Message   string `json:"message,omitempty"`
}

// TurnStoppedPayload marks a stopped turn: Reason is "user", or "guardrail" for a
// loop-guard pause. Any partial text precedes it as a message_complete.
type TurnStoppedPayload struct {
	Reason string `json:"reason"`
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
