package agents

import (
	"encoding/json"
	"sync"
	"time"
)

// PushedEvent is the payload received at POST /internal/runs/{id}/events
// from a session-manager pod.
type PushedEvent struct {
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	SequenceNo int64           `json:"sequence_no,omitempty"` // non-zero for durable events; assigned by PublishEvent on the run_events write
	At         time.Time       `json:"-"`                     // when orbit-agents recorded or received it; the SSE envelope's "at"
}

// durableEventTypes are written to run_events so a reconnecting client can
// replay the run in order. Every other pushed type is streamed and then dropped
// (docs/adr/2026-05-11-agent-event-streaming-protocol.md).
//
// The session-manager keeps its own copy to choose a delivery timeout. Only this
// one decides persistence, so drift costs a bad timeout, not a lost row.
var durableEventTypes = map[string]bool{
	"message_complete":  true,
	"text_block":        true,
	"tool_use":          true,
	"tool_result":       true,
	"run_error":         true,
	"api_retry":         true,
	"sandbox_status":    true,
	"turn_stopped":      true,
	"guardrail_warning": true,
}

// replayOnlyEventTypes are persisted for replay but withheld from live
// subscribers, who already have the same content from the delta events.
var replayOnlyEventTypes = map[string]bool{"text_block": true}

func durableEventType(t string) bool { return durableEventTypes[t] }

func replayOnlyEventType(t string) bool { return replayOnlyEventTypes[t] }

// PubSub is the in-memory fan-out hub for live run events.
// Session-managers push events here; SSE clients subscribe per run.
// Ephemeral events (text_delta, thinking_delta) are dropped when no subscriber
// is present — they are never persisted (docs/adr/2026-05-11-agent-event-streaming-protocol.md).
type PubSub struct {
	mu   sync.RWMutex
	subs map[string][]chan PushedEvent
}

// NewPubSub creates an empty hub.
func NewPubSub() *PubSub {
	return &PubSub{subs: make(map[string][]chan PushedEvent)}
}

// Subscribe registers a buffered channel for the given run.
// The caller must drain the channel and call the returned cancel func to
// deregister and close it when done (e.g. on SSE client disconnect).
func (ps *PubSub) Subscribe(runID string) (<-chan PushedEvent, func()) {
	ch := make(chan PushedEvent, 64)
	ps.mu.Lock()
	ps.subs[runID] = append(ps.subs[runID], ch)
	ps.mu.Unlock()

	cancel := func() {
		ps.mu.Lock()
		defer ps.mu.Unlock()
		subs := ps.subs[runID]
		for i, c := range subs {
			if c == ch {
				ps.subs[runID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(ps.subs[runID]) == 0 {
			delete(ps.subs, runID)
		}
		close(ch)
	}
	return ch, cancel
}

// Publish fans an event out to all current subscribers for the run.
// Drops the event for any subscriber whose buffer is full.
// The read lock is held for the duration so cancel() cannot close a channel
// while a send is in flight (which would panic even with a select).
func (ps *PubSub) Publish(runID string, ev PushedEvent) {
	if ev.At.IsZero() {
		ev.At = eventTime()
	}
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	for _, ch := range ps.subs[runID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// eventTime is the time orbit-agents gives an event. Postgres stores
// microseconds, so minting at that precision keeps a live event's time equal to
// the same event's time on replay.
func eventTime() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
