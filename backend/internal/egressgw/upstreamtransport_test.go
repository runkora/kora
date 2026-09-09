package egressgw

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/egressgw/tlsca/testtlsca"
)

// testCACertPEM returns a valid, freshly-minted self-signed CA certificate
// PEM, reusing the same fixture the CONNECT tests use.
func testCACertPEM(t *testing.T) string {
	t.Helper()
	_, certPEM, err := testtlsca.NewCA("test-tenant")
	if err != nil {
		t.Fatalf("testtlsca.NewCA: %v", err)
	}
	return string(certPEM)
}

func caCertConfig(t *testing.T) json.RawMessage {
	t.Helper()
	cfg, err := json.Marshal(map[string]string{"ca_cert": testCACertPEM(t)})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return cfg
}

// mustCACertPEM pulls the ca_cert back out of a connection config so a test can
// rebuild the same clone For() cached.
func mustCACertPEM(t *testing.T, config json.RawMessage) string {
	t.Helper()
	var cfg connectionTLSConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		t.Fatalf("unmarshal connection config: %v", err)
	}
	return cfg.CACert
}

func TestTransportCache_NoCACertReturnsBase(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)

	got, err := c.For(&credentials.Connection{ID: "acme|github"})
	if err != nil {
		t.Fatalf("For: unexpected error %v", err)
	}
	if got != c.shared {
		t.Errorf("For(no ca_cert) = %v, want the shared traced transport", got)
	}
}

func TestTransportCache_NilConnectionReturnsBase(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)

	got, err := c.For(nil)
	if err != nil {
		t.Fatalf("For: unexpected error %v", err)
	}
	if got != c.shared {
		t.Errorf("For(nil) = %v, want the shared traced transport", got)
	}
}

func TestTransportCache_CACertBuildsScopedTransport(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)
	conn := &credentials.Connection{ID: "acme|k8s", Config: caCertConfig(t)}

	got, err := c.For(conn)
	if err != nil {
		t.Fatalf("For: unexpected error %v", err)
	}
	if got == c.shared {
		t.Fatal("For(ca_cert) returned the shared transport, want a scoped clone")
	}

	// The clone carries the connection's CA. For() hands back a traced wrapper
	// around it, so inspect what buildTransport produced.
	tr, err := c.buildTransport(mustCACertPEM(t, conn.Config))
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
		t.Fatal("scoped transport has no RootCAs configured")
	}
	if tr == base {
		t.Fatal("buildTransport returned the base transport rather than a clone")
	}

	// A second call for the same connection must return the cached transport.
	again, err := c.For(conn)
	if err != nil {
		t.Fatalf("For (second call): unexpected error %v", err)
	}
	if again != got {
		t.Error("For() did not return the cached transport on a second call")
	}
}

func TestTransportCache_ScopedPerConnection(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)
	cfg := caCertConfig(t)
	connA := &credentials.Connection{ID: "acme|k8s-a", Config: cfg}
	connB := &credentials.Connection{ID: "acme|k8s-b", Config: cfg}

	trA, err := c.For(connA)
	if err != nil {
		t.Fatalf("For(connA): %v", err)
	}
	trB, err := c.For(connB)
	if err != nil {
		t.Fatalf("For(connB): %v", err)
	}
	if trA == trB {
		t.Error("two different connections got the same cached transport instance")
	}

	// A connection with no ca_cert must still get the untouched base, even
	// once other connections have populated the cache.
	plain, err := c.For(&credentials.Connection{ID: "acme|github"})
	if err != nil {
		t.Fatalf("For(plain): %v", err)
	}
	if plain != c.shared {
		t.Error("a connection with no ca_cert must get the shared transport, unaffected by other connections' CAs")
	}
}

func TestTransportCache_InvalidCACertErrors(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)

	cfg, _ := json.Marshal(map[string]string{"ca_cert": "not a real certificate"})
	_, err := c.For(&credentials.Connection{ID: "acme|k8s", Config: cfg})
	if err == nil {
		t.Fatal("For: expected error for an unparseable ca_cert, got nil")
	}
}

func TestTransportCache_MalformedConfigErrors(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)

	_, err := c.For(&credentials.Connection{ID: "acme|k8s", Config: json.RawMessage(`{not json`)})
	if err == nil {
		t.Fatal("For: expected error for malformed connection config, got nil")
	}
}

func TestTransportCache_InvalidateClearsCache(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)
	conn := &credentials.Connection{ID: "acme|k8s", Config: caCertConfig(t)}

	first, err := c.For(conn)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	c.Invalidate()
	second, err := c.For(conn)
	if err != nil {
		t.Fatalf("For (after invalidate): %v", err)
	}
	if first == second {
		t.Error("Invalidate did not force a fresh transport to be built")
	}
}
