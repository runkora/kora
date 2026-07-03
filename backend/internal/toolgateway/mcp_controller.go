// MCP request-path controller — the PEP (Phase 4.1).
//
// The gateway is a governing MCP proxy: it authenticates the caller (agent
// platform token or, later, human OAuth token — both ordinary PlatformClaims),
// filters tools/list to only the caller's granted tools, and checks every
// tools/call against mcp:<server>:<tool>:use before forwarding to the upstream.
// Default-deny is structural: Query omits ungranted tools; Check rejects an
// ungranted call even if the client guessed the name.
//
// See docs/adr/2026-06-29-mcp-tool-gateway.md.
package toolgateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
)

// mcpProtocolVersion is the MCP protocol revision the gateway advertises.
const mcpProtocolVersion = "2025-06-18"

// serverToolSep namespaces a tool by its server on the wire: "<server>__<tool>".
// It maps 1:1 to the mcp:<server>:<tool>:use scope path so the wire name and the
// authz path stay aligned (ADR Decision 5).
const serverToolSep = "__"

// jsonRPCVersion is the fixed JSON-RPC version string.
const jsonRPCVersion = "2.0"

// MCPController implements the governing MCP proxy.
type MCPController struct {
	verifier *platformtoken.Verifier
	eval     policy.Evaluator
	catalog  CatalogRepository
	registry *Registry
	proxy    *Proxy
	// basePath is the gateway's base path, used to form the (host-derived) RFC
	// 9728 protected-resource metadata URL advertised in the 401 challenge.
	basePath string
	logger   logging.Logger
	tracer   trace.Tracer
}

// NewMCPController constructs the request-path controller. basePath forms the
// 401 challenge's protected-resource metadata URL (host derived per request).
func NewMCPController(verifier *platformtoken.Verifier, eval policy.Evaluator, catalog CatalogRepository, registry *Registry, proxy *Proxy, basePath string, logger logging.Logger) *MCPController {
	return &MCPController{
		verifier: verifier,
		eval:     eval,
		catalog:  catalog,
		registry: registry,
		proxy:    proxy,
		basePath: basePath,
		logger:   logger,
		tracer:   otel.Tracer("orbit.tool-gateway"),
	}
}

// GroupedBy returns the route group prefix (none — the MCP route sits directly
// under the server base path).
func (c *MCPController) GroupedBy() string { return "" }

// Middleware enforces platform JWT auth. The coarse mcp:invoke gate is enforced
// at the Envoy sidecar (the registered route); this app-level PEP does the
// per-tool Check.
func (c *MCPController) Middleware() mw.MiddlewareHandler {
	// challengeMiddleware runs first so its writer wraps AuthMiddleware's 401,
	// adding the RFC 9728 WWW-Authenticate pointer to the protected-resource metadata.
	return mw.MiddlewareChain(
		challengeMiddleware(c.basePath),
		mw.AuthMiddleware(c.verifier, c.logger),
	)
}

