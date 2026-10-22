// Upstream credentials: the one place that decides which headers a request to
// an MCP server carries, for both introspection and tools/call.
// See docs/adr/2026-10-22-mcp-server-static-credentials.md.
package toolgateway

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

var (
	// ErrCredentialUnavailable is returned when a server's declared credential
	// is not in the mounted Secret. The call fails closed.
	ErrCredentialUnavailable = errors.New("credential_unavailable")
	// ErrUpstreamAuthFailed replaces an upstream 401 or 403. The upstream's
	// response body is dropped because it may echo the credential.
	ErrUpstreamAuthFailed = errors.New("auth_failed")
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

// upstreamCredentials reads static credentials from the mounted Secret on each
// request. The kubelet swaps the files atomically on rotation.
type upstreamCredentials struct {
	secretsDir string
}

// NewUpstreamCredentials returns the gateway's credential lookup over the
// mounted orbit-mcp-server-secrets directory.
func NewUpstreamCredentials(secretsDir string) UpstreamCredentials {
	return upstreamCredentials{secretsDir: secretsDir}
}

// Headers sends a server's declared credential whatever the caller. Otherwise
// it forwards the caller only to a global server flagged callerIdentity:
// forward. A tenant admin could harvest callers' platform tokens through a
// server they control.
func (u upstreamCredentials) Headers(_ context.Context, srv Server, caller *Caller) (http.Header, error) {
	h := http.Header{}
	if len(srv.AuthHeaders) > 0 {
		for _, a := range srv.AuthHeaders {
			v, ok := u.read(a.SecretKey)
			if !ok {
				return nil, fmt.Errorf("%w: header %q", ErrCredentialUnavailable, a.Name)
			}
			h.Set(a.Name, a.Prefix+v)
		}
		return h, nil
	}
	if srv.CallerIdentity != callerIdentityForward || srv.TenantID != "" || caller == nil || caller.Bearer == "" {
		return h, nil
	}
	h.Set("Authorization", "Bearer "+caller.Bearer)
	if caller.Delegation != "" {
		h.Set(mw.DelegationTokenHeader, caller.Delegation)
	}
	return h, nil
}

// read returns one value from the mounted Secret. Keys are flat file names.
func (u upstreamCredentials) read(key string) (string, bool) {
	if key == "" || strings.HasPrefix(key, ".") || strings.ContainsAny(key, `/\`) {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(u.secretsDir, key))
	if err != nil || len(b) == 0 {
		return "", false
	}
	return string(b), true
}

// upstreamClient adds headers to every request and does not follow redirects.
// A followed redirect would carry the headers to another host.
func upstreamClient(timeout time.Duration, tracerName string, headers http.Header) (*http.Client, *headerTransport) {
	t := &headerTransport{headers: headers, base: http.DefaultTransport}
	c := httpclient.NewHTTPClient(timeout, httpclient.NewTracingTransport(tracerName, t))
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c, t
}

// headerTransport sets fixed headers on each request and records an upstream
// 401 or 403. The MCP client folds the response body into its error.
type headerTransport struct {
	headers    http.Header
	base       http.RoundTripper
	authStatus atomic.Int32
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	maps.Copy(r.Header, t.headers)
	resp, err := t.base.RoundTrip(r)
	if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		t.authStatus.Store(int32(resp.StatusCode))
	}
	return resp, err
}

// upstreamError replaces err with ErrUpstreamAuthFailed after a 401 or 403.
func (t *headerTransport) upstreamError(err error) error {
	if code := t.authStatus.Load(); code != 0 {
		return fmt.Errorf("%w: HTTP %d", ErrUpstreamAuthFailed, code)
	}
	return err
}

// failureReason classifies an upstream error for logs.
func failureReason(err error) string {
	switch {
	case errors.Is(err, ErrUpstreamAuthFailed):
		return "auth_failed"
	case errors.Is(err, ErrCredentialUnavailable):
		return "credential_unavailable"
	default:
		return "error"
	}
}
