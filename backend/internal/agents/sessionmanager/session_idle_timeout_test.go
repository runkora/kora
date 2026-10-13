package sessionmanager

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// workRunner is a scriptRunner that reports work in flight, as the Claude
// runner does while subagents or background commands run.
type workRunner struct {
	*scriptRunner
	work    atomic.Int64
	changed chan struct{}
}

func newWorkRunner() *workRunner {
	return &workRunner{scriptRunner: newScriptRunner(), changed: make(chan struct{}, 1)}
}

// setWork reports a new count, as the CLI's background_tasks_changed does.
func (r *workRunner) setWork(n int64) {
	r.work.Store(n)
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

func (r *workRunner) WorkChanged() <-chan struct{} { return r.changed }

func (r *workRunner) InFlightWork() int { return int(r.work.Load()) }

func newRegistryFor(t *testing.T, fallback time.Duration) (*Registry, *stateNotifier) {
	t.Helper()
	n := newStateNotifier()
	return NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), n, fallback, testBrand), n
}

func completesWithin(n *stateNotifier, d time.Duration) bool {
	select {
	case <-n.completed:
		return true
	case <-time.After(d):
		return false
	}
}

func endTurn(t *testing.T, r *scriptRunner) {
	t.Helper()
	pushEvent(t, r, EventTypeTextDelta)
	pushEvent(t, r, EventTypeMessageStop)
}

func TestCreate_IdleTimeoutSelection(t *testing.T) {
	tests := []struct {
		name string
		cfg  time.Duration
		want time.Duration
	}{
		{name: "the run's timeout wins", cfg: 7 * time.Second, want: 7 * time.Second},
		{name: "unset takes the registry's", cfg: 0, want: time.Minute},
		{name: "negative takes the registry's", cfg: -time.Second, want: time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, _ := newRegistryFor(t, time.Minute)
			if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: newScriptRunner(), IdleTimeout: tt.cfg}); err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })
			reg.mu.RLock()
			got := time.Duration(reg.sessions["run-1"].idleTimeout.Load())
			reg.mu.RUnlock()
			if got != tt.want {
				t.Errorf("idle timeout = %s, want %s", got, tt.want)
			}
		})
	}
}

// A trigger run with no further input completes after its own, shorter timeout,
// not the registry's.
func TestDrain_RunTimeoutClosesIdleSession(t *testing.T) {
	reg, n := newRegistryFor(t, time.Hour)
	r := newScriptRunner()
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r, IdleTimeout: 50 * time.Millisecond}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	endTurn(t, r)
	if !completesWithin(n, time.Second) {
		t.Fatal("the session did not close after the run's idle timeout")
	}
}

func TestDrain_RegistryTimeoutAppliesWhenRunHasNone(t *testing.T) {
	reg, n := newRegistryFor(t, 50*time.Millisecond)
	r := newScriptRunner()
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	endTurn(t, r)
	if !completesWithin(n, time.Second) {
		t.Fatal("the session did not close after the registry's idle timeout")
	}
}

// A person's message to a trigger run switches it to the manual timeout: the
// session that would have closed after the short one now outlives it.
func TestSetIdleTimeout_SwitchesToTheManualTimeout(t *testing.T) {
	reg, n := newRegistryFor(t, time.Hour)
	r := newScriptRunner()
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r, IdleTimeout: 80 * time.Millisecond}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })

	if err := reg.SetIdleTimeout("run-1", time.Hour); err != nil {
		t.Fatalf("SetIdleTimeout: %v", err)
	}
	if err := reg.Send(context.Background(), "run-1", "also check X"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	endTurn(t, r)
	if completesWithin(n, 300*time.Millisecond) {
		t.Fatal("the session closed under the trigger timeout after a person messaged it")
	}
}

func TestSetIdleTimeout_UnknownSession(t *testing.T) {
	reg, _ := newRegistryFor(t, time.Minute)
	if err := reg.SetIdleTimeout("nope", time.Second); err != ErrSessionNotFound {
		t.Errorf("SetIdleTimeout = %v, want ErrSessionNotFound", err)
	}
}

// The switched timeout survives an interrupt, which replaces the session.
func TestInterrupt_ResumedSessionKeepsSwitchedTimeout(t *testing.T) {
	reg, n := newRegistryFor(t, time.Hour)
	r := newScriptRunner()
	r.resume = newScriptRunner()
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r, IdleTimeout: time.Hour}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.SetIdleTimeout("run-1", 50*time.Millisecond); err != nil {
		t.Fatalf("SetIdleTimeout: %v", err)
	}
	pushEvent(t, r, EventTypeTextDelta)
	if err := reg.Interrupt(context.Background(), "run-1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	if !completesWithin(n, time.Second) {
		t.Fatal("the resumed session did not close under the switched timeout")
	}
}

