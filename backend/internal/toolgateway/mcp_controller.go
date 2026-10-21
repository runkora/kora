// MCP request-path controller — the PEP (Phase 4.1).
//
// The gateway is a governing MCP proxy. Each upstream server is mounted at its
// own path — POST /v1/mcp/<server> — so a client connects per server (ADR
// Decision 5, the per-server-mount-paths option). The gateway authenticates the
// caller (agent platform token or human OAuth token — both ordinary
// PlatformClaims), filters tools/list to only the caller's granted tools on that
// server, and checks every tools/call against mcp:<server>:<tool>:use before
// forwarding to the upstream. Default-deny is structural: Query omits ungranted
// tools; Check rejects an ungranted call even if the client guessed the name.
//
// See docs/adr/2026-06-29-mcp-tool-gateway.md.
package toolgateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
)

// mcpProtocolVersion is the MCP protocol revision the gateway advertises.
const mcpProtocolVersion = "2025-06-18"

// jsonRPCVersion is the fixed JSON-RPC version string.
const jsonRPCVersion = "2.0"

// MCPController implements the governing MCP proxy.
type MCPController struct {
	verifier  *platformtoken.Verifier
	blocklist mw.JTIBlocklist
	eval      policy.Evaluator
	catalog   CatalogRepository
	registry  *Registry
	proxy     *Proxy
	basePath  string // the gateway base path (e.g. /v1), used to form the per-server resource
	metrics   *Metrics
	logger    logging.Logger
	tracer    trace.Tracer
}

// NewMCPController constructs the request-path controller. basePath forms the
// per-server resource in the 401 challenge (host derived per request).
// blocklist holds revoked delegation JTIs. metrics may be nil (e.g. in
// tests), in which case tools/call recording is a no-op.
func NewMCPController(verifier *platformtoken.Verifier, eval policy.Evaluator, catalog CatalogRepository, registry *Registry, proxy *Proxy, blocklist mw.JTIBlocklist, basePath string, metrics *Metrics, logger logging.Logger) *MCPController {
	return &MCPController{
		verifier:  verifier,
		blocklist: blocklist,
		eval:      eval,
		catalog:   catalog,
		registry:  registry,
		proxy:     proxy,
		basePath:  basePath,
		metrics:   metrics,
		logger:    logger,
		tracer:    otel.Tracer("orbit.tool-gateway"),
	}
}

// GroupedBy returns the route group prefix (none — the MCP route sits directly
// under the server base path).
func (c *MCPController) GroupedBy() string { return "" }

// Middleware enforces platform JWT auth and verifies the optional delegation
// token. The coarse mcp:invoke gate is enforced at the Envoy sidecar (the
// registered route); this app-level PEP does the per-tool Check.
func (c *MCPController) Middleware() mw.MiddlewareHandler {
	// challengeMiddleware runs first so its writer wraps AuthMiddleware's 401,
	// adding the RFC 9728 WWW-Authenticate pointer to the protected-resource metadata.
	// The delegation middleware puts the run's verified delegator into context
	// for every tool decision below and refuses a revoked delegation.
	return mw.MiddlewareChain(
		challengeMiddleware(c.basePath),
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.DelegationMiddlewareWithBlocklist(c.logger, c.verifier, c.blocklist),
	)
}

// Boot registers the per-server Streamable HTTP JSON-RPC entrypoint. The {server}
// path segment selects the upstream server this connection is scoped to.
func (c *MCPController) Boot(mux *api.Mux) {
	mux.Handle("POST /mcp/{server}", c.handleMCP)
}

