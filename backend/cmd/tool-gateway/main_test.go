package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestRetryDelay(t *testing.T) {
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute}
	for i, w := range want {
		if got := retryDelay(i + 1); got != w {
			t.Errorf("retryDelay(%d) = %s, want %s", i+1, got, w)
		}
	}
}

// TestRunIntrospectionLoop_RetriesBetweenTicks retries an unreached server before
// the hour-long tick and stops once it is reached. It takes the first 2s delay.
func TestRunIntrospectionLoop_RetriesBetweenTicks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var retries, cycles atomic.Int32
	var reached atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		runIntrospectionLoop(ctx,
			func(context.Context, bool) { cycles.Add(1) },
			func(context.Context) {
				if retries.Add(1) == 1 {
					reached.Store(true)
				}
			},
			func() bool { return !reached.Load() },
			nil, time.Hour, logging.NewLogger())
	}()

	deadline := time.After(10 * time.Second)
	for !reached.Load() {
		select {
		case <-deadline:
			t.Fatalf("no retry before the tick: retries=%d", retries.Load())
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	<-done
	if got := retries.Load(); got != 1 {
		t.Errorf("retries = %d, want 1 (stop once reached)", got)
	}
	if got := cycles.Load(); got != 1 {
		t.Errorf("cycles = %d, want only the boot cycle", got)
	}
}
