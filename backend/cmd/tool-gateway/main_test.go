package main

import (
	"context"
	"errors"
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

func noRetry() (time.Duration, bool) { return 0, false }

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
	go runIntrospectionLoop(ctx, rec.cycle, 20*time.Millisecond, func() (time.Duration, bool) { return time.Hour, true }, nil, testLogger())

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

// calls counts publish and rebuild, and can fail either on demand.
type calls struct {
	publish, rebuild       int
	publishErr, rebuildErr error
}

func (c *calls) pub(context.Context) error { c.publish++; return c.publishErr }
func (c *calls) reb(context.Context) error { c.rebuild++; return c.rebuildErr }

// The boot cycle always publishes, and a cycle that changes nothing skips both
// calls. This is the whole point of the change: a server that stays down must
// not cost a scope republish and a catalog rewrite on every retry.
func TestNewCycle_SkipsWhenNothingChanged(t *testing.T) {
	ctx := context.Background()
	c := &calls{}
	changed := false
	cycle := newCycle(func(context.Context, bool) bool { return changed }, c.pub, c.reb, testLogger())

	cycle(ctx, true) // boot: starts dirty
	if c.publish != 1 || c.rebuild != 1 {
		t.Fatalf("boot cycle: publish=%d rebuild=%d, want 1 and 1", c.publish, c.rebuild)
	}
	cycle(ctx, false)
	cycle(ctx, false)
	if c.publish != 1 || c.rebuild != 1 {
		t.Errorf("after two no-change cycles: publish=%d rebuild=%d, want both still 1", c.publish, c.rebuild)
	}

	changed = true
	cycle(ctx, false)
	if c.publish != 2 || c.rebuild != 2 {
		t.Errorf("after a change: publish=%d rebuild=%d, want 2 and 2", c.publish, c.rebuild)
	}
}

// A failed publish must be retried on the next cycle even though nothing
// changed, or a policy-service outage strands the scopes until something
// unrelated happens to move.
func TestNewCycle_RetriesAfterAFailedPublish(t *testing.T) {
	ctx := context.Background()
	c := &calls{publishErr: errors.New("policy-service down")}
	cycle := newCycle(func(context.Context, bool) bool { return false }, c.pub, c.reb, testLogger())

	cycle(ctx, true)
	if c.publish != 1 {
		t.Fatalf("boot publish=%d, want 1", c.publish)
	}
	cycle(ctx, false) // nothing changed, but the last publish failed
	if c.publish != 2 {
		t.Errorf("publish=%d after a failure, want 2: a failed publish must be retried", c.publish)
	}

	c.publishErr = nil
	cycle(ctx, false)
	if c.publish != 3 {
		t.Fatalf("publish=%d, want 3", c.publish)
	}
	cycle(ctx, false) // succeeded last time and nothing changed, so skip
	if c.publish != 3 {
		t.Errorf("publish=%d, want 3: once it succeeds the skip resumes", c.publish)
	}
}

// The same guarantee for the catalog write, which fails independently of the
// publish.
func TestNewCycle_RetriesAfterAFailedRebuild(t *testing.T) {
	ctx := context.Background()
	c := &calls{rebuildErr: errors.New("db down")}
	cycle := newCycle(func(context.Context, bool) bool { return false }, c.pub, c.reb, testLogger())

	cycle(ctx, true)
	cycle(ctx, false)
	if c.rebuild != 2 {
		t.Errorf("rebuild=%d after a failure, want 2: a failed catalog write must be retried", c.rebuild)
	}

	c.rebuildErr = nil
	cycle(ctx, false) // the retry, which now succeeds
	cycle(ctx, false) // nothing changed and the last write stuck, so skip
	if c.rebuild != 3 {
		t.Errorf("rebuild=%d, want 3: the retry succeeds and then the skip resumes", c.rebuild)
	}
}
