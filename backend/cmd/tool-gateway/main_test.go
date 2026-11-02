package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func testLogger() logging.Logger { return logging.NewLogger() }

// cycleRecorder counts cycle calls and signals each one.
type cycleRecorder struct {
	mu    sync.Mutex
	calls int
	fire  chan struct{}
}

func newCycleRecorder() *cycleRecorder {
	return &cycleRecorder{fire: make(chan struct{}, 64)}
}

func (c *cycleRecorder) cycle(context.Context, bool) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	select {
	case c.fire <- struct{}{}:
	default:
	}
}

func (c *cycleRecorder) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// waitForCycles blocks until the loop has run n cycles, or fails.
func (c *cycleRecorder) waitForCycles(t *testing.T, n int, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for c.count() < n {
		select {
		case <-c.fire:
		case <-deadline:
			t.Fatalf("saw %d cycles in %s, want %d", c.count(), within, n)
		}
	}
}

// never/always are the two nextRetry shapes the loop sees.
func noRetry() (time.Duration, bool) { return 0, false }
func retryIn(d time.Duration) func() (time.Duration, bool) {
	return func() (time.Duration, bool) { return d, true }
}

// With nothing awaiting a retry the loop waits the configured interval.
func TestRunIntrospectionLoop_UsesTheConfiguredIntervalWhenNothingPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newCycleRecorder()
	go runIntrospectionLoop(ctx, rec.cycle, time.Hour, noRetry, nil, testLogger())

	rec.waitForCycles(t, 1, 5*time.Second) // the boot cycle
	time.Sleep(300 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Errorf("cycles = %d, want 1: with no retry pending the loop must wait the configured interval", got)
	}
}

// The configured interval is the ceiling: a retry further out than it must not
// stretch the wait beyond what the deployment asked for.
func TestRunIntrospectionLoop_NeverWaitsLongerThanConfigured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newCycleRecorder()
	go runIntrospectionLoop(ctx, rec.cycle, 20*time.Millisecond, retryIn(time.Hour), nil, testLogger())

	rec.waitForCycles(t, 3, 5*time.Second)
}

// A reload that lands an unreachable server must not sit out a long wait.
func TestRunIntrospectionLoop_ReloadRecomputesTheWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newCycleRecorder()
	reloaded := make(chan struct{}, 1)
	var mu sync.Mutex
	pending := false
	next := func() (time.Duration, bool) {
		mu.Lock()
		defer mu.Unlock()
		if !pending {
			return 0, false
		}
		return 20 * time.Millisecond, true
	}

	go runIntrospectionLoop(ctx, rec.cycle, time.Hour, next, reloaded, testLogger())
	rec.waitForCycles(t, 1, 5*time.Second) // boot cycle, then parks on the hour

	// With nothing pending the reload must only recompute the wait. If it ran a
	// cycle of its own the count would move.
	reloaded <- struct{}{}
	time.Sleep(200 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Fatalf("cycles = %d after a reload with nothing pending, want 1: the reload arm must not run a cycle", got)
	}

	// Now a retry is pending, so the recomputed wait is the short one.
	mu.Lock()
	pending = true
	mu.Unlock()
	reloaded <- struct{}{}

	rec.waitForCycles(t, 3, 5*time.Second)
}

// Cancelling the context ends the loop rather than leaving it parked on a timer.
func TestRunIntrospectionLoop_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rec := newCycleRecorder()
	done := make(chan struct{})
	go func() {
		runIntrospectionLoop(ctx, rec.cycle, time.Hour, noRetry, nil, testLogger())
		close(done)
	}()

	rec.waitForCycles(t, 1, 5*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not return after its context was cancelled")
	}
}
