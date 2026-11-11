package egressgw

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/bearer"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const bearerRealToken = "ghp_real_registry_token"

type bearerSecrets struct{}

func (bearerSecrets) Resolve(context.Context, string) ([]byte, error) {
	return []byte(bearerRealToken), nil
}

// bearerFixture wires a real bearer adapter behind the proxy. One upstream
// hosts the bearer connection; a second is a host with no connection.
type bearerFixture struct {
	gateway      *httptest.Server
	servedHost   string
	unservedHost string

	mu   sync.Mutex
	seen map[string]http.Header // upstream host -> headers of the last request
	hits map[string]string      // upstream host -> method of the last request
}

func (f *bearerFixture) record(host string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen[host] = r.Header.Clone()
		f.hits[host] = r.Method
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}
}

func newBearerFixture(t *testing.T, eval policyChecker, cfg string) *bearerFixture {
	t.Helper()
	logger := logging.NewLoggerWithOutput(io.Discard)
	f := &bearerFixture{seen: map[string]http.Header{}, hits: map[string]string{}}

	served := httptest.NewTLSServer(nil)
	unserved := httptest.NewTLSServer(nil)
	f.servedHost = strings.TrimPrefix(served.URL, "https://")
	f.unservedHost = strings.TrimPrefix(unserved.URL, "https://")
	served.Config.Handler = f.record(f.servedHost)
	unserved.Config.Handler = f.record(f.unservedHost)

	conn := &credentials.Connection{
		ID: "conn-npm", TenantID: "acme", Name: "npm",
		Adapter: credentials.AdapterBearer, BaseHost: f.servedHost, SecretRef: "ref",
	}
	if cfg != "" {
		conn.Config = []byte(cfg)
	}
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	_ = repo.Create(context.Background(), conn)

	registry := NewAdapterRegistry()
	registry.Register(bearer.New(bearerSecrets{}, logger))
	broker := NewBroker(eval, repo, registry, passthrough.New(), logger)
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	handler := NewProxyHandler(broker, tr, testBrand, logger)
	f.gateway = httptest.NewServer(authInjector(claimsFor("acme", "spiffe://runkora.test/tenant/kora-tenant-acme/agent/coder", "run-9"), handler))

	t.Cleanup(func() { f.gateway.Close(); served.Close(); unserved.Close() })
	return f
}

func (f *bearerFixture) call(t *testing.T, method, host string, hdr map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, f.gateway.URL+"/@clear-route%2fcompass-theme", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (f *bearerFixture) header(host, name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[host].Get(name)
}

func TestProxy_Bearer_ReplacesPlaceholderAuthorization(t *testing.T) {
	f := newBearerFixture(t, heldScopes{"npm:read"}, "")
	resp := f.call(t, http.MethodGet, f.servedHost, map[string]string{"Authorization": "Bearer placeholder"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := f.header(f.servedHost, "Authorization"), "Bearer "+bearerRealToken; got != want {
		t.Errorf("upstream Authorization = %q, want %q", got, want)
	}
}

func TestProxy_Bearer_AddsAuthorizationWhenAbsent(t *testing.T) {
	f := newBearerFixture(t, heldScopes{"npm:read"}, "")
	resp := f.call(t, http.MethodGet, f.servedHost, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := f.header(f.servedHost, "Authorization"), "Bearer "+bearerRealToken; got != want {
		t.Errorf("upstream Authorization = %q, want %q", got, want)
	}
}

func TestProxy_Bearer_CustomHeaderReplacesClientValue(t *testing.T) {
	f := newBearerFixture(t, heldScopes{"npm:read"}, `{"header":"X-Api-Token","prefix":""}`)
	f.call(t, http.MethodGet, f.servedHost, map[string]string{"X-Api-Token": "placeholder"})
	if got := f.header(f.servedHost, "X-Api-Token"); got != bearerRealToken {
		t.Errorf("upstream X-Api-Token = %q, want %q", got, bearerRealToken)
	}
}

func TestProxy_Bearer_ReadOnlyGrantDeniesPut(t *testing.T) {
	f := newBearerFixture(t, heldScopes{"npm:read"}, "")

	if resp := f.call(t, http.MethodGet, f.servedHost, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	f.mu.Lock()
	delete(f.hits, f.servedHost)
	f.mu.Unlock()

	resp := f.call(t, http.MethodPut, f.servedHost, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("PUT status = %d, want 403", resp.StatusCode)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.hits[f.servedHost]; ok {
		t.Errorf("upstream was reached by denied %s", m)
	}
}

func TestProxy_Bearer_UnservedHostFallsThroughToPassthrough(t *testing.T) {
	f := newBearerFixture(t, heldScopes{"npm:read"}, "")
	resp := f.call(t, http.MethodGet, f.unservedHost, map[string]string{"Authorization": "Bearer platform-token"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (passthrough)", resp.StatusCode)
	}
	if got := f.header(f.unservedHost, "Authorization"); got != "" {
		t.Errorf("unserved host saw Authorization %q, want none", got)
	}
	if strings.Contains(f.header(f.unservedHost, "Authorization"), bearerRealToken) {
		t.Error("bearer token leaked to an unserved host")
	}
}
