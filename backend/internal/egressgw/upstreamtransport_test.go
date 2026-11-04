package egressgw

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/egressgw/tlsca/testtlsca"
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
	conn := &credentials.Connection{ID: "acme|k8s", CACert: testCACertPEM(t)}

	got, err := c.For(conn)
	if err != nil {
		t.Fatalf("For: unexpected error %v", err)
	}
	if got == c.shared {
		t.Fatal("For(ca_cert) returned the shared transport, want a scoped clone")
	}

	// The clone carries the connection's CA. For() hands back a traced wrapper
	// around it, so inspect what buildTransport produced.
	tr, err := c.buildTransport(conn.CACert)
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
	pem := testCACertPEM(t)
	connA := &credentials.Connection{ID: "acme|k8s-a", CACert: pem}
	connB := &credentials.Connection{ID: "acme|k8s-b", CACert: pem}

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

	_, err := c.For(&credentials.Connection{ID: "acme|k8s", CACert: "not a real certificate"})
	if err == nil {
		t.Fatal("For: expected error for an unparseable ca_cert, got nil")
	}
}

// TestTransportCache_IgnoresAdapterConfig locks the split: the transport reads
// the connection's CA and nothing else. A config the adapter would reject is
// reported by Open, not here, so malformed adapter keys never reach this path.
func TestTransportCache_IgnoresAdapterConfig(t *testing.T) {
	c := NewTransportCache(&http.Transport{})

	got, err := c.For(&credentials.Connection{ID: "acme|k8s", Config: json.RawMessage(`{not json`)})
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if got != c.shared {
		t.Error("a connection with no CA must get the shared transport")
	}
}

func TestTransportCache_InvalidateClearsCache(t *testing.T) {
	base := &http.Transport{}
	c := NewTransportCache(base)
	conn := &credentials.Connection{ID: "acme|k8s", CACert: testCACertPEM(t)}

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
