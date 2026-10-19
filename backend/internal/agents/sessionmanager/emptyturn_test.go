package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// emptyTurnNotifier records run errors, idle transitions and how the run ended.
type emptyTurnNotifier struct {
	NoopNotifier
	mu        sync.Mutex
	runErrors []string
	idles     int
	completed chan bool
}

func (n *emptyTurnNotifier) Notify(_ context.Context, _ string, ev Event) error {
	if ev.Type != EventTypeRunError {
		return nil
	}
	var p RunErrorPayload
	_ = json.Unmarshal(ev.Payload, &p)
	n.mu.Lock()
	defer n.mu.Unlock()
	n.runErrors = append(n.runErrors, p.Code)
	return nil
}

func (n *emptyTurnNotifier) IdleRun(context.Context, string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.idles++
	return nil
}

func (n *emptyTurnNotifier) CompleteRun(_ context.Context, _ string, _ string, failed bool) error {
	n.completed <- failed
	return nil
}

func (n *emptyTurnNotifier) snapshot() (errs []string, idles int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.runErrors), n.idles
}

func startEmptyTurnSession(t *testing.T, triggered bool) (*Registry, *scriptRunner, *emptyTurnNotifier) {
	t.Helper()
	n := &emptyTurnNotifier{completed: make(chan bool, 1)}
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), n, time.Minute, testBrand)
	r := newScriptRunner()
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r, Triggered: triggered}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return reg, r, n
}

func ev(t *testing.T, typ EventType, payload any) Event {
	t.Helper()
	e, err := newEvent(typ, payload)
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}
	return e
}

// emptyTurn is a turn whose last model response, after a tool call, was empty.
func emptyTurn(t *testing.T) []Event {
	return []Event{
		ev(t, EventTypeToolUse, ToolUsePayload{ID: "t1", Name: "Bash"}),
		ev(t, EventTypeToolResult, ToolResultPayload{ToolUseID: "t1"}),
		ev(t, EventTypeMessageComplete, MessageCompletePayload{Thinking: "a tool call written as text"}),
		ev(t, EventTypeMessageStop, nil),
	}
}

func textTurn(t *testing.T) []Event {
	return []Event{
		ev(t, EventTypeTextDelta, TextDeltaPayload{Delta: "Done."}),
		ev(t, EventTypeMessageComplete, MessageCompletePayload{Text: "Done."}),
		ev(t, EventTypeMessageStop, nil),
	}
}

func push(r *scriptRunner, evs []Event) {
	for _, e := range evs {
		r.events <- e
	}
}

func TestDrain_EmptyTurnRepromptsThenRecovers(t *testing.T) {
	reg, r, n := startEmptyTurnSession(t, false)
	push(r, emptyTurn(t))
	waitFor(t, "the re-prompt", func() bool { return len(r.sentTexts()) == 1 })
	if _, idles := n.snapshot(); idles != 0 {
		t.Errorf("idle transitions after the re-prompt = %d, want 0", idles)
	}

	push(r, textTurn(t))
	waitFor(t, "the run to idle", func() bool { _, idles := n.snapshot(); return idles == 1 })
	if sent := r.sentTexts(); !slices.Equal(sent, []string{emptyTurnPrompt}) {
		t.Errorf("sent = %q, want one re-prompt", sent)
	}
	if errs, _ := n.snapshot(); len(errs) != 0 {
		t.Errorf("run errors = %v, want none", errs)
	}
	_ = reg.Close(context.Background(), "run-1")
}

func TestDrain_TwoEmptyTurnsFailTheRun(t *testing.T) {
	_, r, n := startEmptyTurnSession(t, false)
	push(r, emptyTurn(t))
	waitFor(t, "the re-prompt", func() bool { return len(r.sentTexts()) == 1 })
	push(r, emptyTurn(t))

	select {
	case failed := <-n.completed:
		if !failed {
			t.Error("run completed, want failed")
		}
	case <-time.After(time.Second):
		t.Fatal("run did not end after the second empty turn")
	}
	if errs, _ := n.snapshot(); !slices.Equal(errs, []string{runErrorEmptyTurn}) {
		t.Errorf("run errors = %v, want only empty_turn", errs)
	}
	if sent := r.sentTexts(); len(sent) != 1 {
		t.Errorf("sent = %q, want one re-prompt", sent)
	}
}

// erroredTurn is a turn the runner ended with a run_error before any output.
func erroredTurn(t *testing.T) []Event {
	return []Event{
		ev(t, EventTypeRunError, RunErrorPayload{Code: "empty_response", Message: "stream cut"}),
		ev(t, EventTypeMessageStop, nil),
	}
}

func TestDrain_ErroredEmptyTurnFailsATriggerRun(t *testing.T) {
	_, r, n := startEmptyTurnSession(t, true)
	push(r, erroredTurn(t))

	select {
	case failed := <-n.completed:
		if !failed {
			t.Error("run completed, want failed")
		}
	case <-time.After(time.Second):
		t.Fatal("run did not fail after an empty turn that followed a run error")
	}
	if errs, _ := n.snapshot(); !slices.Equal(errs, []string{"empty_response"}) {
		t.Errorf("run errors = %q, want only the turn's empty_response", errs)
	}
	if sent := r.sentTexts(); len(sent) != 0 {
		t.Errorf("sent = %q, want no re-prompt", sent)
	}
}

