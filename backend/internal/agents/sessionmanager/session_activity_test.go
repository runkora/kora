package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// scriptRunner is a Runner whose events the test pushes by hand. Interrupt
// closes the event stream, the way SIGINT ends the Claude process.
type scriptRunner struct {
	events  chan Event
	mu      sync.Mutex
	sendErr error
	sent    []string
	partial string // text the turn has streamed, for TakeText
	// survives makes Interrupt leave the process running, as Codex and OpenCode do.
	survives bool
	resume   *scriptRunner
	stopped  sync.Once
}

func newScriptRunner() *scriptRunner { return &scriptRunner{events: make(chan Event, 16)} }

func (r *scriptRunner) Start(context.Context, string) error                  { return nil }
func (r *scriptRunner) SendToolResult(context.Context, string, string) error { return nil }
func (r *scriptRunner) Events() <-chan Event                                 { return r.events }
func (r *scriptRunner) TakeText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.partial
	r.partial = ""
	return t
}
func (r *scriptRunner) SessionID() string { return "sess-1" }
func (r *scriptRunner) Send(_ context.Context, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendErr != nil {
		return r.sendErr
	}
	r.sent = append(r.sent, text)
	return nil
}
func (r *scriptRunner) Interrupt() error {
	if !r.survives {
		r.stop()
	}
	return nil
}
func (r *scriptRunner) Close() error { r.stop(); return nil }
func (r *scriptRunner) stop()        { r.stopped.Do(func() { close(r.events) }) }
func (r *scriptRunner) NewResumeRunner() Runner {
	if r.resume == nil {
		return nil
	}
	return r.resume
}
func (r *scriptRunner) sentTexts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sent)
}

// stateNotifier records run-state callbacks and pushed events.
type stateNotifier struct {
	NoopNotifier
	mu        sync.Mutex
	calls     []string
	events    []Event
	completed chan string
}

func newStateNotifier() *stateNotifier { return &stateNotifier{completed: make(chan string, 1)} }

func (n *stateNotifier) record(call string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, call)
}
func (n *stateNotifier) IdleRun(context.Context, string) error     { n.record("idle"); return nil }
func (n *stateNotifier) ActivateRun(context.Context, string) error { n.record("active"); return nil }
func (n *stateNotifier) CompleteRun(_ context.Context, _ string, from string, _ bool) error {
	n.completed <- from
	return nil
}
func (n *stateNotifier) Notify(_ context.Context, _ string, ev Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, ev)
	return nil
}
func (n *stateNotifier) snapshot() ([]string, []Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.calls), slices.Clone(n.events)
}

// waitFor polls cond until it holds or a second passes. The drain loop runs on
// its own goroutine, so each pushed event lands asynchronously.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func startScripted(t *testing.T, idle time.Duration) (*Registry, *scriptRunner, *stateNotifier) {
	t.Helper()
	n := newStateNotifier()
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), n, idle)
	r := newScriptRunner()
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: r}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return reg, r, n
}

func pushEvent(t *testing.T, r *scriptRunner, typ EventType) {
	t.Helper()
	r.events <- Event{Type: typ, Payload: json.RawMessage(`{}`)}
}

