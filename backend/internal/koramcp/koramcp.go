// Package koramcp is the kora MCP server: the first platform-global MCP server.
// It exposes kora's own management surface (agents, policy, models, …) as MCP
// tools so an agent can operate the control plane. This package holds the TOOLS
// ONLY — the protocol/transport/passthrough live in the reusable internal/
// platform/mcp kit, and the HTTP forwarding in internal/platform/clients/korajson.
//
// Identity model: passthrough. Each tool forwards the CALLER's token and any
// delegation token to the relevant kora API (via korajson + the kit's
// CallerToken and CallerDelegation); the downstream service enforces the real
// platform scope. This server holds no authority.
//
// Tools are organized one file per domain (tools_agents.go, tools_models.go,
// tools_policy.go, …) and wired together in register.go.
package koramcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runkora/kora/backend/internal/platform/clients/korajson"
	mcpkit "github.com/runkora/kora/backend/internal/platform/mcp"
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
func NewServer(b Backends, jc *korajson.Client, brand productNamer) *sdk.Server {
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
		panic(fmt.Sprintf("koramcp: build schema for tool %q: %v", name, err))
	}
	for field, vals := range enums {
		p, ok := schema.Properties[field]
		if !ok {
			panic(fmt.Sprintf("koramcp: tool %q has no field %q to constrain with an enum", name, field))
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
	// The tool declares no requires: its scope lets the caller call it, and the
	// platform behind it checks the caller's own permission.
	sdk.AddTool(srv, t,
		func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
			res, err := h(ctx, in)
			if errors.Is(err, korajson.ErrForbidden) {
				return noAccess(t.Name, err), nil, nil
			}
			return res, nil, err
		})
}

// accessNouns names what a tool reads or changes, matched by the longest tool
// name prefix. A platform 403 surfaces as "You do not have access to <noun>."
var accessNouns = map[string]string{
	"agents.":                  "agents",
	"agents.environments.":     "agent environments",
	"agents.sandbox_images.":   "agent environments",
	"agents.connectors":        "connections",
	"agents.mcp_tools":         "the MCP tool catalog",
	"models.":                  "models",
	"models.virtual.":          "the model catalog",
	"models.providers.":        "the model catalog",
	"models.pricing.":          "the model catalog",
	"policy.":                  "policy grants",
	"policy.roles.":            "policy roles",
	"policy.grants.set_budget": "budgets",
	"policy.routes.":           "policy routes",
	"policy.scopes":            "your effective scopes",
	"budgets.":                 "budgets",
	"triggers.":                "triggers",
	"runs.":                    "runs",
	"identity.":                "the user directory",
	"pats.":                    "API keys",
}

// noAccess is the tool result for a platform 403: a plain sentence naming the
// resource, never the upstream body. A business-rule reason is appended; a
// policy reason ("deny:...") adds nothing the caller can act on.
func noAccess(tool string, err error) *sdk.CallToolResult {
	noun, best := "this resource", 0
	for prefix, n := range accessNouns {
		if strings.HasPrefix(tool, prefix) && len(prefix) > best {
			noun, best = n, len(prefix)
		}
	}
	text := "You do not have access to " + noun + "."
	var forbidden *korajson.ForbiddenError
	if errors.As(err, &forbidden) && forbidden.Message != "" && !strings.HasPrefix(forbidden.Message, "deny:") {
		text += " The platform said: " + forbidden.Message
	}
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: text}},
	}
}

// caller returns the bearer and delegation token the kit lifted from the inbound
// request. Every downstream call carries both. A service that reads the
// delegation evaluates agent grant intersected with the delegator's access.
func caller(ctx context.Context) korajson.Caller {
	return korajson.Caller{Bearer: mcpkit.CallerToken(ctx), Delegation: mcpkit.CallerDelegation(ctx)}
}

// jsonResult turns a (rawJSON, err) into an MCP tool result: the JSON as text on
// success, or an MCP isError result on failure (so the model sees and can react).
// A platform 403 is returned as the Go error, so register can name the
// resource the caller lacks.
func jsonResult(body json.RawMessage, err error) (*sdk.CallToolResult, error) {
	if errors.Is(err, korajson.ErrForbidden) {
		return nil, err
	}
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
