package sessionmanager

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubRunner is a minimal Runner that can be configured to return an error
// from Close() and signals when its event channel has been drained.
type stubRunner struct {
	events   chan Event
	closeErr error
	closed   chan struct{}
}

func newStubRunner(closeErr error) *stubRunner {
	return &stubRunner{
		events:   make(chan Event),
		closeErr: closeErr,
		closed:   make(chan struct{}),
	}
}

func (r *stubRunner) Start(_ context.Context, _ string) error             { return nil }
func (r *stubRunner) Send(_ context.Context, _ string) error              { return nil }
func (r *stubRunner) SendToolResult(_ context.Context, _, _ string) error { return nil }
func (r *stubRunner) Events() <-chan Event                                { return r.events }
func (r *stubRunner) Interrupt() error                                    { return nil }
func (r *stubRunner) SessionID() string                                   { return "" }
func (r *stubRunner) TakeText() string                                    { return "" }
func (r *stubRunner) NewResumeRunner() Runner                             { return nil }
func (r *stubRunner) Close() error {
	select {
	case <-r.closed:
	default:
		close(r.closed)
		close(r.events)
	}
	return r.closeErr
}

// completionNotifier records whether CompleteRun was called.
type completionNotifier struct {
	NoopNotifier
	done chan struct{}
}

func (n *completionNotifier) CompleteRun(_ context.Context, _ string, _ string, _ bool) error {
	select {
	case <-n.done:
	default:
		close(n.done)
	}
	return nil
}

// TestClose_RunnerErrorStillDrainsGoroutine verifies that when runner.Close()
// returns a non-nil error, Registry.Close() still waits for the drain goroutine
// to finish before returning (i.e. completeDrain fires CompleteRun, no goroutine
// leak).
func TestClose_RunnerErrorStillDrainsGoroutine(t *testing.T) {
	closeErr := errors.New("subprocess exited non-zero")
	runner := newStubRunner(closeErr)

	notifier := &completionNotifier{done: make(chan struct{})}
	reg, _ := newTestRegistry(t, notifier)

	cfg := SessionConfig{
		RunID:  "run-err",
		Runner: runner,
	}
	if err := reg.Create(context.Background(), cfg); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := reg.Close(context.Background(), "run-err")
	if !errors.Is(err, closeErr) {
		t.Fatalf("Close returned %v, want %v", err, closeErr)
	}

	// After Close returns, completeDrain must have already run. We verify by
	// checking that CompleteRun was called — with a short timeout to guard
	// against a regression that re-introduces the goroutine leak.
	select {
	case <-notifier.done:
		// Drain goroutine completed as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("drain goroutine did not complete after Close returned an error (goroutine leak)")
	}
}
