// Package toolgateway is the MCP tool-gateway's publisher-mode core (Phase 3):
// it mounts the operator-rendered orbit-mcp-servers registry, introspects each
// server's tool surface (initialize + tools/list), and (Phase 3.3) publishes the
// discovered tools as mcp:<server>:<tool>:use scopes.
//
// The request-path PEP + proxy (Phase 4) live alongside this in the same binary
// but are a separate concern; this file is registration/introspection only.
//
// See docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
package toolgateway

import (
	"encoding/json"
	"time"
)

// callerIdentityForward is the caller_identity value meaning "forward the
// caller's bearer to the upstream" (valid only for global servers).
const callerIdentityForward = "forward"

// Server is one entry of the mounted servers.json registry — the runtime
// contract the operator's MCPServerAggregator renders. tenant_id is "" for a
// global (orbit-system) server and the owning tenant for a tenant server.
type Server struct {
	TenantID                    string `json:"tenant_id"`
	Name                        string `json:"name"`
	DisplayName                 string `json:"display_name"`
	Description                 string `json:"description"`
	Endpoint                    string `json:"endpoint"`
	Transport                   string `json:"transport"`
	ReintrospectIntervalSeconds int    `json:"reintrospect_interval_seconds"`
	CallerIdentity              string `json:"caller_identity"`
	// AuthHeaders are static credential headers whose values are files in the
	// mounted orbit-mcp-server-secrets Secret.
	AuthHeaders []AuthHeader `json:"auth_headers,omitempty"`
}

// AuthHeader is one credential header: Name set to Prefix followed by the
// value stored under SecretKey.
type AuthHeader struct {
	Name      string `json:"name"`
	Prefix    string `json:"prefix,omitempty"`
	SecretKey string `json:"secret_key"`
}

// Tool is one tool discovered on a server via tools/list. InputSchema is the
// raw JSON Schema the server advertises, retained for the catalog (Phase 3.4).
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	// RequiresScopes are platform scopes this tool needs upstream, declared by
	// a global server in the tool's _meta["orbit/requires"].
	// Published on the leaf scope declaration so the effective grant confers them.
	// Always empty for a tenant server.
	RequiresScopes []string `json:"requires_scopes,omitempty"`
}

// ServerState is the introspection health of a server (gateway runtime state —
// NOT the CR status the operator owns).
type ServerState string

const (
	// StatePending means the server has not been introspected yet this lifetime.
	StatePending ServerState = "pending"
	// StateOK means the last introspection succeeded.
	StateOK ServerState = "ok"
	// StateDegraded means the last introspection failed; the server's prior
	// tools are retained in the keep-list and it is flagged in metrics/catalog.
	StateDegraded ServerState = "degraded"
)

// ServerSnapshot is the introspection result for one server: its registry
// entry, the tools last successfully discovered (retained across a failure),
// the current state, and — when degraded — the last error.
type ServerSnapshot struct {
	Server             Server
	Tools              []Tool
	State              ServerState
	LastErr            string
	LastIntrospectedAt time.Time
	// Reached is true once any introspection of this server has succeeded.
	Reached bool
}
