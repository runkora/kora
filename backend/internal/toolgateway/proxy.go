// Upstream MCP proxy (Phase 4).
//
// The gateway is an MCP server to consumers and an MCP client to upstreams. For
// a tools/call the gateway opens an SDK client session to the upstream server
// and forwards the call, returning the result. Session/streaming refinements
// (pooling, cancellation, backpressure) are layered on in Phase 4.2.
//
// Caller-identity on the upstream hop is default-OFF: the caller's bearer is a
// full-power aud=orbit-platform token, and forwarding it hands the upstream a
// replayable credential. The gateway forwards it, with the run's delegation token,
// ONLY to servers explicitly flagged callerIdentity: forward AND global
// (tenant_id == "", i.e. orbit-system) — the hard global-only rule from the
// tool-gateway ADR, Decision 4.
package toolgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// proxyClientName identifies the gateway to upstreams in the MCP handshake.
const proxyClientName = "orbit-tool-gateway-proxy"

// defaultCallTimeout bounds a single upstream tools/call.
const defaultCallTimeout = 60 * time.Second

// Proxy forwards MCP operations to upstream servers.
type Proxy struct {
	version string
	timeout time.Duration
	logger  logging.Logger
}

// NewProxy constructs a Proxy.
func NewProxy(version string, logger logging.Logger) *Proxy {
	return &Proxy{version: version, timeout: defaultCallTimeout, logger: logger}
}

// forwardedCaller is the caller's credentials presented to a forward-identity
// upstream: the platform bearer and, for a delegated run, its delegation token.
type forwardedCaller struct {
	bearer     string
	delegation string
}

// CallTool forwards a tools/call to the upstream server and returns the result.
// A non-nil caller is presented to the upstream, which the gateway passes only
// for a forward-eligible global server. With a nil caller the upstream acts as
// itself (the gateway's sidecar-injected identity, or none).
func (p *Proxy) CallTool(ctx context.Context, srv Server, toolName string, args json.RawMessage, caller *forwardedCaller) (*sdk.CallToolResult, error) {
	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	httpClient := http.DefaultClient
	if caller != nil && caller.bearer != "" {
		httpClient = httpclient.NewHTTPClient(0, httpclient.NewTracingTransport("orbit.toolgateway-proxy", &callerTransport{caller: *caller, base: http.DefaultTransport}))
	}

	client := sdk.NewClient(&sdk.Implementation{Name: proxyClientName, Version: p.version}, nil)
	transport := &sdk.StreamableClientTransport{Endpoint: srv.Endpoint, HTTPClient: httpClient}
	session, err := client.Connect(cctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect upstream %q: %w", srv.Name, err)
	}
	defer func() {
		if cerr := session.Close(); cerr != nil {
			p.logger.Debug(ctx, "closing upstream proxy session",
				logging.Field{Key: "server", Value: srv.Name},
				logging.Field{Key: "error", Value: cerr.Error()})
		}
	}()

	var arguments any
	if len(args) > 0 {
		if uerr := json.Unmarshal(args, &arguments); uerr != nil {
			return nil, fmt.Errorf("decode tool arguments: %w", uerr)
		}
	}

	res, err := session.CallTool(cctx, &sdk.CallToolParams{Name: toolName, Arguments: arguments})
	if err != nil {
		return nil, fmt.Errorf("upstream tools/call %q: %w", toolName, err)
	}
	return res, nil
}

// callerTransport presents the caller's credentials on each upstream request.
// The delegation travels with the bearer so the upstream's platform calls are
// evaluated as the delegated run, not as the bare agent.
type callerTransport struct {
	caller forwardedCaller
	base   http.RoundTripper
}

func (t *callerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.caller.bearer)
	if t.caller.delegation != "" {
		r.Header.Set(mw.DelegationTokenHeader, t.caller.delegation)
	}
	return t.base.RoundTrip(r)
}
