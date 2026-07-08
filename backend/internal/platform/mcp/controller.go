package mcp

import (
	"net/http"

	api "github.com/clear-route/orbit/internal/platform/api"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Controller mounts an MCP server as a Streamable HTTP endpoint on the platform
// api.Server. It is the integration seam: the SDK's StreamableHTTPHandler wrapped
// with caller-token passthrough, applying NO authorisation of its own (the
// gateway pre-filters tool access; the downstream API enforces the real scope).
//
// Note: the handler is mounted via HandleFileServer (a raw http.Handler mount),
// so it sits OUTSIDE the controller Middleware() chain — deliberately, so no
// ScopeEnforcementMiddleware fronts it. A Layer-1 sidecar may still gate the pod.
type Controller struct {
	route     string
	server    *sdk.Server
	stateless bool
}

// NewController mounts server at route (e.g. "/mcp"). Stateless Streamable HTTP
// suits the per-request passthrough model: each request carries its own caller
// token and is independent.
func NewController(route string, server *sdk.Server) *Controller {
	return &Controller{route: route, server: server, stateless: true}
}

func (c *Controller) GroupedBy() string { return "" }

// Middleware is intentionally empty — the MCP server performs no authorisation,
// and caller-token extraction is done by the mounted handler wrapper.
func (c *Controller) Middleware() mw.MiddlewareHandler { return mw.MiddlewareChain() }

func (c *Controller) Boot(mux *api.Mux) {
	mux.HandleFileServer(c.route, c.Handler())
}

// Handler returns the Streamable HTTP handler with caller-token passthrough,
// usable standalone (e.g. on a plain mux in tests or a minimal binary).
func (c *Controller) Handler() http.Handler {
	h := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return c.server },
		&sdk.StreamableHTTPOptions{Stateless: c.stateless},
	)
	return callerTokenMiddleware(h)
}
