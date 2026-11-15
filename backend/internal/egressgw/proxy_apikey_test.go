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
	"github.com/runkora/kora/backend/internal/credentials/adapters/apikey"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const apikeyRealToken = "ghp_real_registry_token"

type apikeySecrets struct{}

func (apikeySecrets) Resolve(context.Context, string) ([]byte, error) {
	return []byte(apikeyRealToken), nil
}

// apikeyFixture wires a real apikey adapter behind the proxy. One upstream
// hosts the apikey connection; a second is a host with no connection.
type apikeyFixture struct {
	gateway      *httptest.Server
	servedHost   string
	unservedHost string

	mu   sync.Mutex
	seen map[string]http.Header // upstream host -> headers of the last request
	hits map[string]string      // upstream host -> method of the last request
}

func (f *apikeyFixture) record(host string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen[host] = r.Header.Clone()
		f.hits[host] = r.Method
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}
}

func newAPIKeyFixture(t *testing.T, eval policyChecker, cfg string) *apikeyFixture {
	t.Helper()
	logger := logging.NewLoggerWithOutput(io.Discard)
	f := &apikeyFixture{seen: map[string]http.Header{}, hits: map[string]string{}}

	served := httptest.NewTLSServer(nil)
	unserved := httptest.NewTLSServer(nil)
	f.servedHost = strings.TrimPrefix(served.URL, "https://")
	f.unservedHost = strings.TrimPrefix(unserved.URL, "https://")
	served.Config.Handler = f.record(f.servedHost)
	unserved.Config.Handler = f.record(f.unservedHost)

	conn := &credentials.Connection{
		ID: "conn-npm", TenantID: "acme", Name: "npm",
		Adapter: credentials.AdapterAPIKey, BaseHost: f.servedHost, SecretRef: "ref",
	}
	if cfg != "" {
		conn.Config = []byte(cfg)
	}
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	_ = repo.Create(context.Background(), conn)

	registry := NewAdapterRegistry()
	registry.Register(apikey.New(apikeySecrets{}, logger))
	broker := NewBroker(eval, repo, registry, passthrough.New(), logger)
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	handler := NewProxyHandler(broker, tr, testBrand, logger)
	f.gateway = httptest.NewServer(authInjector(claimsFor("acme", "spiffe://runkora.test/tenant/kora-tenant-acme/agent/coder", "run-9"), handler))

	t.Cleanup(func() { f.gateway.Close(); served.Close(); unserved.Close() })
	return f
}

func (f *apikeyFixture) call(t *testing.T, method, host string, hdr map[string]string) *http.Response {
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

func (f *apikeyFixture) header(host, name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[host].Get(name)
}

func TestProxy_APIKey_ReplacesPlaceholderAuthorization(t *testing.T) {
	f := newAPIKeyFixture(t, heldScopes{"npm:read"}, "")
	resp := f.call(t, http.MethodGet, f.servedHost, map[string]string{"Authorization": "Bearer placeholder"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := f.header(f.servedHost, "Authorization"), "Bearer "+apikeyRealToken; got != want {
		t.Errorf("upstream Authorization = %q, want %q", got, want)
	}
}

func TestProxy_APIKey_AddsAuthorizationWhenAbsent(t *testing.T) {
	f := newAPIKeyFixture(t, heldScopes{"npm:read"}, "")
	resp := f.call(t, http.MethodGet, f.servedHost, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := f.header(f.servedHost, "Authorization"), "Bearer "+apikeyRealToken; got != want {
		t.Errorf("upstream Authorization = %q, want %q", got, want)
	}
}

func TestProxy_APIKey_CustomHeaderReplacesClientValue(t *testing.T) {
	f := newAPIKeyFixture(t, heldScopes{"npm:read"}, `{"headers":[{"name":"X-Api-Token"}]}`)
	f.call(t, http.MethodGet, f.servedHost, map[string]string{"X-Api-Token": "placeholder", "Authorization": "Bearer placeholder"})
	if got := f.header(f.servedHost, "X-Api-Token"); got != apikeyRealToken {
		t.Errorf("upstream X-Api-Token = %q, want %q", got, apikeyRealToken)
	}
	if got := f.header(f.servedHost, "Authorization"); got != "" {
		t.Errorf("upstream Authorization = %q, want none (stripped)", got)
	}
}

func TestProxy_APIKey_MultipleHeadersReplacedAndAdded(t *testing.T) {
	f := newAPIKeyFixture(t, heldScopes{"npm:read"},
		`{"headers":[{"name":"Authorization","prefix":"Token "},{"name":"X-Api-Key"},{"name":"X-Extra","prefix":"k="}]}`)
	// X-Api-Key is supplied as a placeholder (replaced); X-Extra is absent (added).
	f.call(t, http.MethodGet, f.servedHost, map[string]string{"X-Api-Key": "placeholder"})
	for name, want := range map[string]string{
		"Authorization": "Token " + apikeyRealToken,
		"X-Api-Key":     apikeyRealToken,
		"X-Extra":       "k=" + apikeyRealToken,
	} {
		if got := f.header(f.servedHost, name); got != want {
			t.Errorf("upstream %s = %q, want %q", name, got, want)
		}
	}
}

func TestProxy_APIKey_ReadOnlyGrantDeniesPut(t *testing.T) {
	f := newAPIKeyFixture(t, heldScopes{"npm:read"}, "")

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

func TestProxy_APIKey_UnservedHostFallsThroughToPassthrough(t *testing.T) {
	f := newAPIKeyFixture(t, heldScopes{"npm:read"}, "")
	resp := f.call(t, http.MethodGet, f.unservedHost, map[string]string{"Authorization": "Bearer platform-token"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (passthrough)", resp.StatusCode)
	}
	if got := f.header(f.unservedHost, "Authorization"); got != "" {
		t.Errorf("unserved host saw Authorization %q, want none", got)
	}
	if strings.Contains(f.header(f.unservedHost, "Authorization"), apikeyRealToken) {
		t.Error("apikey secret leaked to an unserved host")
	}
}
