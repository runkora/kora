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
	"fmt"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	mcpkit "github.com/clear-route/orbit/internal/platform/mcp"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverName = "orbit"

// Backends holds the in-cluster base URLs of the Orbit services the tools call.
type Backends struct {
	Agents      string
	Policy      string
	Inference   string
	Identity    string
	ToolGateway string
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
	register(srv, &sdk.Tool{Name: name, Description: desc}, h)
}

// addEnumTool is addTool with JSON-Schema enum constraints applied to the named
// fields of the generated input schema — for closed enumerable inputs (e.g.
// framework). Listed values become a real `enum` that is shown in the schema and
// validated on input before the handler runs. Open/dynamic sets (e.g. model ids,
// whose catalog changes at runtime) are NOT enums — point at the relevant list
// tool in the field description instead.
func addEnumTool[In any](srv *sdk.Server, name, desc string, enums map[string][]any, h func(context.Context, In) (*sdk.CallToolResult, error)) {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("orbitmcp: build schema for tool %q: %v", name, err))
	}
	for field, vals := range enums {
		p, ok := schema.Properties[field]
		if !ok {
			panic(fmt.Sprintf("orbitmcp: tool %q has no field %q to constrain with an enum", name, field))
		}
		p.Enum = vals
	}
	register(srv, &sdk.Tool{Name: name, Description: desc, InputSchema: schema}, h)
}

// register wires a typed handler onto the server, adapting it to the SDK's
// (result, out, err) signature. The output type is deliberately `any` (returning
// nil): our results carry their payload as text content (the backend JSON). Using
// a concrete struct here would make the SDK derive an outputSchema and emit a
// `structuredContent` object alongside the text — and clients that prefer
// structuredContent (e.g. Claude Code) would surface that empty object instead of
// the real JSON. With Out = any the SDK sets no structuredContent.
func register[In any](srv *sdk.Server, t *sdk.Tool, h func(context.Context, In) (*sdk.CallToolResult, error)) {
	sdk.AddTool(srv, t,
		func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
			res, err := h(ctx, in)
			return res, nil, err
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
