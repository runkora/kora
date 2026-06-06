package egressgw

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ── stubs ─────────────────────────────────────────────────────────────────────

type stubEvaluator struct {
	allow  bool
	reason string
	err    error

	mu    sync.Mutex
	calls []policy.CheckInput
}

func (s *stubEvaluator) Check(_ context.Context, input policy.CheckInput) (policy.Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, input)
	s.mu.Unlock()
	if s.err != nil {
		return policy.Result{}, s.err
	}
	return policy.Result{Allow: s.allow, Reason: s.reason}, nil
}
func (*stubEvaluator) Query(context.Context, policy.QueryInput) ([]string, error) { return nil, nil }
func (*stubEvaluator) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	return nil, nil
}
func (*stubEvaluator) ScopesExplain(context.Context, policy.ScopesInput) (*policy.ScopesExplainResult, error) {
	return nil, nil
}
func (*stubEvaluator) Start(context.Context) {}

type memConnRepo struct {
	items map[string]*credentials.Connection // key: tenant|name
}

func (m *memConnRepo) Create(_ context.Context, c *credentials.Connection) error {
	// Mirror the Postgres impl: generate an ID if the caller didn't set one,
	// so handlers that expect Create to populate c.ID work the same against
	// the in-memory fixture.
	if c.ID == "" {
		c.ID = "conn-" + c.TenantID + "-" + c.Name
	}
	m.items[c.TenantID+"|"+c.Name] = c
	return nil
}
func (m *memConnRepo) GetByID(_ context.Context, id, tenantID string) (*credentials.Connection, error) {
	for _, c := range m.items {
		if c.ID == id && c.TenantID == tenantID {
			return c, nil
		}
	}
	return nil, credentials.ErrConnectionNotFound
}
func (m *memConnRepo) GetByName(_ context.Context, tenantID, name string) (*credentials.Connection, error) {
	c, ok := m.items[tenantID+"|"+name]
	if !ok {
		return nil, credentials.ErrConnectionNotFound
	}
	return c, nil
}
func (m *memConnRepo) GetByHost(_ context.Context, tenantID, host string) (*credentials.Connection, error) {
	for _, c := range m.items {
		if c.TenantID == tenantID && c.BaseHost == host {
			return c, nil
		}
	}
	return nil, credentials.ErrConnectionNotFound
}
func (m *memConnRepo) ListByTenant(_ context.Context, tenantID string) ([]*credentials.Connection, error) {
	var out []*credentials.Connection
	for _, c := range m.items {
		if c.TenantID == tenantID {
			out = append(out, c)
		}
	}
	return out, nil
}
func (m *memConnRepo) Delete(_ context.Context, id, tenantID string) error {
	for k, c := range m.items {
		if c.ID == id && c.TenantID == tenantID {
			delete(m.items, k)
			return nil
		}
	}
	return credentials.ErrConnectionNotFound
}

type stubSecrets struct{ pem []byte }

func (s stubSecrets) Resolve(context.Context, string) ([]byte, error) { return s.pem, nil }

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// claimsFor builds a verified-style PlatformClaims for a tenant + agent + run.
func claimsFor(tenant, principal, run string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: principal},
		Orbit: token.OrbitClaims{
			TenantID:      tenant,
			PrincipalType: token.PrincipalTypeAgent,
			Claims:        map[string]string{"run_id": run},
		},
	}
}

// ── end-to-end fixture ────────────────────────────────────────────────────────
//
// In production the agent calls https://github.com/... directly; the gateway's
// TLS terminator sees Host: github.com and the broker's GetByHost("github.com")
// resolves the connection. The test simulates that by registering the
// connection with base_host equal to the upstream httptest server's host:port,
// and setting r.Host to the same value on inbound requests.

