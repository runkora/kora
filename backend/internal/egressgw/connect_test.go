package egressgw

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	"github.com/clear-route/orbit/internal/egressgw/tlsca"
	"github.com/clear-route/orbit/internal/egressgw/tlsca/testtlsca"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// fixedCABundle is a one-tenant CABundle for tests.
type fixedCABundle struct {
	tenant string
	ca     *tlsca.IssuingCA
}

func (f *fixedCABundle) For(tenant string) (IssuingCA, error) {
	if tenant != f.tenant {
		return nil, fmt.Errorf("no CA for tenant %q", tenant)
	}
	return f.ca, nil
}

// TestConnect_EndToEnd is the load-bearing Phase 2b test: a real http.Client
// configured to use the gateway as an HTTPS proxy hits an upstream over a
// CONNECT tunnel; the gateway terminates TLS with a per-tenant-CA-signed leaf,
// the broker mints a GitHub installation token, and the upstream sees the
// minted Basic auth credential.
//
// Wiring:
//
//	tool client (trusts test CA root)
//	  -- HTTPS proxy --> gateway (CONNECT)
//	                       hijack + 200
//	                       tls.Server with IssuingCA.GetCertificate
//	                       inner http.ReadRequest → ProxyHandler
//	                                                  └ broker.Authorize
//	                                                       (resolve by Host →
//	                                                        adapter.ClassifyAction →
//	                                                        policy.Check →
//	                                                        github_app.Resolve)
//	                                                  └ swap auth, forward
//	                       └ upstream stub (httptest.NewTLSServer)
func TestConnect_EndToEnd_GitHubMintAndForward(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)

	// Upstream stub — receives the gateway's forward request, captures auth.
	var (
		upstreamSawAuth string
		upstreamSawHost string
	)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSawAuth = r.Header.Get("Authorization")
		upstreamSawHost = r.Host
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	// GitHub API stub — mints the installation token.
	githubAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":                "ghs_phase2b",
			"expires_at":           time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"permissions":          map[string]string{"contents": "write"},
			"repository_selection": "selected",
		})
	}))
	defer githubAPI.Close()

	// Per-tenant CA: the broker uses its private key to mint the leaf the tool
	// sees during its TLS handshake; the test client trusts the public root.
	ca, rootPEM, err := testtlsca.NewCA("acme")
	if err != nil {
		t.Fatalf("testtlsca.NewCA: %v", err)
	}

	// Connection registry: register github by upstream's host:port. ProxyHandler
	// resolves connection by inbound r.Host and forwards to BaseHost.
	upstreamHost := strings.TrimPrefix(upstream.URL, "https://")
	cfg, _ := json.Marshal(struct {
		AppID          string            `json:"app_id"`
		InstallationID int64             `json:"installation_id"`
		Repositories   []string          `json:"repositories"`
		Permissions    map[string]string `json:"permissions"`
	}{AppID: "12345", InstallationID: 42, Repositories: []string{"orbit"}, Permissions: map[string]string{"contents": "write"}})

	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "conn-1", TenantID: "acme", Name: "github",
		Adapter: credentials.AdapterGitHubApp, BaseHost: upstreamHost,
		SecretRef: "k8s:orbit-conn-acme-github", Config: cfg,
	})

	gh := githubapp.New(ghclient.NewClient(githubAPI.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	registry := NewAdapterRegistry()
	registry.Register(gh)

	broker := NewBroker(&stubEvaluator{allow: true}, repo, registry, logger)

	// Forward transport must accept the upstream's self-signed cert. The
	// proxy itself terminates inbound TLS via the IssuingCA.
	forwardTransport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	proxyInner := NewProxyHandler(broker, forwardTransport, logger)

	// Wrap proxyInner with claims-injection so the broker sees a verified caller.
	innerWithAuth := authInjector(claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-9"), proxyInner)

	// Wrap the single test CA in a one-tenant bundle so it satisfies
	// CABundle. In production this comes from cabundle.LoadEager scanning the
	// per-tenant K8s-secret mount.
	bundle := &fixedCABundle{tenant: "acme", ca: ca}
	connect := NewConnectHandler(bundle, innerWithAuth, repo, logger)

	// The gateway's outer HTTP server only handles CONNECT; AuthMiddleware
	// (simulated by authInjector) runs before the CONNECT handler reads claims
	// from context, then re-attaches them onto the inner request.
	//
	// We bypass http.ServeMux because its routing is path-based and CONNECT
	// requests carry host:port in the request-target (no path). The simplest
	// correct behavior is to register a handler that always runs.
	gatewayHandler := authInjector(claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-9"), connect)

	// httptest.NewServer is plain HTTP, which is what we want — the proxy
	// connection itself doesn't need TLS in the test (in production it sits
	// behind the agent-pod sidecar's mTLS).
	gateway := httptest.NewServer(gatewayHandler)
	defer gateway.Close()

	// Test client trusts the per-tenant CA root and uses the gateway as an
	// HTTPS proxy. http.Transport will issue CONNECT for any HTTPS URL.
	clientPool := x509.NewCertPool()
	if ok := clientPool.AppendCertsFromPEM(rootPEM); !ok {
		t.Fatal("append CA root to client pool failed")
	}
	gatewayURL, _ := url.Parse(gateway.URL)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(gatewayURL),
			TLSClientConfig: &tls.Config{
				RootCAs: clientPool,
				// The CONNECT target's hostname is what the cert is issued for;
				// because the test routes everything through the proxy and the
				// upstreamHost is a real listener, we don't need to override
				// ServerName here — Go uses the URL host by default.
			},
			DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		},
		Timeout: 10 * time.Second,
	}

	// The test request — addressed at the connection's BaseHost over HTTPS.
	// http.Transport will:
	//   1. Send `CONNECT <upstreamHost> HTTP/1.1` to the gateway.
	//   2. On 200, perform a TLS handshake; the gateway's IssuingCA mints a
	//      leaf with that SNI which the client accepts via the trusted root.
	//   3. Send the actual GET as a plain HTTP request inside the TLS tunnel.
	reqURL := "https://" + upstreamHost + "/orbit/orbit.git/info/refs?service=git-receive-pack"
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %q, want 200", resp.StatusCode, body)
	}

	// The upstream must have seen the minted Basic auth, never the platform token.
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:ghs_phase2b"))
	if upstreamSawAuth != wantAuth {
		t.Errorf("upstream Authorization = %q, want %q", upstreamSawAuth, wantAuth)
	}
	if upstreamSawHost != upstreamHost {
		t.Errorf("upstream Host = %q, want %q", upstreamSawHost, upstreamHost)
	}
}
