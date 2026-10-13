package sessionmanager

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// A drain cut short by its deadline refuses new sessions and keeps the ones it has.
func TestRegistryDrain_Deadline(t *testing.T) {
	reg, _ := newTestRegistry(t, NoopNotifier{})
	hostSession(reg, "run-1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reg.Drain(ctx, time.Millisecond)

	if !reg.Draining() || reg.ActiveCount() != 1 {
		t.Errorf("draining = %v, active sessions = %d, want true and 1", reg.Draining(), reg.ActiveCount())
	}
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-2"}); !errors.Is(err, ErrDraining) {
		t.Errorf("Create while draining: err = %v, want ErrDraining", err)
	}
}

// A drain returns only once its last session has ended.
func TestRegistryDrain_WaitsForSessions(t *testing.T) {
	reg, _ := newTestRegistry(t, NoopNotifier{})
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: newStubRunner(nil)}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { reg.Drain(context.Background(), time.Millisecond); close(done) }()
	for !reg.Draining() {
		runtime.Gosched()
	}
	select {
	case <-done:
		t.Fatal("Drain returned while a session was open")
	default:
	}

	if err := reg.Close(context.Background(), "run-1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Drain did not return after the last session ended")
	}
}

// A drain with no sessions returns at once.
func TestRegistryDrain_NoSessions(t *testing.T) {
	reg, _ := newTestRegistry(t, NoopNotifier{})
	reg.Drain(context.Background(), time.Hour)
	if !reg.Draining() {
		t.Error("the registry is not draining")
	}
}

// failRecorder records the failed flag CompleteRun reports for each run.
type failRecorder struct {
	NoopNotifier
	mu     sync.Mutex
	failed map[string]bool
}

func (n *failRecorder) CompleteRun(_ context.Context, runID, _ string, failed bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.failed[runID] = failed
	return nil
}

// CloseAll ends every session as failed, and an ordinary close still completes the run.
func TestRegistryCloseAll_FailsRuns(t *testing.T) {
	n := &failRecorder{failed: map[string]bool{}}
	reg, _ := newTestRegistry(t, n)
	for _, id := range []string{"run-1", "run-2", "run-3"} {
		if err := reg.Create(context.Background(), SessionConfig{RunID: id, Runner: newStubRunner(nil)}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	if err := reg.Close(context.Background(), "run-3"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reg.CloseAll(context.Background())

	n.mu.Lock()
	defer n.mu.Unlock()
	if reg.ActiveCount() != 0 || !n.failed["run-1"] || !n.failed["run-2"] || n.failed["run-3"] {
		t.Errorf("active = %d, failed = %v, want 0 and runs 1 and 2 failed", reg.ActiveCount(), n.failed)
	}
}

// A draining pod fails its readiness check.
func TestHandleHealth_Draining(t *testing.T) {
	c, _ := sandboxTestController(t)
	for _, tc := range []struct {
		drain bool
		want  int
	}{{false, http.StatusOK}, {true, http.StatusServiceUnavailable}} {
		if tc.drain {
			c.registry.Drain(context.Background(), time.Millisecond)
		}
		w := httptest.NewRecorder()
		c.handleHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))
		if w.Code != tc.want {
			t.Errorf("draining=%v: status = %d, want %d", tc.drain, w.Code, tc.want)
		}
	}
}

// A draining pod answers a new session with 503, which orbit-agents retries on another pod.
func TestHandleCreateSession_Draining(t *testing.T) {
	c, _ := sandboxTestController(t)
	c.registry.Drain(context.Background(), time.Millisecond)

	body := `{"run_id":"run-1","agent_type_id":"at-1","tenant_id":"t-1","model":"m","framework":"claude-code"}`
	r := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.handleCreateSession(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}
}
