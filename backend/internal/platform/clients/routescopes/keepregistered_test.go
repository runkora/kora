package routescopes

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// TestKeepRegisteredTicks verifies that after the initial registration the
// register function is called on each ticker tick and stops cleanly when the
// context is canceled.
func TestKeepRegisteredTicks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int64
	register := func(context.Context) error {
		calls.Add(1)
		return nil
	}

	interval := 20 * time.Millisecond
	go KeepRegistered(ctx, logging.NewLogger(), "test-service", interval, register)

	// Poll until the loop has registered at startup and ticked at least twice.
	// The bound is generous so a slow or loaded CI runner cannot flake on
	// scheduler delays — the test only asserts that the ticker fires
	// repeatedly, not that it hits a wall-clock deadline.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("expected at least 3 registration calls within 2s (20ms interval), got %d", calls.Load())
		}
		time.Sleep(time.Millisecond)
	}

	// Cancel and assert the loop stops. Wait a few intervals for any in-flight
	// tick to settle, snapshot the count, then confirm it does not advance.
	cancel()
	time.Sleep(5 * interval)
	stopped := calls.Load()
	time.Sleep(5 * interval)
	if got := calls.Load(); got != stopped {
		t.Errorf("registration kept firing after context cancel: had %d, now %d", stopped, got)
	}
}

// TestKeepRegisteredRetriesStartup verifies the startup phase keeps retrying a
// failing registration rather than giving up, and settles into the periodic
// phase once it succeeds.
func TestKeepRegisteredRetriesStartup(t *testing.T) {
	ctx := t.Context()

	var calls atomic.Int64
	register := func(context.Context) error {
		// Fail the first attempt; the loop must come back for a second.
		if calls.Add(1) < 2 {
			return errors.New("policy service still starting")
		}
		return nil
	}

	go KeepRegistered(ctx, logging.NewLogger(), "test-service", time.Hour, register)

	// The first retry delay is startupBackoffBase (2s), so allow well past it.
	// Interval is an hour: reaching 2 calls proves the retry came from the
	// startup phase, not the ticker.
	deadline := time.Now().Add(10 * time.Second)
	for calls.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("expected startup phase to retry to 2 calls, got %d", calls.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestKeepRegisteredStopsDuringStartupBackoff verifies cancellation is honored
// while waiting out a startup retry delay.
func TestKeepRegisteredStopsDuringStartupBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int64
	register := func(context.Context) error {
		calls.Add(1)
		return errors.New("always failing")
	}

	done := make(chan struct{})
	go func() {
		KeepRegistered(ctx, logging.NewLogger(), "test-service", time.Hour, register)
		close(done)
	}()

	// Let the first attempt happen, then cancel mid-backoff.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("expected an initial registration attempt, got %d", calls.Load())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("KeepRegistered did not return after context cancel during startup backoff")
	}
}
