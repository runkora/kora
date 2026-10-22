// Upstream credentials: the one place that decides which headers a request to
// an MCP server carries, for both introspection and tools/call.
// See docs/adr/2026-10-22-mcp-server-static-credentials.md.
package toolgateway

import (
	"context"
	"maps"
	"net/http"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// Caller is the principal behind a tools/call: its platform bearer and, for a
// delegated run, the run's delegation token.
type Caller struct {
	Bearer     string
	Delegation string
}

// UpstreamCredentials returns the headers a request to srv carries.
// Introspection passes a nil caller.
type UpstreamCredentials interface {
	Headers(ctx context.Context, srv Server, caller *Caller) (http.Header, error)
}

// upstreamCredentials is the gateway's UpstreamCredentials.
type upstreamCredentials struct{}

// NewUpstreamCredentials returns the gateway's credential lookup.
func NewUpstreamCredentials() UpstreamCredentials {
	return upstreamCredentials{}
}

// Headers forwards the caller only to a global server flagged
// callerIdentity: forward. A tenant admin could otherwise harvest callers'
// platform tokens through a server they control.
func (upstreamCredentials) Headers(_ context.Context, srv Server, caller *Caller) (http.Header, error) {
	h := http.Header{}
	if srv.CallerIdentity != callerIdentityForward || srv.TenantID != "" || caller == nil || caller.Bearer == "" {
		return h, nil
	}
	h.Set("Authorization", "Bearer "+caller.Bearer)
	if caller.Delegation != "" {
		h.Set(mw.DelegationTokenHeader, caller.Delegation)
	}
	return h, nil
}

// upstreamClient adds headers to every request and does not follow redirects.
// A followed redirect would carry the headers to another host.
func upstreamClient(timeout time.Duration, tracerName string, headers http.Header) *http.Client {
	c := httpclient.NewHTTPClient(timeout, httpclient.NewTracingTransport(tracerName, &headerTransport{headers: headers, base: http.DefaultTransport}))
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// headerTransport sets fixed headers on each request.
type headerTransport struct {
	headers http.Header
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	maps.Copy(r.Header, t.headers)
	return t.base.RoundTrip(r)
}
