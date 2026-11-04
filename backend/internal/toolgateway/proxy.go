// Upstream MCP proxy (Phase 4).
//
// The gateway is an MCP server to consumers and an MCP client to upstreams. For
// a tools/call the gateway opens an SDK client session to the upstream server
// and forwards the call, returning the result. Session/streaming refinements
// (pooling, cancellation, backpressure) are layered on in Phase 4.2.
//
// Which credential the upstream sees is decided by UpstreamCredentials.
package toolgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// proxyClientName identifies the gateway to upstreams in the MCP handshake.
const proxyClientName = "kora-tool-gateway-proxy"

// defaultCallTimeout bounds a single upstream tools/call.
const defaultCallTimeout = 60 * time.Second

// Proxy forwards MCP operations to upstream servers.
type Proxy struct {
	creds   UpstreamCredentials
	version string
	timeout time.Duration
	logger  logging.Logger
}

// NewProxy constructs a Proxy.
func NewProxy(creds UpstreamCredentials, version string, logger logging.Logger) *Proxy {
	return &Proxy{creds: creds, version: version, timeout: defaultCallTimeout, logger: logger}
}

// CallTool forwards a tools/call to the upstream server and returns the result.
// UpstreamCredentials decides what the upstream sees of the caller.
func (p *Proxy) CallTool(ctx context.Context, srv Server, toolName string, args json.RawMessage, caller *Caller) (*sdk.CallToolResult, error) {
	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	headers, err := p.creds.Headers(ctx, srv, caller)
	if err != nil {
		return nil, fmt.Errorf("upstream credentials for %q: %w", srv.Name, err)
	}
	httpClient, upstream := upstreamClient(0, headers)

	client := sdk.NewClient(&sdk.Implementation{Name: proxyClientName, Version: p.version}, nil)
	transport := &sdk.StreamableClientTransport{Endpoint: srv.Endpoint, HTTPClient: httpClient}
	session, err := client.Connect(cctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect upstream %q: %w", srv.Name, upstream.upstreamError(err))
	}
	defer func() {
		if cerr := session.Close(); cerr != nil {
			p.logger.Debug(ctx, "closing upstream proxy session",
				logging.Field{Key: "server", Value: srv.Name},
				logging.Field{Key: "error", Value: upstream.upstreamError(cerr).Error()})
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
		return nil, fmt.Errorf("upstream tools/call %q: %w", toolName, upstream.upstreamError(err))
	}
	return res, nil
}