// jsonRPCRequest is an inbound JSON-RPC message.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse is an outbound JSON-RPC message.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handleMCP dispatches an MCP JSON-RPC request for the server named in the path.
func (c *MCPController) handleMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Kora.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}

	// The connection is scoped to one server, selected by the URL path.
	server := r.PathValue("server")
	srv, found := c.registry.lookup(server)
	if !found {
		api.ErrorResponse(ctx, w, c.logger,
			api.NotFoundError(api.ErrCodeResourceNotFound, "unknown MCP server", nil), nil)
		return
	}
	// Tenant-ownership binding. A global server (TenantID == "") is reachable by
	// every tenant; a tenant-scoped server only by its owner. lookup() resolves by
	// name alone and the scope vocabulary is tenant-blind in the MVP (two tenants'
	// same-named servers share one scope string), so without this a tenant holding
	// mcp:<server>:<tool>:use could reach — or shadow — another tenant's server.
	// Reject as 404 (indistinguishable from an unknown server) so a cross-tenant
	// server can't even be probed. Covers tools/list, tools/call, and initialize.
	if srv.TenantID != "" && srv.TenantID != claims.Kora.TenantID {
		api.ErrorResponse(ctx, w, c.logger,
			api.NotFoundError(api.ErrCodeResourceNotFound, "unknown MCP server", nil), nil)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeRPCError(ctx, w, nil, -32700, "parse error")
		return
	}
	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.writeRPCError(ctx, w, nil, -32700, "parse error")
		return
	}

	// MCP notifications carry no id and expect no response body (202).
	if strings.HasPrefix(req.Method, "notifications/") {
		api.NoBody(w, http.StatusAccepted)
		return
	}

	switch req.Method {
	case "initialize":
		c.writeRPCResult(ctx, w, req.ID, c.initializeResult(server))
	case "ping":
		c.writeRPCResult(ctx, w, req.ID, map[string]any{})
	case "tools/list":
		c.handleToolsList(ctx, w, req, claims, server)
	case "tools/call":
		c.handleToolsCall(ctx, w, r, req, claims, server, srv)
	default:
		c.writeRPCError(ctx, w, req.ID, -32601, "method not found")
	}
}

// mcpTool is a tool in a tools/list result.
type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// handleToolsList returns only the caller's granted tools on this server, with
// plain (un-namespaced) tool names — the connection is already scoped to the
// server by its path. The filter is one Query whose prefix rule lets a
// server-level grant cover the whole list.
func (c *MCPController) handleToolsList(ctx context.Context, w http.ResponseWriter, req jsonRPCRequest, claims *platformtoken.PlatformClaims, server string) {
	tools, err := c.catalog.ListByTenant(ctx, claims.Kora.TenantID)
	if err != nil {
		c.writeRPCError(ctx, w, req.ID, -32603, "failed to load tool catalog")
		return
	}

	candidates := make([]policy.Candidate, 0, len(tools))
	byName := make(map[string]CatalogTool, len(tools))
	for _, t := range tools {
		if t.ServerSlug != server {
			continue
		}
		byName[t.ToolName] = t
		candidates = append(candidates, policy.Candidate{
			ID:           t.ToolName,
			PathSegments: toolScopePath(server, t.ToolName),
		})
	}

	allowed, err := c.eval.Query(ctx, policy.QueryInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "mcp",
		Action:       scopeAction,
		Candidates:   candidates,
	})
	if err != nil {
		c.writeRPCError(ctx, w, req.ID, -32603, "policy query failed")
		return
	}

	out := make([]mcpTool, 0, len(allowed))
	for _, name := range allowed {
		t := byName[name]
		out = append(out, mcpTool{Name: name, Description: t.Description, InputSchema: t.InputSchema})
	}
	c.logger.Info(ctx, "tools/list filtered",
		logging.Field{Key: "tenant_id", Value: claims.Kora.TenantID},
		logging.Field{Key: "server", Value: server},
		logging.Field{Key: "candidates", Value: len(candidates)},
		logging.Field{Key: "allowed", Value: len(out)})
	c.writeRPCResult(ctx, w, req.ID, map[string]any{"tools": out})
}

