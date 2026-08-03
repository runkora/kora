package identityproxy

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	ext_procv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// discardLogger returns a logger that writes nothing.
func discardLogger() logging.Logger {
	return logging.NewLoggerWithOutput(io.Discard)
}

// testConfig returns a Config with fast timeouts suitable for unit tests.
// Values are deliberately shorter than the production settings in base.yaml so
// tests don't block for seconds waiting for retries or polls.
func testConfig() Config {
	return Config{
		SocketPath:        "/tmp/identity-proxy-test.sock",
		ReadyTimeout:      5 * time.Second,
		BackoffInitial:    10 * time.Millisecond,
		BackoffMax:        100 * time.Millisecond,
		PollInterval:      50 * time.Millisecond,
		RefreshThreshold:  60 * time.Second,
		HealthAddr:        ":19090",
		EnvoyReadyTimeout: 100 * time.Millisecond,
	}
}

// stubExchanger is a test double for tokenExchanger.
// It succeeds immediately unless failN > 0, in which case it returns an error
// for the first failN calls and then succeeds.
type stubExchanger struct {
	calls  atomic.Int32
	failN  int32 // fail this many times then succeed
	token  string
	expiry time.Time
	err    error // fixed error (overrides failN pattern when non-nil)
}

func (s *stubExchanger) Exchange(_ context.Context) (string, time.Time, error) {
	n := s.calls.Add(1)
	if s.err != nil {
		return "", time.Time{}, s.err
	}
	if n <= s.failN {
		return "", time.Time{}, errors.New("stub: transient failure")
	}
	tok := s.token
	if tok == "" {
		tok = "test-token"
	}
	exp := s.expiry
	if exp.IsZero() {
		exp = time.Now().Add(5 * time.Minute)
	}
	return tok, exp, nil
}

// ── TokenCache ────────────────────────────────────────────────────────────────

func TestTokenCache_EmptyGet(t *testing.T) {
	c := NewTokenCache()
	_, _, ok := c.Get()
	if ok {
		t.Fatal("expected empty cache to return ok=false")
	}
}

func TestTokenCache_SetGet(t *testing.T) {
	c := NewTokenCache()
	exp := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	c.Set("my-token", exp)

	tok, gotExp, ok := c.Get()
	if !ok {
		t.Fatal("expected ok=true after Set")
	}
	if tok != "my-token" {
		t.Fatalf("token: got %q, want %q", tok, "my-token")
	}
	if !gotExp.Equal(exp) {
		t.Fatalf("expiry: got %v, want %v", gotExp, exp)
	}
}

func TestTokenCache_IsExpiringSoon_True(t *testing.T) {
	c := NewTokenCache()
	// Token expires in 30 s — within the 60 s threshold.
	c.Set("tok", time.Now().Add(30*time.Second))
	if !c.IsExpiringSoon(60 * time.Second) {
		t.Fatal("expected IsExpiringSoon=true when token expires in 30s and threshold is 60s")
	}
}

func TestTokenCache_IsExpiringSoon_False(t *testing.T) {
	c := NewTokenCache()
	// Token expires in 5 min — outside the 60 s threshold.
	c.Set("tok", time.Now().Add(5*time.Minute))
	if c.IsExpiringSoon(60 * time.Second) {
		t.Fatal("expected IsExpiringSoon=false when token expires in 5 min and threshold is 60s")
	}
}

func TestTokenCache_IsExpiringSoon_Empty(t *testing.T) {
	c := NewTokenCache()
	// An empty cache has no expiry, so it should always be considered "expiring
	// soon" — we want to trigger an immediate re-exchange.
	if !c.IsExpiringSoon(60 * time.Second) {
		t.Fatal("expected IsExpiringSoon=true for empty cache")
	}
}

// ── Processor ─────────────────────────────────────────────────────────────────

func TestProcessor_HandleRequestHeaders_WithToken(t *testing.T) {
	cache := NewTokenCache()
	cache.Set("bearer-xyz", time.Now().Add(5*time.Minute))

	p := NewProcessor(cache, nil, discardLogger())
	resp := p.handleRequestHeaders(context.Background(), nil)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	rh := resp.GetRequestHeaders()
	if rh == nil {
		t.Fatal("expected RequestHeaders to be set in response")
	}
	mutation := rh.GetResponse().GetHeaderMutation()
	if mutation == nil {
		t.Fatal("expected HeaderMutation to be non-nil")
	}
	headers := mutation.GetSetHeaders()
	if len(headers) != 1 {
		t.Fatalf("expected 1 header in SetHeaders, got %d", len(headers))
	}
	h := headers[0].GetHeader()
	if h.GetKey() != authorizationHeader {
		t.Fatalf("header key: got %q, want %q", h.GetKey(), authorizationHeader)
	}
	want := "Bearer bearer-xyz"
	if string(h.GetRawValue()) != want {
		t.Fatalf("header value: got %q, want %q", string(h.GetRawValue()), want)
	}
}

func TestProcessor_HandleRequestHeaders_EmptyCache(t *testing.T) {
	cache := NewTokenCache() // nothing Set
	p := NewProcessor(cache, nil, discardLogger())
	resp := p.handleRequestHeaders(context.Background(), nil)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// Empty cache → CONTINUE response with RequestHeaders oneof set (no mutation).
	rh := resp.GetRequestHeaders()
	if rh == nil {
		t.Fatal("expected RequestHeaders oneof to be set in CONTINUE response")
	}
	if rh.GetResponse().GetStatus() != ext_procv3.CommonResponse_CONTINUE {
		t.Fatalf("expected CONTINUE status, got %v", rh.GetResponse().GetStatus())
	}
}

// ── Server.waitForInitialToken ────────────────────────────────────────────────

