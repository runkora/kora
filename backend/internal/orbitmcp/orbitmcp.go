// Package orbitmcp is the Orbit MCP server: the first platform-global MCP server.
// It exposes Orbit's own management surface (agents, policy, models, …) as MCP
// tools so an agent can operate the control plane. This package holds the TOOLS
// ONLY — the protocol/transport/passthrough live in the reusable internal/
// platform/mcp kit, and the HTTP forwarding in internal/platform/clients/orbitjson.
//
// Identity model: passthrough. Each tool forwards the CALLER's token to the
// relevant Orbit API (via orbitjson + the kit's CallerToken); the downstream
// service enforces the real platform scope. This server holds no authority.
//
// Tools are organized one file per domain (tools_agents.go, tools_models.go,
// tools_policy.go, …) and wired together in register.go.
package orbitmcp

import (
	"context"
	"encoding/json"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	mcpkit "github.com/clear-route/orbit/internal/platform/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverName = "orbit"

// Backends holds the in-cluster base URLs of the Orbit services the tools call.
type Backends struct {
	Agents    string
	Policy    string
	Inference string
	Identity  string
}

// NewServer builds the Orbit MCP server and registers its tools onto a kit server.
func NewServer(b Backends, jc *orbitjson.Client) *sdk.Server {
	srv := mcpkit.NewServer(serverName, "0.1.0")
	registerTools(srv, b, jc)
	return srv
}

// ── shared tool-construction helpers ──────────────────────────────────────────

// noArgs is the empty input/output type for tools that take no arguments.
type noArgs struct{}

// addTool registers a typed tool on the server. Out is unused (results carry
// text content), so it is fixed to noArgs.
func addTool[In any](srv *sdk.Server, name, desc string, h func(context.Context, In) (*sdk.CallToolResult, error)) {
	sdk.AddTool(srv, &sdk.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, noArgs, error) {
			res, err := h(ctx, in)
			return res, noArgs{}, err
		})
}

// callerToken returns the bearer the kit lifted from the inbound request.
func callerToken(ctx context.Context) string { return mcpkit.CallerToken(ctx) }

// jsonResult turns a (rawJSON, err) into an MCP tool result: the JSON as text on
// success, or an MCP isError result on failure (so the model sees and can react).
func jsonResult(body json.RawMessage, err error) (*sdk.CallToolResult, error) {
	if err != nil {
		return toolError(err), nil
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(body)}}}, nil
}

func toolError(err error) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: "Error: " + err.Error()}},
	}
}
