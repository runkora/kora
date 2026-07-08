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

func TestTransportCache_NoCACertReturnsBase(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)

	got, err := c.For(&credentials.Connection{ID: "acme|github"})
	if err != nil {
		t.Fatalf("For: unexpected error %v", err)
	}
	if got != base {
		t.Errorf("For(no ca_cert) = %v, want the shared base transport", got)
	}
}

func TestTransportCache_NilConnectionReturnsBase(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)

	got, err := c.For(nil)
	if err != nil {
		t.Fatalf("For: unexpected error %v", err)
	}
	if got != base {
		t.Errorf("For(nil) = %v, want the shared base transport", got)
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
	if got == base {
		t.Fatal("For(ca_cert) returned the shared base transport, want a scoped clone")
	}
	tr, ok := got.(*http.Transport)
	if !ok {
		t.Fatalf("For(ca_cert) = %T, want *http.Transport", got)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
		t.Fatal("scoped transport has no RootCAs configured")
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
	if plain != base {
		t.Error("a connection with no ca_cert must get the shared base transport, unaffected by other connections' CAs")
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

func TestTransportCache_NonHTTPTransportBaseErrors(t *testing.T) {
	// A base RoundTripper that isn't *http.Transport cannot be cloned to carry
	// a per-connection TLSClientConfig.
	c := NewTransportCache(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))

	_, err := c.For(&credentials.Connection{ID: "acme|k8s", Config: caCertConfig(t)})
	if err == nil {
		t.Fatal("For: expected error when base transport cannot be cloned, got nil")
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
