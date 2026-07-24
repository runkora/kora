package identityproxy

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	ext_procv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"

	identityclient "github.com/clear-route/orbit/internal/platform/clients/identity"
)

// ── fakes and helpers ─────────────────────────────────────────────────────────

// stubFetcher scripts GetDelegationForRun responses per call.
type stubFetcher struct {
	mu    sync.Mutex
	calls int
	// token/expiresIn returned on success; err overrides.
	token     string
	expiresIn int
	err       error
}

func (s *stubFetcher) GetDelegationForRun(_ context.Context, runID string) (*identityclient.DelegationToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &identityclient.DelegationToken{DelegationToken: s.token, ExpiresIn: s.expiresIn, RunID: runID}, nil
}

func (s *stubFetcher) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubFetcher) set(token string, expiresIn int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token, s.expiresIn, s.err = token, expiresIn, err
}

// newTestManager wires a manager with a controllable clock.
func newTestManager(fetcher delegationFetcher) (*DelegationManager, *time.Time) {
	m := NewDelegationManager(fetcher, discardLogger(), testConfig())
	now := time.Now()
	m.now = func() time.Time { return now }
	return m, &now
}

// reqHeaders builds an ext_proc HttpHeaders message from key/value pairs.
func reqHeaders(pairs map[string]string) *ext_procv3.HttpHeaders {
	hm := &corev3.HeaderMap{}
	for k, v := range pairs {
		hm.Headers = append(hm.Headers, &corev3.HeaderValue{Key: k, RawValue: []byte(v)})
	}
	return &ext_procv3.HttpHeaders{Headers: hm}
}

// mutationOf extracts the header mutation from a ProcessingResponse.
func mutationOf(t *testing.T, resp *ext_procv3.ProcessingResponse) *ext_procv3.HeaderMutation {
	t.Helper()
	rh := resp.GetRequestHeaders()
	if rh == nil {
		t.Fatal("expected RequestHeaders oneof")
	}
	return rh.GetResponse().GetHeaderMutation()
}

// setHeaderValue returns the mutation's set value for key, or "" if unset.
func setHeaderValue(mutation *ext_procv3.HeaderMutation, key string) string {
	for _, h := range mutation.GetSetHeaders() {
		if h.GetHeader().GetKey() == key {
			return string(h.GetHeader().GetRawValue())
		}
	}
	return ""
}

func removesHeader(mutation *ext_procv3.HeaderMutation, key string) bool {
	return slices.Contains(mutation.GetRemoveHeaders(), key)
}

// ── DelegationManager: path mode, caching, refresh ────────────────────────────

func TestDelegationManager_PathMode_FetchAndCache(t *testing.T) {
	fetcher := &stubFetcher{token: "tok-a", expiresIn: 3600}
	m, _ := newTestManager(fetcher)

	tok, ok := m.TokenForPath(context.Background(), "/run/run-a/v1/messages")
	if !ok || tok != "tok-a" {
		t.Fatalf("TokenForPath = (%q, %v), want (tok-a, true)", tok, ok)
	}
	// Second request: served from cache, no second fetch.
	tok, ok = m.TokenForPath(context.Background(), "/run/run-a/v1/messages")
	if !ok || tok != "tok-a" {
		t.Fatalf("cached TokenForPath = (%q, %v), want (tok-a, true)", tok, ok)
	}
	if fetcher.callCount() != 1 {
		t.Errorf("fetch calls = %d, want 1 (once per run, cached)", fetcher.callCount())
	}
}

func TestDelegationManager_NoRunPrefix_NoToken(t *testing.T) {
	fetcher := &stubFetcher{token: "tok-a", expiresIn: 3600}
	m, _ := newTestManager(fetcher)

	if _, ok := m.TokenForPath(context.Background(), "/v1/messages"); ok {
		t.Fatal("expected no token for a request without a run prefix")
	}
	if fetcher.callCount() != 0 {
		t.Errorf("fetch calls = %d, want 0", fetcher.callCount())
	}
}

func TestDelegationManager_NegativeCache(t *testing.T) {
	fetcher := &stubFetcher{err: identityclient.ErrDelegationNotFound}
	m, now := newTestManager(fetcher)

	if _, ok := m.TokenForPath(context.Background(), "/run/run-auto/v1/messages"); ok {
		t.Fatal("expected no token for an autonomous run")
	}
	// Within the negative TTL: no re-fetch.
	if _, ok := m.TokenForPath(context.Background(), "/run/run-auto/v1/messages"); ok {
		t.Fatal("expected negative cache to hold")
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("fetch calls = %d, want 1 (negative-cached)", fetcher.callCount())
	}

	// Past the negative TTL the manager re-consults identity — the TTL bounds
	// how long a stale "autonomous" answer can shadow a live issuance.
	*now = now.Add(6 * time.Minute)
	fetcher.set("tok-late", 3600, nil)
	tok, ok := m.TokenForPath(context.Background(), "/run/run-auto/v1/messages")
	if !ok || tok != "tok-late" {
		t.Fatalf("post-TTL TokenForPath = (%q, %v), want (tok-late, true)", tok, ok)
	}
	if fetcher.callCount() != 2 {
		t.Errorf("fetch calls = %d, want 2", fetcher.callCount())
	}
}

