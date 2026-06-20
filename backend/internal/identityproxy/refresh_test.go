package identityproxy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestRefresher_ExchangeWithBackoff_BackoffProgression validates that the
// backoff delay doubles on each retry until it hits the max.
func TestRefresher_ExchangeWithBackoff_BackoffProgression(t *testing.T) {
	cache := NewTokenCache()
	// Fail 3 times, then succeed on the 4th attempt.
	exc := &stubExchanger{failN: 3, token: "backoff-token"}
	cfg := testConfig()
	cfg.BackoffInitial = 10 * time.Millisecond
	cfg.BackoffMax = 50 * time.Millisecond
	r := NewRefresher(cache, exc, discardLogger(), cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	r.exchangeWithBackoff(ctx)
	elapsed := time.Since(start)

	// Expected backoffs: 10ms, 20ms, 40ms → total ~70ms + call overhead.
	// Allow reasonable margin for test flakiness.
	if elapsed < 60*time.Millisecond {
		t.Fatalf("backoff too fast: elapsed %v, expected ≥60ms", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("backoff too slow: elapsed %v, expected <500ms", elapsed)
	}

	tok, _, ok := cache.Get()
	if !ok || tok != "backoff-token" {
		t.Fatalf("cache state wrong: tok=%q ok=%v", tok, ok)
	}
}

// TestRefresher_ExchangeWithBackoff_MaxBackoffCap validates that backoff
// stops doubling once it reaches BackoffMax.
func TestRefresher_ExchangeWithBackoff_MaxBackoffCap(t *testing.T) {
	cache := NewTokenCache()
	// Fail many times to trigger multiple backoff iterations.
	exc := &stubExchanger{failN: 5, token: "max-backoff"}
	cfg := testConfig()
	cfg.BackoffInitial = 5 * time.Millisecond
	cfg.BackoffMax = 20 * time.Millisecond // cap at 20ms
	r := NewRefresher(cache, exc, discardLogger(), cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	r.exchangeWithBackoff(ctx)
	elapsed := time.Since(start)

	// Backoff sequence: 5ms, 10ms, 20ms, 20ms, 20ms → total ~75ms.
	// After hitting max, all subsequent backoffs should be 20ms.
	if elapsed < 60*time.Millisecond {
		t.Fatalf("backoff too fast: elapsed %v, expected ≥60ms", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("backoff too slow: elapsed %v", elapsed)
	}

	if exc.calls.Load() != 6 {
		t.Fatalf("expected 6 exchange calls, got %d", exc.calls.Load())
	}
}

// TestRefresher_ExchangeWithBackoff_EnvoyReadyGate validates that the
// refresher blocks until envoyReady is closed before making the first call.
func TestRefresher_ExchangeWithBackoff_EnvoyReadyGate(t *testing.T) {
	cache := NewTokenCache()
	exc := &stubExchanger{token: "gated-token"}
	cfg := testConfig()
	r := NewRefresher(cache, exc, discardLogger(), cfg)

	// Inject envoyReady channel (normally set by Server).
	envoyReady := make(chan struct{})
	r.envoyReady = envoyReady

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start exchange in background; it should block until envoyReady closes.
	done := make(chan struct{})
	go func() {
		r.exchangeWithBackoff(ctx)
		close(done)
	}()

	// Wait a bit to ensure exchangeWithBackoff is blocking.
	time.Sleep(50 * time.Millisecond)
	if exc.calls.Load() != 0 {
		t.Fatalf("expected 0 exchange calls before envoyReady, got %d", exc.calls.Load())
	}

	// Close envoyReady → exchange should proceed immediately.
	close(envoyReady)

	select {
	case <-done:
		// Success: exchangeWithBackoff returned.
	case <-time.After(2 * time.Second):
		t.Fatal("exchangeWithBackoff did not return after envoyReady closed")
	}

	tok, _, ok := cache.Get()
	if !ok || tok != "gated-token" {
		t.Fatalf("cache state wrong: tok=%q ok=%v", tok, ok)
	}
}

// TestRefresher_ExchangeWithBackoff_ContextCancelDuringGate validates that
// canceling the context while waiting for envoyReady unblocks immediately.
func TestRefresher_ExchangeWithBackoff_ContextCancelDuringGate(t *testing.T) {
	cache := NewTokenCache()
	exc := &stubExchanger{token: "never-called"}
	cfg := testConfig()
	r := NewRefresher(cache, exc, discardLogger(), cfg)

	envoyReady := make(chan struct{}) // never closed
	r.envoyReady = envoyReady

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.exchangeWithBackoff(ctx)
		close(done)
	}()

	// Cancel context while blocked on envoyReady.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Success: exchangeWithBackoff returned immediately.
	case <-time.After(2 * time.Second):
		t.Fatal("exchangeWithBackoff did not return after context cancel")
	}

	if exc.calls.Load() != 0 {
		t.Fatalf("expected 0 exchange calls after context cancel, got %d", exc.calls.Load())
	}
}

// TestRefresher_Run_PollTriggersRefresh validates that the background poll
// goroutine calls exchangeWithBackoff when IsExpiringSoon returns true.
func TestRefresher_Run_PollTriggersRefresh(t *testing.T) {
	cache := NewTokenCache()
	// Set a token that's close to expiry (within threshold).
	cache.Set("expiring", time.Now().Add(30*time.Second))

	var calls atomic.Int32
	exc := &stubExchanger{token: "refreshed"}
	// Wrap exchanger to track calls.
	trackedExc := &callTrackingExchanger{
		inner: exc,
		calls: &calls,
	}

	cfg := testConfig()
	cfg.PollInterval = 20 * time.Millisecond    // poll frequently
	cfg.RefreshThreshold = 60 * time.Second     // 30s token is within threshold
	r := NewRefresher(cache, trackedExc, discardLogger(), cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	r.Run(ctx)

	// After Run exits, check that at least one refresh was triggered.
	if calls.Load() < 1 {
		t.Fatalf("expected at least 1 exchange call from poll, got %d", calls.Load())
	}

	// Verify cache was updated.
	tok, _, ok := cache.Get()
	if !ok || tok != "refreshed" {
		t.Fatalf("cache not refreshed: tok=%q ok=%v", tok, ok)
	}
}

// TestRefresher_Run_NoRefreshWhenNotExpiring validates that the poll loop
// does NOT trigger a refresh when the token is fresh.
func TestRefresher_Run_NoRefreshWhenNotExpiring(t *testing.T) {
	cache := NewTokenCache()
	// Set a token that won't expire for a long time.
	cache.Set("fresh", time.Now().Add(10*time.Minute))

	var calls atomic.Int32
	exc := &stubExchanger{token: "should-not-be-called"}
	trackedExc := &callTrackingExchanger{
		inner: exc,
		calls: &calls,
	}

	cfg := testConfig()
	cfg.PollInterval = 20 * time.Millisecond
	cfg.RefreshThreshold = 60 * time.Second // fresh token is outside threshold
	r := NewRefresher(cache, trackedExc, discardLogger(), cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r.Run(ctx)

	// No refresh should have been triggered.
	if calls.Load() != 0 {
		t.Fatalf("expected 0 exchange calls for fresh token, got %d", calls.Load())
	}

	// Cache should still have original token.
	tok, _, ok := cache.Get()
	if !ok || tok != "fresh" {
		t.Fatalf("cache unexpectedly changed: tok=%q ok=%v", tok, ok)
	}
}

// TestRefresher_ExchangeWithBackoff_ImmediateRetryAfterTransientFailure
// validates that transient failures are retried immediately without giving up.
func TestRefresher_ExchangeWithBackoff_ImmediateRetryAfterTransientFailure(t *testing.T) {
	cache := NewTokenCache()
	// Fail once, then succeed.
	exc := &stubExchanger{failN: 1, token: "retry-success"}
	cfg := testConfig()
	cfg.BackoffInitial = 5 * time.Millisecond
	r := NewRefresher(cache, exc, discardLogger(), cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	r.exchangeWithBackoff(ctx)
	elapsed := time.Since(start)

	// Should retry after ~5ms and succeed immediately.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("retry took too long: elapsed %v", elapsed)
	}

	tok, _, ok := cache.Get()
	if !ok || tok != "retry-success" {
		t.Fatalf("cache state wrong: tok=%q ok=%v", tok, ok)
	}

	if exc.calls.Load() != 2 {
		t.Fatalf("expected 2 exchange calls (1 fail + 1 success), got %d", exc.calls.Load())
	}
}

// ── test helpers ──────────────────────────────────────────────────────────────

// callTrackingExchanger wraps a tokenExchanger to count calls.
type callTrackingExchanger struct {
	inner tokenExchanger
	calls *atomic.Int32
}

func (c *callTrackingExchanger) Exchange(ctx context.Context) (string, time.Time, error) {
	c.calls.Add(1)
	return c.inner.Exchange(ctx)
}
