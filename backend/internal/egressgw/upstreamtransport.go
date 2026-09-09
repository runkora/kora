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
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
)

// connectionTLSConfig is the adapter-agnostic shape of the one CA-trust field
// any connection's Config may carry. It is deliberately NOT any adapter's own
// config type such as k8sbearer's. ca_cert is a connection-level convention,
// not a Kubernetes-specific one, so any future adapter needing a private CA
// opts in by putting ca_cert in its own Config, with no gateway change.
type connectionTLSConfig struct {
	CACert string `json:"ca_cert,omitempty"`
}

// ErrTransportUnavailable is returned by TransportCache.For when a connection's
// declared ca_cert cannot be parsed. The proxy treats this as a distinct,
// logged failure mode from a policy deny.
var ErrTransportUnavailable = errors.New("upstream transport unavailable")

// upstreamTracer names the OTel tracer for the forward leg. ReverseProxy owns
// the request lifecycle, so the span comes from the RoundTripper layer.
const upstreamTracer = "orbit.egress-upstream"

// TransportCache resolves the http.RoundTripper the proxy should use to
// re-originate a call to a given connection's upstream. Connections with no
// declared ca_cert get the shared base transport unchanged (e.g. github.com's
// behavior, validated against the OS trust store); a connection with a
// ca_cert gets its own cached *http.Transport that additionally trusts that
// CA, scoped to exactly that connection so a leaked or misconfigured CA can
// never affect certificate validation for any other connection.
// Every transport it hands out is wrapped in the platform tracing transport, so
// each forwarded call carries an OTel client span. The wrapper sits outside the
// CA-trust clone, keeping the span across the whole round-trip.
type TransportCache struct {
	base   *http.Transport
	shared http.RoundTripper

	mu     sync.RWMutex
	byConn map[string]http.RoundTripper
}

// NewTransportCache constructs a TransportCache. base is cloned per connection
// that declares a ca_cert and used as-is otherwise. It is a concrete
// *http.Transport because CA trust is applied by cloning it, and it is required:
// the owning binary decides the forward leg's timeouts.
func NewTransportCache(base *http.Transport) *TransportCache {
	return &TransportCache{
		base:   base,
		shared: httpclient.NewTracingTransport(upstreamTracer, base),
		byConn: make(map[string]http.RoundTripper),
	}
}

// For returns the http.RoundTripper the proxy should use to forward to conn's
// upstream. A nil conn, or one with no ca_cert in Config, returns the shared
// base transport.
func (c *TransportCache) For(conn *credentials.Connection) (http.RoundTripper, error) {
	if conn == nil || len(conn.Config) == 0 {
		return c.shared, nil
	}

	var cfg connectionTLSConfig
	if err := json.Unmarshal(conn.Config, &cfg); err != nil {
		return nil, fmt.Errorf("%w: parse connection config: %w", ErrTransportUnavailable, err)
	}
	if cfg.CACert == "" {
		return c.shared, nil
	}

	c.mu.RLock()
	cached, ok := c.byConn[conn.ID]
	c.mu.RUnlock()
	if ok {
		return cached, nil
	}

	clone, err := c.buildTransport(cfg.CACert)
	if err != nil {
		return nil, err
	}
	traced := httpclient.NewTracingTransport(upstreamTracer, clone)

	c.mu.Lock()
	c.byConn[conn.ID] = traced
	c.mu.Unlock()
	return traced, nil
}

// buildTransport clones the base transport with an additional trusted CA
// appended to the system pool. The caller wraps it for tracing.
func (c *TransportCache) buildTransport(caCertPEM string) (*http.Transport, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM([]byte(caCertPEM)) {
		return nil, fmt.Errorf("%w: connection ca_cert contains no usable certificate", ErrTransportUnavailable)
	}

	clone := c.base.Clone()
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