// Boot registers the Streamable HTTP JSON-RPC entrypoint.
func (c *MCPController) Boot(mux *api.Mux) {
	mux.Handle("POST /mcp", c.handleMCP)
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

// handleMCP dispatches an MCP JSON-RPC request by method.
func (c *MCPController) handleMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
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
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "initialize":
		c.writeRPCResult(ctx, w, req.ID, c.initializeResult())
	case "ping":
		c.writeRPCResult(ctx, w, req.ID, map[string]any{})
	case "tools/list":
		c.handleToolsList(ctx, w, req, claims)
	case "tools/call":
		c.handleToolsCall(ctx, w, r, req, claims)
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

// handleToolsList returns only the caller's granted tools across all registered
// servers, namespaced <server>__<tool>. Candidates come from the catalog; the
// filter is one Query whose prefix rule lets a server-level grant cover the set.
func (c *MCPController) handleToolsList(ctx context.Context, w http.ResponseWriter, req jsonRPCRequest, claims *platformtoken.PlatformClaims) {
	tools, err := c.catalog.ListByTenant(ctx, claims.Orbit.TenantID)
	if err != nil {
		c.writeRPCError(ctx, w, req.ID, -32603, "failed to load tool catalog")
		return
	}

	candidates := make([]policy.Candidate, 0, len(tools))
	byWireName := make(map[string]CatalogTool, len(tools))
	for _, t := range tools {
		wire := t.ServerSlug + serverToolSep + t.ToolName
		byWireName[wire] = t
		candidates = append(candidates, policy.Candidate{
			ID:           wire,
			PathSegments: toolScopePath(t.ServerSlug, t.ToolName),
		})
	}

	allowed, err := c.eval.Query(ctx, policy.QueryInput{
		Claims:       claims,
		ResourceType: "mcp",
		Action:       scopeAction,
		Candidates:   candidates,
	})
	if err != nil {
		c.writeRPCError(ctx, w, req.ID, -32603, "policy query failed")
		return
	}

	out := make([]mcpTool, 0, len(allowed))
	for _, wire := range allowed {
		t := byWireName[wire]
		out = append(out, mcpTool{Name: wire, Description: t.Description, InputSchema: t.InputSchema})
	}
	c.logger.Info(ctx, "tools/list filtered",
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "candidates", Value: len(candidates)},
		logging.Field{Key: "allowed", Value: len(out)})
	c.writeRPCResult(ctx, w, req.ID, map[string]any{"tools": out})
}

// callParams is the params of a tools/call.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// handleToolsCall checks mcp:<server>:<tool>:use and, if allowed, forwards to
// the upstream. Default-deny: an ungranted call is rejected before the upstream.
func (c *MCPController) handleToolsCall(ctx context.Context, w http.ResponseWriter, r *http.Request, req jsonRPCRequest, claims *platformtoken.PlatformClaims) {
	var params callParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		c.writeRPCError(ctx, w, req.ID, -32602, "invalid params")
		return
	}

	server, tool, ok := strings.Cut(params.Name, serverToolSep)
	if !ok || server == "" || tool == "" {
		c.writeRPCError(ctx, w, req.ID, -32602, "tool name must be <server>__<tool>")
		return
	}

	srv, found := c.registry.lookup(server)
	if !found {
		c.writeRPCError(ctx, w, req.ID, -32602, "unknown server")
		return
	}

	ctx, span := c.tracer.Start(ctx, "mcp.tools.call")
	defer span.End()
	span.SetAttributes(
		attribute.String("mcp.server", server),
		attribute.String("mcp.tool", tool),
		attribute.String("orbit.tenant_id", claims.Orbit.TenantID),
		attribute.String("orbit.principal", claims.Subject),
	)

	res, err := c.eval.Check(ctx, policy.CheckInput{
		Claims:       claims,
		ResourceType: "mcp",
		PathSegments: toolScopePath(server, tool),
		Action:       scopeAction,
	})
	if err != nil {
		span.SetAttributes(attribute.String("mcp.decision", "error"))
		c.writeRPCError(ctx, w, req.ID, -32603, "policy check failed")
		return
	}
	if !res.Allow {
		span.SetAttributes(attribute.String("mcp.decision", "deny"))
		c.logger.Info(ctx, "tools/call denied",
			logging.Field{Key: "server", Value: server},
			logging.Field{Key: "tool", Value: tool},
			logging.Field{Key: "principal", Value: claims.Subject},
			logging.Field{Key: "reason", Value: res.Reason})
		c.writeRPCError(ctx, w, req.ID, -32001, "tool not permitted")
		return
	}
	span.SetAttributes(attribute.String("mcp.decision", "allow"))
	c.logger.Info(ctx, "tools/call allowed",
		logging.Field{Key: "server", Value: server},
		logging.Field{Key: "tool", Value: tool},
		logging.Field{Key: "principal", Value: claims.Subject})

	// callerIdentity: forward only for global (tenant_id == "") forward servers.
	forward := srv.CallerIdentity == callerIdentityForward && srv.TenantID == ""
	result, err := c.proxy.CallTool(ctx, srv, tool, params.Arguments, bearerFromRequest(r), forward)
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

// initializeResult is the gateway's MCP server capabilities.
func (c *MCPController) initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "orbit-tool-gateway", "version": "0.1.0"},
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