func TestDrain_ErroredEmptyTurnIdlesAPersonRun(t *testing.T) {
	tests := []struct {
		name      string
		triggered bool
	}{
		{"a run a person started", false},
		{"a trigger run a person messaged", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, r, n := startEmptyTurnSession(t, tt.triggered)
			if tt.triggered {
				reg.SetTriggered("run-1", false)
			}
			push(r, erroredTurn(t))
			waitFor(t, "the run to idle", func() bool { _, idles := n.snapshot(); return idles == 1 })
			if errs, _ := n.snapshot(); !slices.Equal(errs, []string{"empty_response"}) {
				t.Errorf("run errors = %q, want only the turn's empty_response", errs)
			}
			_ = reg.Close(context.Background(), "run-1")
		})
	}
}

func TestDrain_TurnWithTextIsNotEmpty(t *testing.T) {
	reg, r, n := startEmptyTurnSession(t, false)
	push(r, textTurn(t))
	waitFor(t, "the run to idle", func() bool { _, idles := n.snapshot(); return idles == 1 })
	if sent := r.sentTexts(); len(sent) != 0 {
		t.Errorf("sent = %q, want no re-prompt", sent)
	}
	_ = reg.Close(context.Background(), "run-1")
}

func TestEmptyTurnGuard_Verdicts(t *testing.T) {
	stop := func(t *testing.T) Event { return ev(t, EventTypeMessageStop, nil) }
	tests := []struct {
		name   string
		events func(t *testing.T) []Event
		want   emptyTurnVerdict
	}{
		{"no output", func(t *testing.T) []Event { return []Event{stop(t)} }, emptyTurnReprompt},
		{"text before a tool call", func(t *testing.T) []Event {
			return []Event{ev(t, EventTypeTextBlock, TextBlockPayload{Content: "Looking."}), ev(t, EventTypeToolUse, ToolUsePayload{ID: "t1"}), stop(t)}
		}, emptyTurnOK},
		{"empty response after a tool result", func(t *testing.T) []Event { return emptyTurn(t) }, emptyTurnReprompt},
		{"subagent tool result keeps the parent's output", func(t *testing.T) []Event {
			return []Event{ev(t, EventTypeToolUse, ToolUsePayload{ID: "a1", Name: "Agent"}), ev(t, EventTypeToolResult, ToolResultPayload{ToolUseID: "s1", ParentToolUseID: "a1"}), stop(t)}
		}, emptyTurnOK},
		{"subagent tool call after the main tool result", func(t *testing.T) []Event {
			return []Event{ev(t, EventTypeToolUse, ToolUsePayload{ID: "a1", Name: "spawn"}), ev(t, EventTypeToolResult, ToolResultPayload{ToolUseID: "a1"}), ev(t, EventTypeToolUse, ToolUsePayload{ID: "s1", Name: "Bash", ParentToolUseID: "a1"}), stop(t)}
		}, emptyTurnReprompt},
		{"error turn", func(t *testing.T) []Event {
			return []Event{ev(t, EventTypeRunError, RunErrorPayload{Code: "agent_error"}), stop(t)}
		}, emptyTurnErrored},
		{"error turn with partial text", func(t *testing.T) []Event {
			return []Event{ev(t, EventTypeMessageComplete, MessageCompletePayload{Text: "part", Interrupted: true}), ev(t, EventTypeRunError, RunErrorPayload{Code: "agent_error"}), stop(t)}
		}, emptyTurnOK},
		{"stopped turn", func(t *testing.T) []Event {
			return []Event{ev(t, EventTypeTurnStopped, TurnStoppedPayload{Reason: "user"}), stop(t)}
		}, emptyTurnOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g emptyTurnGuard
			var got emptyTurnVerdict
			for _, e := range tt.events(t) {
				got = g.observe(e)
			}
			if got != tt.want {
				t.Errorf("verdict = %v, want %v", got, tt.want)
			}
		})
	}
}

// A turn with output between two empty turns resets the re-prompt.
func TestEmptyTurnGuard_OutputResetsReprompt(t *testing.T) {
	var g emptyTurnGuard
	stop := ev(t, EventTypeMessageStop, nil)
	text := ev(t, EventTypeTextDelta, TextDeltaPayload{Delta: "ok"})
	for i, step := range []struct {
		events []Event
		want   emptyTurnVerdict
	}{
		{[]Event{stop}, emptyTurnReprompt},
		{[]Event{text, stop}, emptyTurnOK},
		{[]Event{stop}, emptyTurnReprompt},
		{[]Event{stop}, emptyTurnFail},
	} {
		var got emptyTurnVerdict
		for _, e := range step.events {
			got = g.observe(e)
		}
		if got != step.want {
			t.Errorf("turn %d: verdict = %v, want %v", i, got, step.want)
		}
	}
}

// A re-prompt that could not be sent does not count, so the next empty turn
// is re-prompted instead of failing the run.
func TestDrain_FailedRepromptDoesNotCount(t *testing.T) {
	reg, r, n := startEmptyTurnSession(t, false)
	r.mu.Lock()
	r.sendErr = errors.New("stdin closed")
	r.mu.Unlock()
	push(r, emptyTurn(t))
	waitFor(t, "the run to idle", func() bool { _, idles := n.snapshot(); return idles == 1 })

	r.mu.Lock()
	r.sendErr = nil
	r.mu.Unlock()
	push(r, emptyTurn(t))
	waitFor(t, "the re-prompt", func() bool { return len(r.sentTexts()) == 1 })
	if errs, _ := n.snapshot(); len(errs) != 0 {
		t.Errorf("run errors = %v, want none", errs)
	}
	_ = reg.Close(context.Background(), "run-1")
}