func TestServer_WaitForInitialToken_ImmediateSuccess(t *testing.T) {
	cache := NewTokenCache()
	exc := &stubExchanger{token: "tok-abc"}
	refresher := NewRefresher(cache, exc, discardLogger(), testConfig())
	srv := NewServer(cache, nil, exc, refresher, discardLogger(), testConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.waitForInitialToken(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tok, _, ok := cache.Get()
	if !ok {
		t.Fatal("expected cache to be populated after waitForInitialToken")
	}
	if tok != "tok-abc" {
		t.Fatalf("token: got %q, want %q", tok, "tok-abc")
	}
}

func TestServer_WaitForInitialToken_RetriesAndSucceeds(t *testing.T) {
	cache := NewTokenCache()
	// Fail 2 times, then succeed on the 3rd call.
	exc := &stubExchanger{failN: 2, token: "tok-retry"}
	refresher := NewRefresher(cache, exc, discardLogger(), testConfig())
	srv := NewServer(cache, nil, exc, refresher, discardLogger(), testConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.waitForInitialToken(ctx); err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if exc.calls.Load() != 3 {
		t.Fatalf("expected 3 exchange calls, got %d", exc.calls.Load())
	}
	tok, _, ok := cache.Get()
	if !ok || tok != "tok-retry" {
		t.Fatalf("cache state wrong: tok=%q ok=%v", tok, ok)
	}
}

func TestServer_WaitForInitialToken_ContextCancelled(t *testing.T) {
	cache := NewTokenCache()
	// Always fail so waitForInitialToken never exits on its own.
	exc := &stubExchanger{err: errors.New("always fail")}
	refresher := NewRefresher(cache, exc, discardLogger(), testConfig())
	srv := NewServer(cache, nil, exc, refresher, discardLogger(), testConfig())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := srv.waitForInitialToken(ctx)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

// ── Refresher ─────────────────────────────────────────────────────────────────

func TestRefresher_ExchangeWithBackoff_Success(t *testing.T) {
	cache := NewTokenCache()
	exc := &stubExchanger{token: "refreshed"}
	r := NewRefresher(cache, exc, discardLogger(), testConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	r.exchangeWithBackoff(ctx)

	tok, _, ok := cache.Get()
	if !ok || tok != "refreshed" {
		t.Fatalf("expected cache to contain 'refreshed', got %q ok=%v", tok, ok)
	}
}

func TestRefresher_ExchangeWithBackoff_CtxCancelled(t *testing.T) {
	cache := NewTokenCache()
	exc := &stubExchanger{err: errors.New("permanent")}
	r := NewRefresher(cache, exc, discardLogger(), testConfig())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	// Should return without blocking indefinitely.
	done := make(chan struct{})
	go func() {
		r.exchangeWithBackoff(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("exchangeWithBackoff did not return after context cancellation")
	}
}

func TestRefresher_Run_StopsOnCtxCancel(t *testing.T) {
	cache := NewTokenCache()
	exc := &stubExchanger{token: "bg"}
	r := NewRefresher(cache, exc, discardLogger(), testConfig())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

// ── svidWatcher ───────────────────────────────────────────────────────────────

// gatedExchanger blocks inside Exchange until release is closed, signalling on
// entered when the call is reached. It lets a test observe the cache state while
// a re-exchange is in flight.
type gatedExchanger struct {
	entered chan struct{}
	release chan struct{}
	token   string
}

func (g *gatedExchanger) Exchange(_ context.Context) (string, time.Time, error) {
	close(g.entered)
	<-g.release
	return g.token, time.Now().Add(5 * time.Minute), nil
}

// TestSVIDWatcher_OnX509ContextUpdate_KeepsValidTokenUntilSwap guards the
// regression fixed here: on SVID rotation the cache must keep serving the still
// valid token until the re-exchange atomically replaces it. Any empty window
// would let handleRequestHeaders forward requests with no Authorization header,
// which OPA fail-closed denies — surfacing to agents as intermittent
// "Connection reset by peer" on git clone and other egress.
func TestSVIDWatcher_OnX509ContextUpdate_KeepsValidTokenUntilSwap(t *testing.T) {
	cache := NewTokenCache()
	cache.Set("old-token", time.Now().Add(5*time.Minute))

	exc := &gatedExchanger{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		token:   "new-token",
	}
	r := NewRefresher(cache, exc, discardLogger(), testConfig())
	watcher := r.OnSVIDRotation(context.Background())

	// Simulate a SVID rotation. Passing nil is safe: OnX509ContextUpdate ignores
	// its X509Context argument entirely.
	watcher.OnX509ContextUpdate(nil)

	// Wait until the background re-exchange is in flight (blocked in Exchange).
	select {
	case <-exc.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("re-exchange was never triggered by SVID rotation")
	}

	// While the new token is being minted, the cache must still serve the valid
	// old token — never an empty window.
	if tok, _, ok := cache.Get(); !ok || tok != "old-token" {
		t.Fatalf("cache must keep serving the valid token during re-exchange, got tok=%q ok=%v", tok, ok)
	}

	// Let the exchange complete; the new token atomically replaces the old.
	close(exc.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tok, _, ok := cache.Get(); ok && tok == "new-token" {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	tok, _, ok := cache.Get()
	t.Fatalf("cache not repopulated after SVID rotation: tok=%q ok=%v", tok, ok)
}

func TestSVIDWatcher_OnX509ContextWatchError_DoesNotPanic(t *testing.T) {
	cache := NewTokenCache()
	exc := &stubExchanger{}
	r := NewRefresher(cache, exc, discardLogger(), testConfig())
	watcher := r.OnSVIDRotation(context.Background())

	// Must not panic or return an error.
	watcher.OnX509ContextWatchError(errors.New("simulated watch error"))
}
