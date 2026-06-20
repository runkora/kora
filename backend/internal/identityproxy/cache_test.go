package identityproxy

import (
	"sync"
	"testing"
	"time"
)

// TestTokenCache_ExpiredToken verifies that Get returns ok=false when the
// cached token has already expired (time.Now() > expiry).
func TestTokenCache_ExpiredToken(t *testing.T) {
	c := NewTokenCache()
	// Set a token that expired 1 second ago.
	c.Set("expired-token", time.Now().Add(-1*time.Second))

	_, _, ok := c.Get()
	if ok {
		t.Fatal("expected ok=false for expired token")
	}
}

// TestTokenCache_ExactExpiryBoundary verifies that a token expiring at exactly
// time.Now() is treated as expired (boundary is exclusive).
func TestTokenCache_ExactExpiryBoundary(t *testing.T) {
	c := NewTokenCache()
	now := time.Now()
	c.Set("boundary-token", now)

	// Sleep a tiny bit to ensure time.Now() in Get is after the expiry.
	time.Sleep(1 * time.Millisecond)

	_, _, ok := c.Get()
	if ok {
		t.Fatal("expected ok=false when current time equals expiry")
	}
}

// TestTokenCache_ConcurrentReads validates that multiple goroutines can read
// from the cache simultaneously without blocking or data races.
func TestTokenCache_ConcurrentReads(t *testing.T) {
	c := NewTokenCache()
	exp := time.Now().Add(10 * time.Minute)
	c.Set("concurrent-token", exp)

	const numReaders = 100
	var wg sync.WaitGroup
	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			tok, gotExp, ok := c.Get()
			if !ok {
				t.Error("expected ok=true during concurrent read")
				return
			}
			if tok != "concurrent-token" {
				t.Errorf("token: got %q, want %q", tok, "concurrent-token")
			}
			if !gotExp.Equal(exp) {
				t.Errorf("expiry mismatch: got %v, want %v", gotExp, exp)
			}
		}()
	}

	wg.Wait()
}

// TestTokenCache_ConcurrentReadsDuringWrite validates that reads happening
// during a write do not panic and either see the old or new value (both valid).
func TestTokenCache_ConcurrentReadsDuringWrite(t *testing.T) {
	c := NewTokenCache()
	c.Set("initial", time.Now().Add(10*time.Minute))

	const numIterations = 1000
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer goroutine: repeatedly update the token.
	go func() {
		defer wg.Done()
		for i := 0; i < numIterations; i++ {
			c.Set("updated", time.Now().Add(10*time.Minute))
			time.Sleep(1 * time.Microsecond)
		}
	}()

	// Reader goroutine: repeatedly read the token.
	go func() {
		defer wg.Done()
		for i := 0; i < numIterations; i++ {
			tok, _, ok := c.Get()
			if !ok {
				t.Error("expected ok=true during concurrent write")
				return
			}
			// Valid tokens are either "initial" or "updated".
			if tok != "initial" && tok != "updated" {
				t.Errorf("unexpected token value: %q", tok)
			}
		}
	}()

	wg.Wait()
}

// TestTokenCache_InvalidateRaceWithGet validates that Invalidate and Get can
// be called concurrently without deadlock or panic.
func TestTokenCache_InvalidateRaceWithGet(t *testing.T) {
	c := NewTokenCache()
	c.Set("race-token", time.Now().Add(10*time.Minute))

	const numIterations = 1000
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < numIterations; i++ {
			c.Invalidate()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < numIterations; i++ {
			c.Get()
		}
	}()

	wg.Wait()
}

// TestTokenCache_MultipleInvalidate verifies that calling Invalidate multiple
// times in succession is safe and idempotent.
func TestTokenCache_MultipleInvalidate(t *testing.T) {
	c := NewTokenCache()
	c.Set("token", time.Now().Add(5*time.Minute))

	c.Invalidate()
	c.Invalidate()
	c.Invalidate()

	_, _, ok := c.Get()
	if ok {
		t.Fatal("expected ok=false after multiple Invalidate calls")
	}
}

