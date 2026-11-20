// Package grading turns a run's events into a Grader state and a Grader's
// answers into verdicts. It does no I/O (docs/adr/2026-11-19-graders.md).
package grading

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"unicode/utf8"
)

// Event is one recorded run event, as read from run_events.
type Event struct {
	Seq     int64
	Type    string
	Payload json.RawMessage
}

// stateEventTypes are the event types a Grader reads. text_block is kept
// because it holds the agent's text before a tool call, which message_complete omits.
var stateEventTypes = map[string]bool{
	"human_message":     true,
	"trigger_message":   true,
	"text_block":        true,
	"tool_use":          true,
	"tool_result":       true,
	"message_complete":  true,
	"run_state":         true,
	"run_error":         true,
	"turn_stopped":      true,
	"guardrail_warning": true,
}

// DefaultWindow is the decision model's input window in tokens, measured on Jev 1.14.
const DefaultWindow = 247_000

// Cap is the most characters a tool_result body keeps in the state.
type Cap int

// Uncapped means every tool_result body is kept whole.
const Uncapped Cap = -1

// capSteps are tried in order after the uncapped state. 0 drops every body.
var capSteps = []Cap{4000, 2000, 1000, 500, 0}

// Snapshot is the run's uncapped state. It is stored once per run, and every
// grade of the run fits it to the window.
type Snapshot struct {
	JSON []byte
	// Hash is the hex sha256 of JSON.
	Hash string
}

// State is a snapshot fitted to the window. Cap is the step used.
type State struct {
	JSON   []byte
	Tokens int
	Cap    Cap
	// TooLarge is set when the state does not fit with no result bodies.
	// JSON then holds the state with no bodies.
	TooLarge bool
}

// ErrBadPayload reports an event payload or snapshot that is not valid JSON.
var ErrBadPayload = errors.New("grading: bad event payload")

// EstimateTokens is the fit estimate: bytes divided by 4. It is deterministic
// so the same run always gets the same cap.
func EstimateTokens(b []byte) int { return len(b) / 4 }

// BuildSnapshot keeps the event types a Grader reads, in sequence order.
func BuildSnapshot(events []Event) (Snapshot, error) {
	kept := make([]Event, 0, len(events))
	for _, e := range events {
		if stateEventTypes[e.Type] {
			kept = append(kept, e)
		}
	}
	slices.SortStableFunc(kept, func(a, b Event) int { return cmp.Compare(a.Seq, b.Seq) })
	b, err := serialize(kept, Uncapped)
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256(b)
	return Snapshot{JSON: b, Hash: hex.EncodeToString(sum[:])}, nil
}

// Fit caps a snapshot's tool_result bodies at the first step that fits window
// tokens. The caller passes the window net of the prompt and questions, which
// this package does not see.
func Fit(snapshot []byte, window int) (State, error) {
	var items []stateEvent
	if err := json.Unmarshal(snapshot, &items); err != nil {
		return State{}, fmt.Errorf("%w: snapshot: %w", ErrBadPayload, err)
	}
	events := make([]Event, len(items))
	for i, it := range items {
		events[i] = Event(it)
	}
	var s State
	for _, c := range append([]Cap{Uncapped}, capSteps...) {
		b, err := serialize(events, c)
		if err != nil {
			return State{}, err
		}
		s = State{JSON: b, Tokens: EstimateTokens(b), Cap: c}
		if s.Tokens <= window {
			break
		}
	}
	s.TooLarge = s.Tokens > window
	return s, nil
}

type stateEvent struct {
	Seq     int64           `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// serialize writes the events as one JSON array.
func serialize(events []Event, c Cap) ([]byte, error) {
	out := make([]stateEvent, len(events))
	for i, e := range events {
		p := e.Payload
		if e.Type == "tool_result" && c != Uncapped {
			var err error
			if p, err = capResult(p, c); err != nil {
				return nil, fmt.Errorf("event %d: %w", e.Seq, err)
			}
		}
		if len(p) > 0 && !json.Valid(p) {
			return nil, fmt.Errorf("event %d: %w", e.Seq, ErrBadPayload)
		}
		out[i] = stateEvent{Seq: e.Seq, Type: e.Type, Payload: p}
	}
	return encode(out)
}

// capResult cuts a tool_result's content to c characters. A cut body gets
// content_capped and content_chars, its length before the cut. Other fields,
// including the 64 KiB truncation marker, are kept.
func capResult(payload json.RawMessage, c Cap) (json.RawMessage, error) {
	// An empty payload has no body to cut. The uncapped state keeps it the same way.
	if len(payload) == 0 {
		return payload, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadPayload, err)
	}
	var content string
	if raw, ok := fields["content"]; ok {
		if err := json.Unmarshal(raw, &content); err != nil {
			return nil, fmt.Errorf("%w: content: %w", ErrBadPayload, err)
		}
	}
	n := utf8.RuneCountInString(content)
	if n <= int(c) {
		return payload, nil
	}
	cut, err := encode(firstRunes(content, int(c)))
	if err != nil {
		return nil, err
	}
	fields["content"] = cut
	fields["content_capped"] = json.RawMessage("true")
	fields["content_chars"] = json.RawMessage(strconv.Itoa(n))
	return encode(fields)
}

// firstRunes is the prefix of s holding its first n characters.
func firstRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// encode is json.Marshal without HTML escaping. Escaping inflates the size
// estimate, and the model reads the text as written.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode state: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
