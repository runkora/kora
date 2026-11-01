package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func testLogger() logging.Logger { return logging.NewLogger() }

// cycleRecorder records each cycle call and the gap before it, so a test can
// assert which interval the loop waited.
type cycleRecorder struct {
	mu    sync.Mutex
	gaps  []time.Duration
	last  time.Time
	calls int
	fire  chan struct{}
}

func newCycleRecorder() *cycleRecorder {
	return &cycleRecorder{last: time.Now(), fire: make(chan struct{}, 64)}
}

func (c *cycleRecorder) cycle(context.Context, bool) {
	c.mu.Lock()
	now := time.Now()
	c.gaps = append(c.gaps, now.Sub(c.last))
	c.last = now
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

// A server that has never been reached leaves the gateway serving an empty tool
// list, so the loop must not wait out the configured interval before trying
// again (#880). With ready() false the wait is catchUpInterval, not the
// configured one.
func TestRunIntrospectionLoop_CatchesUpWhenAServerIsUnreached(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newCycleRecorder()
	// An interval far longer than the test: a second cycle can only mean the
	// loop used the catch-up wait instead of waiting this out.
	notReady := func() bool { return false }
	go runIntrospectionLoop(ctx, rec.cycle, time.Hour, 20*time.Millisecond, notReady, testLogger())

	rec.waitForCycles(t, 3, 5*time.Second)
}

// Once every server has tools the loop must settle back to the configured
// interval rather than keep hammering on the catch-up one.
func TestRunIntrospectionLoop_UsesTheConfiguredIntervalWhenReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newCycleRecorder()
	ready := func() bool { return true }

	// Far longer than the shortened catch-up: a ready loop must wait this out,
	// so the only cycle in the window below is the boot one.
	go runIntrospectionLoop(ctx, rec.cycle, time.Hour, 20*time.Millisecond, ready, testLogger())

	rec.waitForCycles(t, 1, 5*time.Second) // the boot cycle
	time.Sleep(500 * time.Millisecond)     // 25x the catch-up interval
	if got := rec.count(); got != 1 {
		t.Errorf("cycles = %d, want 1: a ready loop must wait the configured interval, not the catch-up one", got)
	}
}

// The configured interval is the floor: a deployment that sets an interval
// shorter than the catch-up must not have it lengthened.
func TestRunIntrospectionLoop_NeverWaitsLongerThanConfigured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newCycleRecorder()
	notReady := func() bool { return false }
	go runIntrospectionLoop(ctx, rec.cycle, 20*time.Millisecond, time.Hour, notReady, testLogger())

	// Cycles keep landing, so the loop took min(catch-up, configured) and did
	// not stretch the configured interval up to the catch-up one.
	rec.waitForCycles(t, 3, 5*time.Second)
}

// Cancelling the context ends the loop rather than leaving it parked on a timer.
func TestRunIntrospectionLoop_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rec := newCycleRecorder()
	done := make(chan struct{})
	go func() {
		runIntrospectionLoop(ctx, rec.cycle, time.Hour, time.Hour, func() bool { return true }, testLogger())
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
