package routescopes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// TestDefaultInitialDelayIsNonZero pins the startup grace that every caller
// wiring a periodic re-registration loop has to opt out of. It exists so that a
// change to the default is a deliberate one: the delay is paid on EVERY
// Register call, not only the first, so a client left on the default would add
// it to each self-heal tick.
func TestDefaultInitialDelayIsNonZero(t *testing.T) {
	c := NewClient("http://policy.invalid", logging.NewLogger())
	if c.initialDelay != defaultInitialDelay {
		t.Fatalf("expected default initial delay %s, got %s", defaultInitialDelay, c.initialDelay)
	}
	if defaultInitialDelay == 0 {
		t.Fatal("defaultInitialDelay is 0: the sidecar startup grace has been removed")
	}
}

// TestWithInitialDelayZeroRegistersPromptlyEveryCall is the regression test for
// the self-heal loop: a client built with WithInitialDelay(0) must not pay the
// startup grace on any call, including the second and later ones a ticker
// drives. Left on the default, each of these would take 15 seconds.
func TestWithInitialDelayZeroRegistersPromptlyEveryCall(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, logging.NewLogger(), WithInitialDelay(0))

	// Three sequential registrations. The bound is well under the 15s default
	// grace for even one call, so it cannot pass if the option is dropped.
	start := time.Now()
	for i := range 3 {
		if err := client.Register(t.Context(), "test-service",
			[]RouteScope{{PathPrefix: "/v1/thing", Method: "GET", RequiredScope: "thing:read"}},
			[]ScopeDeclaration{{Scope: "thing:read", ResourceType: "thing", PathSegments: []string{}, Action: "read"}},
		); err != nil {
			t.Fatalf("registration %d failed: %v", i+1, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("3 registrations took %s: the initial delay is being applied per call", elapsed)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("expected 3 requests at the policy service, got %d", got)
	}
}

// TestKeepRegisteredSelfHealsThroughZeroDelayClient wires the two halves the
// newly-converted binaries use together — a zero-initial-delay client driven by
// KeepRegistered — and asserts the loop actually re-publishes on its interval.
// With the client left on the default grace, no tick would land inside the
// bound.
func TestKeepRegisteredSelfHealsThroughZeroDelayClient(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, logging.NewLogger(), WithInitialDelay(0))
	register := func(ctx context.Context) error {
		return client.Register(ctx, "test-service",
			[]RouteScope{{PathPrefix: "/v1/thing", Method: "GET", RequiredScope: "thing:read"}},
			[]ScopeDeclaration{{Scope: "thing:read", ResourceType: "thing", PathSegments: []string{}, Action: "read"}},
		)
	}

	ctx := t.Context()
	go KeepRegistered(ctx, logging.NewLogger(), "test-service", 20*time.Millisecond, register)

	// Startup registration plus at least two ticks. The bound is generous for a
	// loaded CI runner but far below the 15s the default grace would cost.
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("expected at least 3 registrations within 5s (20ms interval), got %d", hits.Load())
		}
		time.Sleep(time.Millisecond)
	}
}
