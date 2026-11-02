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

type cycleRecorder struct {
	mu     sync.Mutex
	calls  int
	forced []bool
	fire   chan struct{}
}

func newCycleRecorder() *cycleRecorder {
	return &cycleRecorder{fire: make(chan struct{}, 64)}
}

func (c *cycleRecorder) cycle(_ context.Context, force bool) {
	c.mu.Lock()
	c.calls++
	c.forced = append(c.forced, force)
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

func TestRunIntrospectionLoop_UsesTheConfiguredIntervalWhenNothingPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newCycleRecorder()
	go runIntrospectionLoop(ctx, rec.cycle, time.Hour, noRetry, nil, testLogger())

	rec.waitForCycles(t, 1, 5*time.Second) // the boot cycle
	rec.mu.Lock()
	bootForced := rec.forced[0]
	rec.mu.Unlock()
	if !bootForced {
		t.Error("the boot cycle must be forced, or a server inside its interval is never introspected")
	}
	time.Sleep(300 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Errorf("cycles = %d, want 1: with no retry pending the loop must wait the configured interval", got)
	}
}

func TestRunIntrospectionLoop_NeverWaitsLongerThanConfigured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newCycleRecorder()
	go runIntrospectionLoop(ctx, rec.cycle, 20*time.Millisecond, func() (time.Duration, bool) { return time.Hour, true }, nil, testLogger())

	rec.waitForCycles(t, 3, 5*time.Second)
}

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

	reloaded <- struct{}{}
	time.Sleep(200 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Fatalf("cycles = %d after a reload with nothing pending, want 1: the reload arm must not run a cycle", got)
	}

	mu.Lock()
	pending = true
	mu.Unlock()
	reloaded <- struct{}{}

	rec.waitForCycles(t, 3, 5*time.Second)
}

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

type calls struct {
	publish, rebuild       int
	publishErr, rebuildErr error
}

func (c *calls) pub(context.Context) error { c.publish++; return c.publishErr }
func (c *calls) reb(context.Context) error { c.rebuild++; return c.rebuildErr }

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

func TestNewCycle_ForcedCycleAlwaysPublishes(t *testing.T) {
	ctx := context.Background()
	c := &calls{}
	cycle := newCycle(func(context.Context, bool) bool { return false }, c.pub, c.reb, testLogger())

	cycle(ctx, true)
	cycle(ctx, false) // nothing changed: skipped
	if c.publish != 1 {
		t.Fatalf("publish=%d, want 1", c.publish)
	}
	// A reload recomputes prune from registry.Loaded(), which no snapshot
	// reflects, so a forced cycle must publish even with nothing introspected.
	cycle(ctx, true)
	if c.publish != 2 {
		t.Errorf("publish=%d after a forced cycle, want 2", c.publish)
	}
}

// A failed publish or catalog write leaves the cycle owing a republish, so the
// next cycle retries it even though nothing changed, and the skip resumes once
// it sticks.
func TestNewCycle_RetriesAfterAFailedStep(t *testing.T) {
	boom := errors.New("downstream down")
	for _, tc := range []struct {
		name string
		fail func(*calls, error)
		got  func(*calls) int
	}{
		{"publish", func(c *calls, e error) { c.publishErr = e }, func(c *calls) int { return c.publish }},
		{"rebuild", func(c *calls, e error) { c.rebuildErr = e }, func(c *calls) int { return c.rebuild }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			c := &calls{}
			tc.fail(c, boom)
			cycle := newCycle(func(context.Context, bool) bool { return false }, c.pub, c.reb, testLogger())

			cycle(ctx, true)
			cycle(ctx, false) // nothing changed, but the last cycle failed
			if got := tc.got(c); got != 2 {
				t.Errorf("%s=%d after a failure, want 2: a failed %s must be retried", tc.name, got, tc.name)
			}

			tc.fail(c, nil)
			cycle(ctx, false) // the retry, which now succeeds
			cycle(ctx, false) // nothing changed and it stuck, so skip
			if got := tc.got(c); got != 3 {
				t.Errorf("%s=%d, want 3: once it succeeds the skip resumes", tc.name, got)
			}
		})
	}
}
