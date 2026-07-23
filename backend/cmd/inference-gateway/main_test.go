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

	// Wait for at least 2 ticks.
	time.Sleep(55 * time.Millisecond)
	got := calls.Load()
	if got < 2 {
		t.Errorf("expected at least 2 registration calls in 55ms with 20ms interval, got %d", got)
	}

	// Cancel context and ensure no further calls accumulate.
	cancel()
	time.Sleep(30 * time.Millisecond)
	afterCancel := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != afterCancel {
		t.Errorf("registration called after context canceled: had %d, now %d", afterCancel, calls.Load())
	}
}