func TestDelegationManager_RefreshNearExpiry(t *testing.T) {
	fetcher := &stubFetcher{token: "tok-1", expiresIn: 100}
	m, now := newTestManager(fetcher)

	if tok, ok := m.tokenForRun(context.Background(), "run-a"); !ok || tok != "tok-1" {
		t.Fatalf("initial fetch = (%q, %v)", tok, ok)
	}

	// 50% remaining: no refresh yet.
	*now = now.Add(50 * time.Second)
	if tok, _ := m.tokenForRun(context.Background(), "run-a"); tok != "tok-1" {
		t.Fatalf("mid-life token = %q, want tok-1", tok)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("fetch calls = %d, want 1 (no refresh at 50%% remaining)", fetcher.callCount())
	}

	// <20% remaining: re-fetch; the endpoint re-signs (jti stable, exp fresh).
	fetcher.set("tok-2", 100, nil)
	*now = now.Add(35 * time.Second) // 15s of 100s left
	tok, ok := m.tokenForRun(context.Background(), "run-a")
	if !ok || tok != "tok-2" {
		t.Fatalf("near-expiry token = (%q, %v), want (tok-2, true)", tok, ok)
	}
	if fetcher.callCount() != 2 {
		t.Errorf("fetch calls = %d, want 2 (refreshed)", fetcher.callCount())
	}
}

func TestDelegationManager_RefreshFailureKeepsValidToken(t *testing.T) {
	fetcher := &stubFetcher{token: "tok-1", expiresIn: 100}
	m, now := newTestManager(fetcher)

	if _, ok := m.tokenForRun(context.Background(), "run-a"); !ok {
		t.Fatal("initial fetch failed")
	}

	// Refresh window, but identity is down: keep serving the still-valid token.
	fetcher.set("", 0, errors.New("identity unreachable"))
	*now = now.Add(85 * time.Second)
	tok, ok := m.tokenForRun(context.Background(), "run-a")
	if !ok || tok != "tok-1" {
		t.Fatalf("token after failed refresh = (%q, %v), want (tok-1, true)", tok, ok)
	}

	// Once actually expired with retrieval still failing: attach nothing.
	*now = now.Add(30 * time.Second)
	if _, ok := m.tokenForRun(context.Background(), "run-a"); ok {
		t.Fatal("expected no token once expired and unrefreshable (fail closed)")
	}
}

func TestDelegationManager_ErrorBackoff(t *testing.T) {
	fetcher := &stubFetcher{err: errors.New("boom")}
	m, now := newTestManager(fetcher)

	if _, ok := m.tokenForRun(context.Background(), "run-a"); ok {
		t.Fatal("expected no token on retrieval error")
	}
	// Immediately again: backoff gate holds, no second fetch.
	if _, ok := m.tokenForRun(context.Background(), "run-a"); ok {
		t.Fatal("expected no token during backoff")
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("fetch calls = %d, want 1 (backoff gate)", fetcher.callCount())
	}

	// Past the backoff: retried, and success attaches.
	fetcher.set("tok-ok", 3600, nil)
	*now = now.Add(time.Second)
	tok, ok := m.tokenForRun(context.Background(), "run-a")
	if !ok || tok != "tok-ok" {
		t.Fatalf("post-backoff = (%q, %v), want (tok-ok, true)", tok, ok)
	}
}

// ── Sandbox mode ──────────────────────────────────────────────────────────────

func TestDelegationManager_SandboxMode_AttachesOnAllPaths(t *testing.T) {
	fetcher := &stubFetcher{token: "tok-sbx", expiresIn: 3600}
	m, _ := newTestManager(fetcher)
	m.SetSandboxRunID(context.Background(), "run-sbx")

	// No run prefix — sandbox identity still selects the run.
	tok, ok := m.TokenForPath(context.Background(), "/v1/messages")
	if !ok || tok != "tok-sbx" {
		t.Fatalf("sandbox TokenForPath = (%q, %v), want (tok-sbx, true)", tok, ok)
	}
	// A path asserting a DIFFERENT run: the attested sandbox identity wins.
	tok, ok = m.TokenForPath(context.Background(), "/run/other-run/v1/messages")
	if !ok || tok != "tok-sbx" {
		t.Fatalf("sandbox TokenForPath (foreign prefix) = (%q, %v), want (tok-sbx, true)", tok, ok)
	}
	if fetcher.callCount() != 1 {
		t.Errorf("fetch calls = %d, want 1 (single sandbox run)", fetcher.callCount())
	}
}

