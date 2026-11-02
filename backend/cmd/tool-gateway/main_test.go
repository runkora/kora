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

// The loop waits min(next retry, interval): the configured interval when
// nothing is pending, and never longer than it when a retry is further out.
func TestCycleWait(t *testing.T) {
	retry := func(d time.Duration, pending bool) func() (time.Duration, bool) {
		return func() (time.Duration, bool) { return d, pending }
	}
	for _, tc := range []struct {
		name     string
		interval time.Duration
		next     func() (time.Duration, bool)
		want     time.Duration
	}{
		{"nothing pending waits the interval", 5 * time.Minute, retry(0, false), 5 * time.Minute},
		{"a retry sooner than the interval wins", 5 * time.Minute, retry(30*time.Second, true), 30 * time.Second},
		{"a retry past the interval does not stretch it", time.Minute, retry(time.Hour, true), time.Minute},
		// An overdue retry reports zero. Without the floor the loop would run
		// cycles back to back, which a few hanging servers can cause on their
		// own because one cycle can outrun the backoff cap.
		{"an overdue retry is floored", 5 * time.Minute, retry(0, true), minCycleWait},
		// The floor never overrides a deliberately short interval.
		{"the floor never exceeds the interval", 20 * time.Millisecond, retry(0, true), 20 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cycleWait(tc.interval, tc.next); got != tc.want {
				t.Errorf("cycleWait(%v) = %v, want %v", tc.interval, got, tc.want)
			}
		})
	}
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

	rec.mu.Lock()
	bootForced := rec.forced[0]
	rec.mu.Unlock()
	if !bootForced {
		t.Error("the boot cycle must be forced, or a server inside its interval is never introspected")
	}

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
