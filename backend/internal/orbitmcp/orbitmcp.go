// Package orbitmcp is the kora MCP server: the first platform-global MCP server.
// It exposes kora's own management surface (agents, policy, models, …) as MCP
// tools so an agent can operate the control plane. This package holds the TOOLS
// ONLY — the protocol/transport/passthrough live in the reusable internal/
// platform/mcp kit, and the HTTP forwarding in internal/platform/clients/orbitjson.
//
// Identity model: passthrough. Each tool forwards the CALLER's token to the
// relevant kora API (via orbitjson + the kit's CallerToken); the downstream
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

// Backends holds the in-cluster base URLs of the kora services the tools call.
type Backends struct {
	Agents      string
	Policy      string
	Inference   string
	Identity    string
	Triggers    string
	ToolGateway string
}

// productNamer is the one brand method the server calls.
type productNamer interface {
	ProductName() string
}

// NewServer builds the MCP server, named after brand, and registers its tools onto a kit server.
func NewServer(b Backends, jc *orbitjson.Client, brand productNamer) *sdk.Server {
	product := brand.ProductName()
	srv := mcpkit.NewServer(product, "0.1.0")
	registerTools(srv, b, jc, product)
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
	// Emit the tool's upstream platform scopes as _meta["orbit/requires"] so the
	// tool-gateway publishes them and a grant of this tool's mcp:orbit:<tool>:use
	// scope confers them in the effective set (see toolRequires).
	if req, ok := toolRequires[t.Name]; ok {
		t.Meta = sdk.Meta{metaRequiresKey: req}
	}
	sdk.AddTool(srv, t,
		func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
			res, err := h(ctx, in)
			return res, nil, err
		})
}

// metaRequiresKey is the tool _meta field carrying the platform scopes a tool
// calls upstream. Mirrors toolgateway.metaRequiresKey.
const metaRequiresKey = "orbit/requires"

// toolRequires maps an orbit MCP tool to the platform scopes it calls upstream —
// the scope the tool-gateway's forwarded token needs for the tool to succeed.
// Emitted per tool as _meta so a fine-grained grant of the tool confers exactly
// that scope in the effective set, no more. Tools absent here need no platform
// scope (e.g. agents.mcp_tools reads the auth-only catalog; policy.scopes is
// HUMAN-only and no agent scope makes it work). The single source of truth for
// the orbit (first-party, platform-calling) MCP; external MCPs declare nothing.
var toolRequires = map[string][]string{
	"agents.list":       {"agents:types:read"},
	"agents.get":        {"agents:types:read"},
	"agents.create":     {"agents:types:write"},
	"agents.update":     {"agents:types:write"},
	"agents.delete":     {"agents:types:write"},
	"agents.connectors": {"platform:connections:read"},

	"agents.environments.list":    {"agents:environments:read"},
	"agents.environments.get":     {"agents:environments:read"},
	"agents.environments.create":  {"agents:environments:write"},
	"agents.environments.replace": {"agents:environments:write"},
	"agents.environments.delete":  {"agents:environments:write"},
	"agents.sandbox_images.list":  {"agents:environments:read"},
	"agents.environments.sizes":   {"agents:environments:read"},

	"models.list":                   {"inference:list"},
	"models.virtual.list":           {"platform:catalog:read"},
	"models.virtual.get":            {"platform:catalog:read"},
	"models.virtual.create":         {"platform:catalog:write"},
	"models.virtual.update":         {"platform:catalog:write"},
	"models.virtual.delete":         {"platform:catalog:write"},
	"models.providers.status":       {"platform:catalog:read"},
	"models.providers.list":         {"platform:catalog:read"},
	"models.providers.refresh":      {"platform:catalog:write"},
	"models.providers.set_enabled":  {"platform:catalog:write"},
	"models.pricing.list":           {"platform:catalog:read"},
	"models.pricing.set_override":   {"platform:catalog:write"},
	"models.pricing.clear_override": {"platform:catalog:write"},

	"policy.grants.list":       {"platform:grants:read"},
	"policy.grants.get":        {"platform:grants:read"},
	"policy.grants.create":     {"platform:grants:write"},
	"policy.grants.delete":     {"platform:grants:write"},
	"policy.grants.set_budget": {"platform:budgets:write"},
	"policy.roles.list":        {"platform:roles:read"},
	"policy.roles.get":         {"platform:roles:read"},
	"policy.roles.upsert":      {"platform:roles:write"},
	"policy.roles.delete":      {"platform:roles:write"},
	"policy.vocabulary.list":   {"platform:grants:read"},
	"policy.vocabulary.get":    {"platform:grants:read"},

	"budgets.tenant.get":        {"platform:budgets:read"},
	"budgets.tenant.set":        {"platform:budgets:write"},
	"budgets.tenant.delete":     {"platform:budgets:write"},
	"budgets.principals.status": {"platform:budgets:read"},

	"triggers.sources.list":          {"triggers:read"},
	"triggers.sources.get":           {"triggers:read"},
	"triggers.sources.create":        {"triggers:write"},
	"triggers.sources.update":        {"triggers:write"},
	"triggers.sources.delete":        {"triggers:write"},
	"triggers.list":                  {"triggers:read"},
	"triggers.get":                   {"triggers:read"},
	"triggers.create":                {"triggers:write"},
	"triggers.update":                {"triggers:write"},
	"triggers.schedule_preview":      {"triggers:write"},
	"triggers.delete":                {"triggers:write"},
	"triggers.deliveries.list":       {"triggers:read"},
	"triggers.providers.event_types": {"triggers:read"},
	"triggers.providers.fields":      {"triggers:read"},

	"runs.list":         {"agents:runs:read"},
	"runs.get":          {"agents:runs:read"},
	"runs.events":       {"agents:runs:read"},
	"runs.start":        {"agents:runs:write"},
	"runs.send_message": {"agents:runs:write"},
	"runs.interrupt":    {"agents:runs:write"},
	"runs.terminate":    {"agents:runs:write"},
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
