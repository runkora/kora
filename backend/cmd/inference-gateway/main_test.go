package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// TestRunRegistrationLoop verifies that the registration function is called on
// each ticker tick and stops cleanly when the context is canceled.
func TestRunRegistrationLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int64
	register := func(_ context.Context) { calls.Add(1) }

	interval := 20 * time.Millisecond
	logger := logging.NewLogger()

	go runRegistrationLoop(ctx, register, interval, logger)

	// Poll until the loop has ticked at least twice. The bound is generous so a
	// slow or loaded CI runner cannot flake on scheduler delays — the test only
	// asserts that the ticker fires repeatedly, not that it hits a wall-clock
	// deadline.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("expected at least 2 registration calls within 2s (20ms interval), got %d", calls.Load())
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
