package egressgw

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/clear-route/orbit/internal/credentials"
)

// connectionTLSConfig is the adapter-agnostic shape of the one CA-trust field
// any connection's Config may carry. It is deliberately NOT any adapter's own
// config type (e.g. k8sbearer's) — ca_cert is a connection-level convention,
// not a Kubernetes-specific one, so any future adapter needing a private CA
// can opt in by putting ca_cert in its own Config with no gateway change.
type connectionTLSConfig struct {
	CACert string `json:"ca_cert,omitempty"`
}

// ErrTransportUnavailable is returned by TransportCache.For when a
// connection's declared ca_cert cannot be parsed, or when CA trust is
// requested against a base transport that cannot be cloned. The proxy treats
// this as a distinct, logged failure mode from a policy deny.
var ErrTransportUnavailable = errors.New("upstream transport unavailable")

// TransportCache resolves the http.RoundTripper the proxy should use to
// re-originate a call to a given connection's upstream. Connections with no
// declared ca_cert get the shared base transport unchanged (e.g. github.com's
// behavior, validated against the OS trust store); a connection with a
// ca_cert gets its own cached *http.Transport that additionally trusts that
// CA, scoped to exactly that connection so a leaked or misconfigured CA can
// never affect certificate validation for any other connection.
type TransportCache struct {
	base http.RoundTripper

	mu     sync.RWMutex
	byConn map[string]http.RoundTripper
}

// NewTransportCache constructs a TransportCache. base is used unchanged for
// every connection that declares no ca_cert; a nil base defaults to
// http.DefaultTransport.
func NewTransportCache(base http.RoundTripper) *TransportCache {
	if base == nil {
		base = http.DefaultTransport
	}
	return &TransportCache{base: base, byConn: make(map[string]http.RoundTripper)}
}

// For returns the http.RoundTripper the proxy should use to forward to conn's
// upstream. A nil conn, or one with no ca_cert in Config, returns the shared
// base transport.
func (c *TransportCache) For(conn *credentials.Connection) (http.RoundTripper, error) {
	if conn == nil || len(conn.Config) == 0 {
		return c.base, nil
	}

	var cfg connectionTLSConfig
	if err := json.Unmarshal(conn.Config, &cfg); err != nil {
		return nil, fmt.Errorf("%w: parse connection config: %w", ErrTransportUnavailable, err)
	}
	if cfg.CACert == "" {
		return c.base, nil
	}

	c.mu.RLock()
	cached, ok := c.byConn[conn.ID]
	c.mu.RUnlock()
	if ok {
		return cached, nil
	}

	transport, err := c.buildTransport(cfg.CACert)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.byConn[conn.ID] = transport
	c.mu.Unlock()
	return transport, nil
}

// buildTransport clones the base *http.Transport with an additional trusted
// CA appended to the system pool. The base must be an *http.Transport — the
// production gateway always passes http.DefaultTransport (or an equivalent
// configured Transport); a base that cannot be cloned is a caller error, not
// a per-request failure.
func (c *TransportCache) buildTransport(caCertPEM string) (http.RoundTripper, error) {
	base, ok := c.base.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("%w: base transport does not support per-connection CA trust", ErrTransportUnavailable)
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM([]byte(caCertPEM)) {
		return nil, fmt.Errorf("%w: connection ca_cert contains no usable certificate", ErrTransportUnavailable)
	}

	clone := base.Clone()
	tlsConfig := clone.TLSClientConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	tlsConfig.RootCAs = pool
	clone.TLSClientConfig = tlsConfig

	return clone, nil
}

// Invalidate drops every cached transport. Wired into the connection
// registry's reload hook so a Connection/Secret change (including a rotated
// ca_cert) is picked up without a gateway restart, without needing to know
// which specific connection changed.
func (c *TransportCache) Invalidate() {
	c.mu.Lock()
	c.byConn = make(map[string]http.RoundTripper)
	c.mu.Unlock()
}