func TestDrain_IdleToActive(t *testing.T) {
	tests := []struct {
		name       string
		viaMessage bool
		next       EventType
		wantCalls  []string
	}{
		{name: "a user message activates the run once, when it is sent", viaMessage: true, next: EventTypeTextDelta, wantCalls: []string{"idle", "active"}},
		{name: "a turn with no message activates the run", next: EventTypeTextDelta, wantCalls: []string{"idle", "active"}},
		{name: "a tool heartbeat leaves the run idle", next: EventTypeToolProgress, wantCalls: []string{"idle"}},
		{name: "a provider retry leaves the run idle", next: EventTypeAPIRetry, wantCalls: []string{"idle"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, r, n := startScripted(t, time.Minute)
			t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })

			pushEvent(t, r, EventTypeMessageStop)
			waitFor(t, "idle", func() bool { c, _ := n.snapshot(); return len(c) == 1 })
			if tt.viaMessage {
				if err := reg.Send(context.Background(), "run-1", "next"); err != nil {
					t.Fatalf("Send: %v", err)
				}
				waitFor(t, "activation on send", func() bool { c, _ := n.snapshot(); return len(c) == 2 })
			}
			pushEvent(t, r, tt.next)
			waitFor(t, "event pushed", func() bool { _, evs := n.snapshot(); return len(evs) == 2 })

			if calls, _ := n.snapshot(); !slices.Equal(calls, tt.wantCalls) {
				t.Errorf("state calls = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

// A message must stop the idle timer at once: the turn it starts may emit
// only provider retries for minutes before its first real event.
func TestSend_StopsIdleTimerBeforeTheTurnsFirstEvent(t *testing.T) {
	reg, r, n := startScripted(t, 50*time.Millisecond)
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })

	pushEvent(t, r, EventTypeMessageStop)
	waitFor(t, "idle", func() bool { c, _ := n.snapshot(); return len(c) == 1 })
	if err := reg.Send(context.Background(), "run-1", "next"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	pushEvent(t, r, EventTypeAPIRetry)
	select {
	case <-n.completed:
		t.Fatal("the idle timer closed a session whose turn had started")
	case <-time.After(200 * time.Millisecond):
	}
}

// A resumed session after an interrupt must time out like any idle run
// instead of waiting for the reaper.
func TestInterrupt_ResumedSessionArmsIdleTimer(t *testing.T) {
	reg, r, n := startScripted(t, 50*time.Millisecond)
	r.resume = newScriptRunner()

	pushEvent(t, r, EventTypeTextDelta)
	if err := reg.Interrupt(context.Background(), "run-1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	select {
	case from := <-n.completed:
		if from != "idle" {
			t.Errorf("completed from %q, want idle", from)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resumed session never timed out")
	}
}

// The run is reported idle before the resumed session can take a message, so
// that message's activation lands last and the run ends up active.
func TestInterrupt_IdleIsReportedBeforeTheResumedSessionRuns(t *testing.T) {
	reg, r, n := startScripted(t, time.Minute)
	resumed := newScriptRunner()
	r.resume = resumed
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })

	pushEvent(t, r, EventTypeTextDelta)
	if err := reg.Interrupt(context.Background(), "run-1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	waitFor(t, "resume", func() bool {
		reg.mu.RLock()
		defer reg.mu.RUnlock()
		return reg.sessions["run-1"] != nil && reg.sessions["run-1"].runner == Runner(resumed)
	})
	if err := reg.Send(context.Background(), "run-1", "go on"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitFor(t, "activation", func() bool { c, _ := n.snapshot(); return slices.Contains(c, "active") })
	if calls, _ := n.snapshot(); calls[len(calls)-1] != "active" {
		t.Errorf("state calls = %v, want the activation last", calls)
	}
}

func TestInterrupt_NextMessageTellsModelTheUserStopped(t *testing.T) {
	reg, r, _ := startScripted(t, time.Minute)
	resumed := newScriptRunner()
	r.resume = resumed
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })

	if err := reg.Interrupt(context.Background(), "run-1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	waitFor(t, "resume", func() bool {
		reg.mu.RLock()
		defer reg.mu.RUnlock()
		return reg.sessions["run-1"] != nil && reg.sessions["run-1"].runner == Runner(resumed)
	})

	resumed.sendErr = errors.New("stdin closed")
	if err := reg.Send(context.Background(), "run-1", "lost"); err == nil {
		t.Fatal("Send succeeded, want the runner's error")
	}
	resumed.sendErr = nil
	for _, msg := range []string{"carry on", "and again"} {
		if err := reg.Send(context.Background(), "run-1", msg); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	got := resumed.sentTexts()
	want := []string{stopNote + "carry on", "and again"}
	if !slices.Equal(got, want) {
		t.Errorf("sent = %q, want %q", got, want)
	}
}

type failingProvisionNotifier struct {
	*stateNotifier
	err error
}

func (n *failingProvisionNotifier) ProvisionSandbox(context.Context, string, bool) (Sandbox, error) {
	return Sandbox{}, n.err
}

func sandboxPhases(t *testing.T, evs []Event) []string {
	t.Helper()
	var phases []string
	for _, ev := range evs {
		if ev.Type != EventTypeSandboxStatus {
			continue
		}
		var p SandboxStatusPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("unmarshal sandbox_status: %v", err)
		}
		phases = append(phases, p.Phase)
	}
	return phases
}