// callParams is the params of a tools/call.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// handleToolsCall checks mcp:<server>:<tool>:use and, if allowed, forwards to the
// upstream. The server is fixed by the connection's path; the tool name is plain.
// Default-deny: an ungranted call is rejected before the upstream.
func (c *MCPController) handleToolsCall(ctx context.Context, w http.ResponseWriter, r *http.Request, req jsonRPCRequest, claims *platformtoken.PlatformClaims, server string, srv Server) {
	start := time.Now()
	var params callParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		c.metrics.recordInvocation(ctx, server, "error", claims.Kora.TenantID, time.Since(start))
		c.writeRPCError(ctx, w, req.ID, -32602, "invalid params")
		return
	}
	tool := params.Name
	if tool == "" {
		c.metrics.recordInvocation(ctx, server, "error", claims.Kora.TenantID, time.Since(start))
		c.writeRPCError(ctx, w, req.ID, -32602, "tool name is required")
		return
	}

	ctx, span := c.tracer.Start(ctx, "mcp.tools.call")
	defer span.End()
	span.SetAttributes(
		attribute.String("mcp.server", server),
		attribute.String("mcp.tool", tool),
		attribute.String("orbit.tenant_id", claims.Kora.TenantID),
		attribute.String("orbit.principal", claims.Subject),
	)

	res, err := c.eval.Check(ctx, policy.CheckInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "mcp",
		PathSegments: toolScopePath(server, tool),
		Action:       scopeAction,
		RunID:        mw.RunIDFromContext(ctx),
	})
	if err != nil {
		span.SetAttributes(attribute.String("mcp.decision", "error"))
		c.metrics.recordInvocation(ctx, server, "error", claims.Kora.TenantID, time.Since(start))
		c.writeRPCError(ctx, w, req.ID, -32603, "policy check failed")
		return
	}
	if !res.Allow {
		span.SetAttributes(attribute.String("mcp.decision", "deny"))
		c.metrics.recordInvocation(ctx, server, "deny", claims.Kora.TenantID, time.Since(start))
		c.logger.Info(ctx, "tools/call denied",
			logging.Field{Key: "server", Value: server},
			logging.Field{Key: "tool", Value: tool},
			logging.Field{Key: "principal", Value: claims.Subject},
			logging.Field{Key: "reason", Value: res.Reason})
		// Pass the OPA deny reason through verbatim: it distinguishes which leg
		// of the delegated intersection denied (deny:delegation_required,
		// deny:no_delegator_grant, …) and this RPC error is what surfaces in
		// the run's transcript (delegated-agent-identity ADR, Phase 5). Reasons
		// are decision labels from our own bundle — same disclosure as the
		// scope-enforcement middleware's writeForbidden.
		c.writeRPCError(ctx, w, req.ID, -32001, "tool not permitted ("+res.Reason+")")
		return
	}
	span.SetAttributes(attribute.String("mcp.decision", "allow"))
	c.logger.Info(ctx, "tools/call allowed",
		logging.Field{Key: "server", Value: server},
		logging.Field{Key: "tool", Value: tool},
		logging.Field{Key: "principal", Value: claims.Subject})

	// callerIdentity: forward only for global (tenant_id == "") forward servers.
	// External servers use their own upstream credentials and never see the caller's.
	var caller *forwardedCaller
	if srv.CallerIdentity == callerIdentityForward && srv.TenantID == "" {
		caller = &forwardedCaller{bearer: bearerFromRequest(r), delegation: r.Header.Get(mw.DelegationTokenHeader)}
	}
	// The timer runs through this call: an allowed invocation's recorded
	// duration is end-to-end (policy check + upstream round trip), which is
	// the number worth alerting on — "how long did this tool call actually
	// take" — not just how fast the policy decided. recordInvocation fires
	// once we have that full elapsed time, whether or not the upstream call
	// itself succeeded; the policy decision stays "allow" either way, since
	// that label describes the authorization outcome, not the upstream result.
	result, err := c.proxy.CallTool(ctx, srv, tool, params.Arguments, caller)
	c.metrics.recordInvocation(ctx, server, "allow", claims.Kora.TenantID, time.Since(start))
	if err != nil {
		span.RecordError(err)
		c.logger.Error(ctx, "upstream tools/call failed",
			logging.Field{Key: "server", Value: server},
			logging.Field{Key: "tool", Value: tool},
			logging.Field{Key: "error", Value: err.Error()})
		c.writeRPCError(ctx, w, req.ID, -32603, "upstream call failed")
		return
	}
	c.writeRPCResult(ctx, w, req.ID, result)
}

// initializeResult is the gateway's MCP server capabilities for one mounted server.
func (c *MCPController) initializeResult(server string) map[string]any {
	return map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "orbit/" + server, "version": "0.1.0"},
	}
}

func (c *MCPController) writeRPCResult(ctx context.Context, w http.ResponseWriter, id json.RawMessage, result any) {
	api.JSON(ctx, w, c.logger, http.StatusOK, jsonRPCResponse{JSONRPC: jsonRPCVersion, ID: id, Result: result})
}

func (c *MCPController) writeRPCError(ctx context.Context, w http.ResponseWriter, id json.RawMessage, code int, message string) {
	api.JSON(ctx, w, c.logger, http.StatusOK, jsonRPCResponse{
		JSONRPC: jsonRPCVersion, ID: id, Error: &jsonRPCError{Code: code, Message: message},
	})
}

// bearerFromRequest extracts the raw bearer token from the Authorization header.
func bearerFromRequest(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return after
	}
	return ""
}