// TestTokenCache_SetAfterInvalidate verifies that Set works correctly after
// the cache has been invalidated.
func TestTokenCache_SetAfterInvalidate(t *testing.T) {
	c := NewTokenCache()
	c.Set("first", time.Now().Add(5*time.Minute))
	c.Invalidate()

	exp := time.Now().Add(10 * time.Minute)
	c.Set("second", exp)

	tok, gotExp, ok := c.Get()
	if !ok {
		t.Fatal("expected ok=true after Set following Invalidate")
	}
	if tok != "second" {
		t.Fatalf("token: got %q, want %q", tok, "second")
	}
	if !gotExp.Equal(exp) {
		t.Fatalf("expiry: got %v, want %v", gotExp, exp)
	}
}

// TestTokenCache_IsExpiringSoon_ExactThreshold verifies boundary behavior when
// the token expiry exactly matches the threshold (should return true).
func TestTokenCache_IsExpiringSoon_ExactThreshold(t *testing.T) {
	c := NewTokenCache()
	// Token expires in exactly 60 seconds.
	c.Set("threshold-token", time.Now().Add(60*time.Second))

	// Threshold is 60 seconds, so time.Now().Add(60s) ≥ expiry → expiring soon.
	if !c.IsExpiringSoon(60 * time.Second) {
		t.Fatal("expected IsExpiringSoon=true when expiry exactly matches threshold")
	}
}

// TestTokenCache_IsExpiringSoon_ZeroThreshold checks that a zero threshold
// means "expiring immediately" — should return true unless expiry is in the future.
func TestTokenCache_IsExpiringSoon_ZeroThreshold(t *testing.T) {
	c := NewTokenCache()
	c.Set("future-token", time.Now().Add(5*time.Minute))

	// Zero threshold: only return true if token is already expired.
	// Since expiry is 5 minutes away, should be false.
	if c.IsExpiringSoon(0) {
		t.Fatal("expected IsExpiringSoon=false with zero threshold and future expiry")
	}
}

// TestTokenCache_IsExpiringSoon_NegativeThreshold verifies that negative
// thresholds behave sensibly (treat as checking "already expired").
func TestTokenCache_IsExpiringSoon_NegativeThreshold(t *testing.T) {
	c := NewTokenCache()
	// Token expires 2 minutes from now.
	c.Set("future", time.Now().Add(2*time.Minute))

	// Negative threshold means "already past expiry minus threshold" → false.
	if c.IsExpiringSoon(-1 * time.Minute) {
		t.Fatal("expected IsExpiringSoon=false with negative threshold")
	}
}

// TestTokenCache_GetReturnsZeroTimeOnFailure verifies that Get returns a zero
// time.Time when the cache is empty or expired (not an arbitrary time).
func TestTokenCache_GetReturnsZeroTimeOnFailure(t *testing.T) {
	c := NewTokenCache()

	_, expiry, ok := c.Get()
	if ok {
		t.Fatal("expected ok=false for empty cache")
	}
	if !expiry.IsZero() {
		t.Fatalf("expected zero time for empty cache, got %v", expiry)
	}

	// Also check after expiration.
	c.Set("expired", time.Now().Add(-1*time.Second))
	_, expiry, ok = c.Get()
	if ok {
		t.Fatal("expected ok=false for expired token")
	}
	if !expiry.IsZero() {
		t.Fatalf("expected zero time for expired token, got %v", expiry)
	}
}

// TestTokenCache_EmptyString verifies that an empty string token is treated
// as invalid (ok=false) even if expiry is in the future.
func TestTokenCache_EmptyString(t *testing.T) {
	c := NewTokenCache()
	c.Set("", time.Now().Add(10*time.Minute))

	_, _, ok := c.Get()
	if ok {
		t.Fatal("expected ok=false when token is empty string")
	}
}
