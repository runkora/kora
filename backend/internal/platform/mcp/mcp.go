// Package mcp is the reusable platform kit for building gateway-fronted Orbit
// MCP servers on the official Go MCP SDK (github.com/modelcontextprotocol/go-sdk).
//
// It owns the platform seams so each MCP server is "just its tools": the
// Streamable HTTP transport, caller-token passthrough (the server itself does
// NO authorisation — the gateway pre-filters and the downstream API enforces),
// mounting onto the platform api.Server, and the dotted-tool-name → scope-path
// convention used by the registration ADR's introspection.
//
// Scope: this kit targets gateway-fronted platform MCP servers (Streamable HTTP,
// passthrough). It is NOT for agent-runtime-internal stdio MCPs.
package mcp

import (
	"context"
	"net/http"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates an MCP server. Register tools onto it with the SDK's typed
// mcp.AddTool[In, Out] (schema is generated from the Go input struct).
func NewServer(name, version string) *sdk.Server {
	return sdk.NewServer(&sdk.Implementation{Name: name, Version: version}, nil)
}

// ── Caller-token passthrough ──────────────────────────────────────────────────

type ctxKey int

const callerTokenKey ctxKey = iota

// WithCallerToken stores the caller's bearer token in the context.
func WithCallerToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, callerTokenKey, token)
}

// CallerToken returns the caller's bearer token (no "Bearer " prefix), or "".
// Tool handlers use it to build caller-scoped downstream clients — the call then
// carries the CALLER's identity, and the downstream service enforces the real
// platform scope. The kit verifies nothing.
func CallerToken(ctx context.Context) string {
	t, _ := ctx.Value(callerTokenKey).(string)
	return t
}

// callerTokenMiddleware lifts the inbound Authorization bearer into the request
// context so it propagates (via the SDK's request context) to tool handlers.
// It does NOT verify the token — authorisation is the gateway's and the
// downstream service's job.
func callerTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if raw := strings.TrimSpace(r.Header.Get("Authorization")); raw != "" {
			r = r.WithContext(WithCallerToken(r.Context(), strings.TrimPrefix(raw, "Bearer ")))
		}
		next.ServeHTTP(w, r)
	})
}

// ── Scope-path convention ─────────────────────────────────────────────────────

// ScopePath splits a dotted MCP tool name into scope-path segments per the
// registration ADR's multi-segment amendment: "agents.create" → ["agents",
// "create"], registered as mcp:<server>:agents:create:use. A non-dotted name
// yields a single segment. The "." is the reserved delimiter; each segment is
// expected to be charset-safe ([a-z0-9_-]).
func ScopePath(toolName string) []string {
	return strings.Split(toolName, ".")
}
