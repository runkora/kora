// Upstream MCP proxy (Phase 4).
//
// The gateway is an MCP server to consumers and an MCP client to upstreams. For
// a tools/call the gateway opens an SDK client session to the upstream server
// and forwards the call, returning the result. Session/streaming refinements
// (pooling, cancellation, backpressure) are layered on in Phase 4.2.
//
// Caller-identity on the upstream hop is default-OFF: the caller's bearer is a
// full-power aud=orbit-platform token, and forwarding it hands the upstream a
// replayable credential. The gateway forwards it ONLY to servers explicitly
// flagged callerIdentity: forward AND global (tenant_id == "", i.e. orbit-system) —
// the hard global-only rule from the tool-gateway ADR, Decision 4.
package toolgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
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

// CallTool forwards a tools/call to the upstream server and returns the result.
// callerToken is forwarded to the upstream ONLY when forwardIdentity is true
// (the caller resolved a forward-eligible global server); otherwise the upstream
// acts as itself (the gateway's sidecar-injected identity, or none).
func (p *Proxy) CallTool(ctx context.Context, srv Server, toolName string, args json.RawMessage, callerToken string, forwardIdentity bool) (*sdk.CallToolResult, error) {
	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	httpClient := http.DefaultClient
	if forwardIdentity && callerToken != "" {
		// Act AS the caller: present the caller's bearer to the upstream. Only
		// reached for global forward servers (the gateway enforces that upstream).
		httpClient = httpclient.NewHTTPClient(0, httpclient.NewTracingTransport("orbit.toolgateway-proxy", &bearerTransport{token: callerToken, base: http.DefaultTransport}))
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

// bearerTransport injects a fixed Authorization bearer on each request — used to
// forward the caller's identity to a forward-eligible upstream.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}