type fixture struct {
	t         *testing.T
	logger    logging.Logger
	eval      *stubEvaluator
	repo      *memConnRepo
	registry  *AdapterRegistry
	broker    *Broker
	gateway   *httptest.Server
	githubAPI *httptest.Server
	upstream  *httptest.Server

	// connectionHost is the upstream's host:port — the connection's base_host,
	// and the value tests set on req.Host.
	connectionHost string

	// captured by the upstream stub on each forwarded request
	upstreamSawAuth   string
	upstreamSawHost   string
	upstreamSawMethod string
	upstreamSawPath   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logger := logging.NewLoggerWithOutput(io.Discard)
	f := &fixture{t: t, logger: logger}

	// Upstream — receives forwarded requests; records what it saw and replies 200.
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.upstreamSawAuth = r.Header.Get("Authorization")
		f.upstreamSawHost = r.Host
		f.upstreamSawMethod = r.Method
		f.upstreamSawPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	f.upstream = upstream
	f.connectionHost = strings.TrimPrefix(upstream.URL, "https://")

	// GitHub API stub — exchanges the App JWT for an installation token.
	githubAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":                "ghs_phase2a",
			"expires_at":           time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"permissions":          map[string]string{"contents": "write"},
			"repository_selection": "selected",
		})
	}))
	f.githubAPI = githubAPI

	cfg, _ := json.Marshal(struct {
		AppID          string            `json:"app_id"`
		InstallationID int64             `json:"installation_id"`
		Repositories   []string          `json:"repositories"`
		Permissions    map[string]string `json:"permissions"`
	}{AppID: "12345", InstallationID: 42, Repositories: []string{"orbit"}, Permissions: map[string]string{"contents": "write"}})

	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "conn-1", TenantID: "acme", Name: "github",
		Adapter: credentials.AdapterGitHubApp, BaseHost: f.connectionHost,
		SecretRef: "k8s:orbit-conn-acme-github", Config: cfg,
	})
	f.repo = repo

	// Adapter registry holds the github_app adapter pointed at the GitHub
	// stub, with a stub secret resolver returning a generated RSA key.
	gh := githubapp.New(ghclient.NewClient(githubAPI.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	registry := NewAdapterRegistry()
	registry.Register(gh)
	f.registry = registry

	eval := &stubEvaluator{allow: true, reason: "allow"}
	f.eval = eval

	f.broker = NewBroker(eval, repo, registry, logger)

	// Forward transport must accept the upstream's self-signed test cert.
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	handler := NewProxyHandler(f.broker, tr, logger)

	// The gateway HTTP server runs as plain HTTP for tests — TLS termination
	// is Phase 2b, not 2a. AuthMiddleware is simulated by authInjector.
	f.gateway = httptest.NewServer(authInjector(claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-9"), handler))

	t.Cleanup(func() {
		f.gateway.Close()
		f.githubAPI.Close()
		f.upstream.Close()
	})
	return f
}

// authInjector simulates AuthMiddleware: places verified PlatformClaims into
// the request context so ProxyHandler can read them.
func authInjector(claims *token.PlatformClaims, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims == nil {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(token.WithClaims(r.Context(), claims)))
	})
}

// callGateway issues a request against the test gateway with the supplied Host
// (the broker resolves the connection from this) and a placeholder platform
// token Authorization header (which the gateway must strip).
func (f *fixture) callGateway(method, path, host string) (*http.Response, error) {
	req, err := http.NewRequest(method, f.gateway.URL+path, nil)
	if err != nil {
		return nil, err
	}
	if host != "" {
		req.Host = host
	}
	req.Header.Set("Authorization", "Bearer platform-token-abc")
	return http.DefaultClient.Do(req)
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestProxy_HappyPath_GitPush_MintsAndForwards(t *testing.T) {
	f := newFixture(t)

	// git push discovery — the path the adapter classifies as repo:write.
	resp, err := f.callGateway(http.MethodGet, "/orbit/orbit.git/info/refs?service=git-receive-pack", f.connectionHost)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s, want 200", resp.StatusCode, body)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:ghs_phase2a"))
	if f.upstreamSawAuth != wantAuth {
		t.Errorf("upstream Authorization = %q, want %q", f.upstreamSawAuth, wantAuth)
	}
	// The platform token must be stripped before the upstream sees the request.
	if strings.Contains(f.upstreamSawAuth, "platform-token-abc") {
		t.Errorf("upstream still saw platform token: %q", f.upstreamSawAuth)
	}
	// The forwarded Host must be the upstream's, not the gateway's.
	if f.upstreamSawHost != f.connectionHost {
		t.Errorf("upstream Host = %q, want %q", f.upstreamSawHost, f.connectionHost)
	}

	// Policy was actually consulted with the action the adapter classified.
	f.eval.mu.Lock()
	defer f.eval.mu.Unlock()
	if len(f.eval.calls) != 1 {
		t.Fatalf("policy.Check calls = %d, want 1", len(f.eval.calls))
	}
	got := f.eval.calls[0]
	if got.ResourceType != "connection" || len(got.PathSegments) != 1 || got.PathSegments[0] != "github" || got.Action != "repo:write" {
		t.Errorf("policy input = %+v, want connection/[github]/repo:write", got)
	}
}