func TestDelegationSVIDWatcher_ExtractsSandboxRunID(t *testing.T) {
	m, _ := newTestManager(&stubFetcher{})
	watcher := m.SVIDWatcher(context.Background())

	id := spiffeid.RequireFromString("spiffe://orbit.test/tenant/orbit-tenant-t1/agent/coder/sandbox/run-9")
	watcher.OnX509ContextUpdate(&workloadapi.X509Context{SVIDs: []*x509svid.SVID{{ID: id}}})
	if got := m.SandboxRunID(); got != "run-9" {
		t.Fatalf("SandboxRunID = %q, want run-9", got)
	}
}

func TestDelegationSVIDWatcher_SharedPodSVIDIgnored(t *testing.T) {
	m, _ := newTestManager(&stubFetcher{})
	watcher := m.SVIDWatcher(context.Background())

	id := spiffeid.RequireFromString("spiffe://orbit.test/tenant/orbit-tenant-t1/agent/coder")
	watcher.OnX509ContextUpdate(&workloadapi.X509Context{SVIDs: []*x509svid.SVID{{ID: id}}})
	if got := m.SandboxRunID(); got != "" {
		t.Fatalf("SandboxRunID = %q, want empty for a shared-pod SVID", got)
	}
}

// ── Processor integration ─────────────────────────────────────────────────────

func TestProcessor_AttachesDelegationHeader_PathMode(t *testing.T) {
	cache := NewTokenCache()
	cache.Set("platform-token", time.Now().Add(5*time.Minute))
	m, _ := newTestManager(&stubFetcher{token: "delegation-token", expiresIn: 3600})
	p := NewProcessor(cache, m, discardLogger())

	resp := p.handleRequestHeaders(context.Background(), reqHeaders(map[string]string{":path": "/run/run-a/v1/messages"}))
	mutation := mutationOf(t, resp)

	if got := setHeaderValue(mutation, authorizationHeader); got != "Bearer platform-token" {
		t.Errorf("authorization = %q, want Bearer platform-token", got)
	}
	if got := setHeaderValue(mutation, delegationHeader); got != "delegation-token" {
		t.Errorf("delegation header = %q, want delegation-token", got)
	}
	if !removesHeader(mutation, delegationHeader) {
		t.Error("inbound delegation header must be stripped before ours is set")
	}
}

func TestProcessor_NoRunPrefix_NoDelegationHeader(t *testing.T) {
	cache := NewTokenCache()
	cache.Set("platform-token", time.Now().Add(5*time.Minute))
	m, _ := newTestManager(&stubFetcher{token: "delegation-token", expiresIn: 3600})
	p := NewProcessor(cache, m, discardLogger())

	resp := p.handleRequestHeaders(context.Background(), reqHeaders(map[string]string{":path": "/v1/messages"}))
	mutation := mutationOf(t, resp)

	if got := setHeaderValue(mutation, delegationHeader); got != "" {
		t.Errorf("delegation header = %q, want unset without a run prefix", got)
	}
	if !removesHeader(mutation, delegationHeader) {
		t.Error("inbound delegation header must always be stripped")
	}
}

// The subprocess must not be able to inject a delegation token: any inbound
// value is removed, and only the proxy's own retrieval sets one.
func TestProcessor_StripsInboundDelegationHeader(t *testing.T) {
	cache := NewTokenCache()
	cache.Set("platform-token", time.Now().Add(5*time.Minute))
	// Autonomous run: identity says no delegation exists.
	m, _ := newTestManager(&stubFetcher{err: identityclient.ErrDelegationNotFound})
	p := NewProcessor(cache, m, discardLogger())

	resp := p.handleRequestHeaders(context.Background(), reqHeaders(map[string]string{
		":path":                    "/run/run-a/v1/messages",
		"x-orbit-delegation-token": "forged-by-subprocess",
	}))
	mutation := mutationOf(t, resp)

	if !removesHeader(mutation, delegationHeader) {
		t.Fatal("forged inbound delegation header must be stripped")
	}
	if got := setHeaderValue(mutation, delegationHeader); got != "" {
		t.Errorf("delegation header = %q, want unset (autonomous run)", got)
	}
}

func TestProcessor_EmptyCache_StillStripsDelegationHeader(t *testing.T) {
	cache := NewTokenCache() // no platform token yet
	p := NewProcessor(cache, nil, discardLogger())

	resp := p.handleRequestHeaders(context.Background(), reqHeaders(map[string]string{
		":path":                    "/run/run-a/v1/messages",
		"x-orbit-delegation-token": "forged",
	}))
	mutation := mutationOf(t, resp)
	if !removesHeader(mutation, delegationHeader) {
		t.Fatal("delegation header must be stripped even while the token cache is warming")
	}
}
