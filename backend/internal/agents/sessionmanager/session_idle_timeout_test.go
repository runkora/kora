package sessionmanager

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// workRunner is a scriptRunner that reports work in flight, as the Claude
// runner does while subagents or background commands run.
type workRunner struct {
	*scriptRunner
	work atomic.Int64
}

func (r *workRunner) InFlightWork() int { return int(r.work.Load()) }

func newRegistryFor(t *testing.T, fallback time.Duration) (*Registry, *stateNotifier) {
	t.Helper()
	n := newStateNotifier()
	return NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), n, fallback), n
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
	r := &workRunner{scriptRunner: newScriptRunner()}
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
	r.work.Store(0)
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
