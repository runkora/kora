package routescopes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// testRoutes and testScopes are the minimal valid payload; no test here is
// about their contents, only about how often they are sent.
var (
	testRoutes = []RouteScope{{PathPrefix: "/v1/thing", Method: "GET", RequiredScope: "thing:read"}}
	testScopes = []ScopeDeclaration{{Scope: "thing:read", ResourceType: "thing", PathSegments: []string{}, Action: "read"}}
)

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

	go KeepRegistered(ctx, logging.NewLogger(), "test-service", register)

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

// TestClientRegisterMakesOneAttemptPerCall pins where retry lives. The client
// must not retry: KeepRegistered is the only retry loop, and a second one
// underneath it stacks, so a single failing call could block for minutes and
// swallow a whole self-heal tick.
func TestClientRegisterMakesOneAttemptPerCall(t *testing.T) {
	var attempts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, logging.NewLogger())

	start := time.Now()
	if err := client.Register(t.Context(), "test-service", testRoutes, testScopes); err == nil {
		t.Fatal("expected the 503 to be returned to the caller")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("client made %d HTTP attempts for one Register call, want exactly 1", got)
	}
	// A retry loop in the client would have slept before its second attempt.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("a failing Register took %s: the client is backing off internally", elapsed)
	}
}

// TestKeepRegisteredRetriesThroughClient wires the two halves together over a
// real HTTP round trip: the client returns the failure immediately, and
// KeepRegistered is what comes back for another attempt.
//
// The time bound is the assertion that matters. The client used to apply a
// 15-second startup delay inside every register call and retry 15 times before
// returning, so a first attempt that failed could not have been followed by a
// successful second one within seconds. Recovering this quickly is only
// possible because neither layer adds a per-call delay any more.
func TestKeepRegisteredRetriesThroughClient(t *testing.T) {
	var attempts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Fail the first attempt, then succeed: the retry can only come from
		// KeepRegistered now that the client makes one attempt per call.
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, logging.NewLogger())
	register := func(ctx context.Context) error {
		return client.Register(ctx, "test-service", testRoutes, testScopes)
	}

	go KeepRegistered(t.Context(), logging.NewLogger(), "test-service", register)

	// One failure, one success. The bound is comfortably under the old 15s
	// per-call delay and the client's old multi-attempt loop, so reaching two
	// attempts here could not happen if either had survived. It is still well
	// clear of the 2s startup backoff between them.
	deadline := time.Now().Add(10 * time.Second)
	for attempts.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("expected a retried registration to succeed within 10s, got %d attempts", attempts.Load())
		}
		time.Sleep(time.Millisecond)
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
		KeepRegistered(ctx, logging.NewLogger(), "test-service", register)
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

// ── RepublishOnEvent ──────────────────────────────────────────────────────────

// TestRepublishOnEventDoesNotBlockCaller is the property that made this helper
// necessary: SetAgentTypeHook invokes its callback synchronously inside an
// agent-type write, so a callback that waited on a policy-service round trip
// would put that round trip on the write path.
func TestRepublishOnEventDoesNotBlockCaller(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	register := func(context.Context) error {
		close(entered)
		<-release
		return nil
	}

	republish := RepublishOnEvent(logging.NewLogger(), "test-service", register)

	returned := make(chan struct{})
	go func() {
		republish()
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("republish callback blocked its caller while register was in flight")
	}

	// The work is still running: prove the callback returned early rather than
	// because register had already finished.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("register was never invoked")
	}
	close(release)
}

// TestRepublishOnEventSurvivesFailure pins that a failing republish is logged
// and swallowed rather than panicking a hot path that has no way to handle it.
// The periodic tick is the backstop.
func TestRepublishOnEventFailureIsContained(t *testing.T) {
	called := make(chan struct{})
	register := func(context.Context) error {
		close(called)
		return errors.New("policy service down")
	}

	RepublishOnEvent(logging.NewLogger(), "test-service", register)()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("register was never invoked")
	}
	// Give the goroutine a moment to finish handling the error.
	time.Sleep(50 * time.Millisecond)
}