func TestProvisionSandbox_ReportsPhases(t *testing.T) {
	t.Run("fresh provision with egress settled", func(t *testing.T) {
		addr := startStubExecServer(t, &stubExecServer{})
		n := &provisionPhaseNotifier{stateNotifier: newStateNotifier(), endpoint: addr}
		reg, _ := newTestRegistry(t, n)
		hostSession(reg, "run-1")

		if _, err := reg.ProvisionSandbox(context.Background(), "run-1", false); err != nil {
			t.Fatalf("ProvisionSandbox: %v", err)
		}
		_, evs := n.snapshot()
		want := []string{SandboxPhaseProvisioning, SandboxPhasePodReady, SandboxPhaseEgressReady}
		if got := sandboxPhases(t, evs); !slices.Equal(got, want) {
			t.Errorf("phases = %v, want %v", got, want)
		}
	})

	t.Run("descriptor write failure", func(t *testing.T) {
		addr := startStubExecServer(t, &stubExecServer{})
		n := &provisionPhaseNotifier{stateNotifier: newStateNotifier(), endpoint: addr}
		reg, ws := newTestRegistry(t, n)
		hostSession(reg, "run-1")
		// A file where the descriptor directory belongs makes the write fail.
		if err := os.MkdirAll(filepath.Join(ws, "run-1"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ws, "run-1", ".sandbox"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.ProvisionSandbox(context.Background(), "run-1", false); err == nil {
			t.Fatal("ProvisionSandbox succeeded, want the write error")
		}
		_, evs := n.snapshot()
		want := []string{SandboxPhaseProvisioning, SandboxPhasePodReady, SandboxPhaseFailed}
		if got := sandboxPhases(t, evs); !slices.Equal(got, want) {
			t.Errorf("phases = %v, want %v", got, want)
		}
	})

	t.Run("provision failure", func(t *testing.T) {
		n := &failingProvisionNotifier{stateNotifier: newStateNotifier(), err: errors.New("no capacity")}
		reg, _ := newTestRegistry(t, n)
		hostSession(reg, "run-1")

		if _, err := reg.ProvisionSandbox(context.Background(), "run-1", false); err == nil {
			t.Fatal("ProvisionSandbox succeeded, want the notifier's error")
		}
		_, evs := n.snapshot()
		want := []string{SandboxPhaseProvisioning, SandboxPhaseFailed}
		if got := sandboxPhases(t, evs); !slices.Equal(got, want) {
			t.Errorf("phases = %v, want %v", got, want)
		}
		var failed SandboxStatusPayload
		if err := json.Unmarshal(evs[len(evs)-1].Payload, &failed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if failed.Message != "sandbox provisioning failed" {
			t.Errorf("message = %q, want the fixed text, not the internal error", failed.Message)
		}
	})
}

type provisionPhaseNotifier struct {
	*stateNotifier
	endpoint string
}

func (n *provisionPhaseNotifier) ProvisionSandbox(context.Context, string, bool) (Sandbox, error) {
	return testSandbox(n.endpoint), nil
}

// Codex and OpenCode stop a turn without exiting: the session carries on, idles
// normally, and the next message still carries the stop note.
func TestInterrupt_RuntimeThatSurvivesTheStop(t *testing.T) {
	reg, r, n := startScripted(t, time.Minute)
	r.survives = true
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })

	pushEvent(t, r, EventTypeTextDelta)
	if err := reg.Interrupt(context.Background(), "run-1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	pushEvent(t, r, EventTypeTurnStopped)
	pushEvent(t, r, EventTypeMessageStop)
	waitFor(t, "idle", func() bool { c, _ := n.snapshot(); return slices.Contains(c, "idle") })

	if err := reg.Send(context.Background(), "run-1", "carry on"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := r.sentTexts(); !slices.Equal(got, []string{stopNote + "carry on"}) {
		t.Errorf("sent = %q, want the stop note first", got)
	}
	reg.mu.RLock()
	s := reg.sessions["run-1"]
	reg.mu.RUnlock()
	if s == nil || s.runner != Runner(r) || s.interruptClose.Load() {
		t.Error("the session should carry on with the same runner and no pending restart")
	}
}

// Text the agent wrote before a mid-turn message is recorded before the
// message, which orbit-agents records once Send returns.
func TestSend_RecordsTheTextBeforeAMidTurnMessage(t *testing.T) {
	reg, r, n := startScripted(t, time.Minute)
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })
	r.partial = "Working on it. "

	if err := reg.Send(context.Background(), "run-1", "also do X"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	_, evs := n.snapshot()
	if len(evs) != 1 || evs[0].Type != EventTypeTextBlock {
		t.Fatalf("pushed %v, want one text_block before Send returned", eventTypes(evs))
	}
	if got := decode[TextBlockPayload](t, evs[0]).Content; got != "Working on it. " {
		t.Errorf("text_block = %q", got)
	}
	if got := r.sentTexts(); !slices.Equal(got, []string{"also do X"}) {
		t.Errorf("sent = %q", got)
	}
}
