// Package mcp is the reusable platform kit for building gateway-fronted kora
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

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// NewServer creates an MCP server. Register tools onto it with the SDK's typed
// mcp.AddTool[In, Out] (schema is generated from the Go input struct).
func NewServer(name, version string) *sdk.Server {
	return sdk.NewServer(&sdk.Implementation{Name: name, Version: version}, nil)
}

// ── Caller-token passthrough ──────────────────────────────────────────────────

type ctxKey int

const (
	callerTokenKey ctxKey = iota
	callerDelegationKey
)

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

// WithCallerDelegation stores the caller's raw delegation token in the context.
func WithCallerDelegation(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, callerDelegationKey, token)
}

// CallerDelegation returns the caller's raw delegation token, or "" for an
// autonomous caller. Tool handlers pass it downstream with the bearer so the
// downstream service evaluates the delegated run. The kit verifies nothing.
func CallerDelegation(ctx context.Context) string {
	t, _ := ctx.Value(callerDelegationKey).(string)
	return t
}

// callerTokenMiddleware lifts the inbound Authorization bearer and delegation
// token into the request context so they propagate (via the SDK's request
// context) to tool handlers. It does NOT verify either token — authorisation is
// the gateway's and the downstream service's job.
func callerTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if raw := strings.TrimSpace(r.Header.Get("Authorization")); raw != "" {
			ctx = WithCallerToken(ctx, strings.TrimPrefix(raw, "Bearer "))
		}
		if raw := strings.TrimSpace(r.Header.Get(mw.DelegationTokenHeader)); raw != "" {
			ctx = WithCallerDelegation(ctx, raw)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
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