func TestProxy_HappyPath_RestRead_ClassifiesAsRepoRead(t *testing.T) {
	f := newFixture(t)

	resp, err := f.callGateway(http.MethodGet, "/orgs/orbit/repos", f.connectionHost)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	f.eval.mu.Lock()
	defer f.eval.mu.Unlock()
	if len(f.eval.calls) != 1 || f.eval.calls[0].Action != "repo:read" {
		t.Fatalf("policy action = %q, want repo:read", f.eval.calls[0].Action)
	}
}

func TestProxy_PolicyDenies(t *testing.T) {
	f := newFixture(t)
	f.eval.allow = false
	f.eval.reason = "deny:no_grant"

	resp, err := f.callGateway(http.MethodGet, "/anything", f.connectionHost)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if f.upstreamSawAuth != "" {
		t.Errorf("upstream was reached on a denied egress: Authorization = %q", f.upstreamSawAuth)
	}
}

func TestProxy_NoConnectionForHost(t *testing.T) {
	f := newFixture(t)

	resp, err := f.callGateway(http.MethodGet, "/x", "unregistered.example.com")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestProxy_UnsupportedActionDenied(t *testing.T) {
	f := newFixture(t)

	// PROPFIND has no GitHub mapping; the adapter returns ErrUnsupportedAction
	// and the broker treats it as a deny without minting.
	resp, err := f.callGateway("PROPFIND", "/x", f.connectionHost)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if f.upstreamSawAuth != "" {
		t.Errorf("upstream was reached on an unclassifiable request: Authorization = %q", f.upstreamSawAuth)
	}
}

func TestProxy_Unauthenticated(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	registry := NewAdapterRegistry()
	broker := NewBroker(&stubEvaluator{allow: true}, repo, registry, logger)
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	handler := NewProxyHandler(broker, tr, logger)

	srv := httptest.NewServer(authInjector(nil, handler)) // no claims in context
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/x", nil)
	req.Host = "github.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// guard against url.URL{Scheme:"https"} ever being silently rewritten if the
// connection's base_host is mis-formatted.
func TestBuildUpstreamURL_StripSchemes(t *testing.T) {
	in, _ := url.Parse("/foo/bar?x=1")
	for _, base := range []string{"github.com", "https://github.com", "http://github.com", "github.com/"} {
		got, err := buildUpstreamURL(base, in)
		if err != nil {
			t.Fatalf("%q: %v", base, err)
		}
		if got.Host != "github.com" || got.Scheme != "https" || got.Path != "/foo/bar" || got.RawQuery != "x=1" {
			t.Errorf("%q -> %+v", base, got)
		}
	}
}