// The agent ends its turn while subagents or background tasks run. The run stays
// active until they end, however short the timeout.
func TestDrain_WorkInFlightKeepsRunActive(t *testing.T) {
	reg, n := newRegistryFor(t, 40*time.Millisecond)
	r := newWorkRunner()
	r.work.Store(2)
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })

	endTurn(t, r.scriptRunner)
	if completesWithin(n, 250*time.Millisecond) {
		t.Fatal("the session closed while work was in flight")
	}
	if calls, _ := n.snapshot(); len(calls) != 0 {
		t.Errorf("state calls = %v, want none: the run stays active", calls)
	}

	// The work ends, the agent wakes and finishes its turn: now it is idle.
	r.setWork(0)
	endTurn(t, r.scriptRunner)
	if !completesWithin(n, time.Second) {
		t.Fatal("the session did not close once the work ended")
	}
}

// A long foreground command emits no events and no message_stop, so the session
// never goes idle however long it runs.
func TestDrain_QuietForegroundCommandNeverIdles(t *testing.T) {
	reg, n := newRegistryFor(t, 30*time.Millisecond)
	r := newScriptRunner()
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })

	pushEvent(t, r, EventTypeToolUse)
	if completesWithin(n, 200*time.Millisecond) {
		t.Fatal("the session closed during a foreground command")
	}
	if calls, _ := n.snapshot(); len(calls) != 0 {
		t.Errorf("state calls = %v, want none", calls)
	}
}

func TestIdleTimeoutFromSecs(t *testing.T) {
	for secs, want := range map[int]time.Duration{-5: 0, 0: 0, 300: 300 * time.Second} {
		if got := idleTimeoutFromSecs(secs); got != want {
			t.Errorf("idleTimeoutFromSecs(%d) = %s, want %s", secs, got, want)
		}
	}
}

// The last task ends and the agent starts no turn: the run goes idle after the
// grace, so it does not wait for the reaper.
func TestDrain_IdleAfterWorkEndsWithNoNewTurn(t *testing.T) {
	reg, n := newRegistryFor(t, 40*time.Millisecond)
	reg.workGrace = 30 * time.Millisecond
	r := newWorkRunner()
	r.work.Store(1)
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	endTurn(t, r.scriptRunner)
	waitFor(t, "turn held", func() bool { _, evs := n.snapshot(); return len(evs) == 2 })

	r.setWork(0)
	if !completesWithin(n, time.Second) {
		t.Fatal("the session never went idle after its work ended")
	}
	if calls, _ := n.snapshot(); len(calls) == 0 || calls[0] != "idle" {
		t.Errorf("state calls = %v, want the run idled first", calls)
	}
}

// A task ends and the agent wakes to read the result: the new turn within the
// grace means the run never flaps to idle.
func TestDrain_NewTurnWithinGraceKeepsRunActive(t *testing.T) {
	reg, n := newRegistryFor(t, 40*time.Millisecond)
	reg.workGrace = 150 * time.Millisecond
	r := newWorkRunner()
	r.work.Store(1)
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })
	endTurn(t, r.scriptRunner)
	waitFor(t, "turn held", func() bool { _, evs := n.snapshot(); return len(evs) == 2 })

	r.setWork(0)
	pushEvent(t, r.scriptRunner, EventTypeTextDelta)
	if completesWithin(n, 400*time.Millisecond) {
		t.Fatal("the session closed while the agent was working on the result")
	}
	if calls, _ := n.snapshot(); len(calls) != 0 {
		t.Errorf("state calls = %v, want none", calls)
	}
}

// The timeout moves only with a delivered message: a failed send leaves a
// trigger run on its own timeout.
func TestHandleSendMessage_PersonSwitchAppliesOnlyWhenDelivered(t *testing.T) {
	tests := []struct {
		name          string
		sendErr       error
		want          time.Duration
		wantTriggered bool
	}{
		{name: "delivered", want: 20 * time.Minute},
		{name: "send failed", sendErr: io.ErrClosedPipe, want: 5 * time.Minute, wantTriggered: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, _ := newRegistryFor(t, time.Hour)
			r := newScriptRunner()
			r.sendErr = tt.sendErr
			if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r, IdleTimeout: 5 * time.Minute, Triggered: true}); err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })
			ctrl := &Controller{logger: logging.NewLoggerWithOutput(io.Discard), registry: reg}

			req := httptest.NewRequest(http.MethodPost, "/session/run-1/messages", strings.NewReader(`{"content":"hi","idle_timeout_secs":1200,"from_person":true}`))
			req.SetPathValue("runID", "run-1")
			req.Header.Set("Content-Type", "application/json")
			ctrl.handleSendMessage(httptest.NewRecorder(), req)

			reg.mu.RLock()
			got := time.Duration(reg.sessions["run-1"].idleTimeout.Load())
			triggered := reg.sessions["run-1"].triggered.Load()
			reg.mu.RUnlock()
			if got != tt.want {
				t.Errorf("idle timeout = %s, want %s", got, tt.want)
			}
			if triggered != tt.wantTriggered {
				t.Errorf("triggered = %v, want %v", triggered, tt.wantTriggered)
			}
		})
	}
}
