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
// Phase 1: a read-only vertical proving the pattern across agents, policy, and
// inference. Write tools and the full surface are later phases.
package orbitmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

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
	registerReadTools(srv, b, jc)
	return srv
}

// registerReadTools wires the Phase 1 read-only vertical. Each tool forwards the
// caller's token and returns the backend JSON verbatim.
func registerReadTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	get := func(toolURL string) func(context.Context) (*sdk.CallToolResult, error) {
		return func(ctx context.Context) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, mcpkit.CallerToken(ctx), toolURL, nil))
		}
	}

	addTool(srv, "agents.list",
		"List the agent types (agent definitions) registered in the caller's tenant.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return get(b.Agents + "/api/v1/agent-types")(ctx)
		})

	addTool(srv, "agents.get",
		"Get a single agent type by id, including model, scopes, connectivity, and budget.",
		func(ctx context.Context, a getAgentArgs) (*sdk.CallToolResult, error) {
			if a.AgentTypeID == "" {
				return toolError(errors.New("agent_type_id is required")), nil
			}
			return get(b.Agents + "/api/v1/agent-types/" + url.PathEscape(a.AgentTypeID))(ctx)
		})

	addTool(srv, "policy.scopes",
		"List the calling principal's full effective scope set (grants + roles).",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return get(b.Policy + "/api/v1/policy/effective-scopes")(ctx)
		})

	addTool(srv, "models.list",
		"List the inference models available through Orbit, with provider and capability metadata.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return get(b.Inference + "/v1/models")(ctx)
		})
}

// ── tool argument types (schema is generated from these) ──────────────────────

type noArgs struct{}

type getAgentArgs struct {
	AgentTypeID string `json:"agent_type_id" jsonschema:"the agent type id"`
}

// ── helpers ───────────────────────────────────────────────────────────────────

// addTool registers a typed tool on the server. Out is unused (results carry
// text content), so it is fixed to noArgs.
func addTool[In any](srv *sdk.Server, name, desc string, h func(context.Context, In) (*sdk.CallToolResult, error)) {
	sdk.AddTool(srv, &sdk.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, noArgs, error) {
			res, err := h(ctx, in)
			return res, noArgs{}, err
		})
}

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
