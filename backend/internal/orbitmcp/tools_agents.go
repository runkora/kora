package orbitmcp

import (
	"context"
	"errors"
	"net/url"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerAgentTools wires the agents domain: the agent-type (agent definition)
// lifecycle. Reads forward the caller's token to orbit-agents; writes do the
// same and the agents service enforces the real platform scope (agents:types:*).
func registerAgentTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	base := b.Agents + "/api/v1/agent-types"

	addTool(srv, "agents.list",
		"List the agent types (agent definitions) registered in the caller's tenant.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base, nil))
		})

	addTool(srv, "agents.get",
		"Get a single agent type by id, including model, scopes, connectivity, and budget.",
		func(ctx context.Context, a getAgentArgs) (*sdk.CallToolResult, error) {
			if a.AgentTypeID == "" {
				return toolError(errors.New("agent_type_id is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.AgentTypeID), nil))
		})

	addEnumTool(srv, "agents.create",
		"Create an agent type (agent definition) in the caller's tenant. Returns the created agent type. "+
			"Supplying name, slug and model_id is enough — grant_scopes are auto-derived from the model.",
		map[string][]any{"framework": agentFrameworks, "identity_mode": agentIdentityModes},
		func(ctx context.Context, a createAgentArgs) (*sdk.CallToolResult, error) {
			if a.Name == "" || a.Slug == "" {
				return toolError(errors.New("name and slug are required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), base, a))
		})

	addEnumTool(srv, "agents.update",
		"Update an agent type by id. Only the fields you supply are changed; omitted fields are left untouched. "+
			"If you change model_id without setting grant_scopes, the inference grant_scopes are re-derived to match.",
		map[string][]any{"framework": agentFrameworks},
		func(ctx context.Context, a updateAgentArgs) (*sdk.CallToolResult, error) {
			if a.AgentTypeID == "" {
				return toolError(errors.New("agent_type_id is required")), nil
			}
			return jsonResult(jc.Patch(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.AgentTypeID), a))
		})

	addTool(srv, "agents.delete",
		"Delete (archive) an agent type by id. The agent type must be in the active state.",
		func(ctx context.Context, a getAgentArgs) (*sdk.CallToolResult, error) {
			if a.AgentTypeID == "" {
				return toolError(errors.New("agent_type_id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.AgentTypeID)))
		})

	addTool(srv, "agents.connectors",
		"List the credential connections available in the caller's tenant — each connection's name, adapter, and "+
			"the actions it supports. Use this to fill the connectors field of agents.create / agents.update.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), b.Agents+"/api/v1/connections", nil))
		})

	addTool(srv, "agents.mcp_tools",
		"List the governed MCP tools that can be granted to an agent — each tool's mcp:<server>:<tool>:use scope, "+
			"its server, and what it does, from the tool-gateway catalog. Use this to choose values for the "+
			"mcp_tools field of agents.create / agents.update.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), b.ToolGateway+"/v1/catalog", nil))
		})
}

// agentFrameworks is the closed set of agent CLI frameworks, surfaced as a
// JSON-Schema enum on the framework field. Mirrors the agents service source of
// truth (internal/agents.AgentFramework constants) — keep in sync if it changes.
var agentFrameworks = []any{"claude-code", "codex", "opencode"}

// agentIdentityModes is the closed set of agent identity models, surfaced as a
// JSON-Schema enum on the identity_mode field of agents.create. Mirrors
// internal/agents.IdentityMode — keep in sync if it changes. Deliberately not
// offered on agents.update: the identity model is immutable after creation
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 1).
var agentIdentityModes = []any{"autonomous", "delegated"}

// ── tool argument types (JSON schema is generated from these) ─────────────────

type getAgentArgs struct {
	AgentTypeID string `json:"agent_type_id" jsonschema:"the agent type id"`
}

// createAgentArgs is the input to agents.create. The tenant is derived from the
// caller's token by the agents service; any tenant in the body is ignored.
type createAgentArgs struct {
	Name                    string           `json:"name" jsonschema:"display name for the agent type"`
	Slug                    string           `json:"slug" jsonschema:"unique slug within the tenant (lowercase, dash-separated)"`
	Description             string           `json:"description,omitempty" jsonschema:"short human description of what the agent does"`
	SystemPrompt            string           `json:"system_prompt,omitempty" jsonschema:"the system prompt the agent runs with"`
	Framework               string           `json:"framework,omitempty" jsonschema:"agent CLI framework"`
	IdentityMode            string           `json:"identity_mode,omitempty" jsonschema:"identity model, immutable after creation (default autonomous): autonomous = the agent acts as itself on its own grant; delegated = the agent acts on behalf of the person who runs it, bounded by the intersection of its grant and that person's live access"`
	ModelID                 string           `json:"model_id" jsonschema:"inference model id — call the models.list tool first to get valid ids (e.g. anthropic/claude-sonnet-5)"`
	GrantScopes             []string         `json:"grant_scopes,omitempty" jsonschema:"platform grant scopes for the agent. Optional — the server derives the inference scopes from model_id automatically; set this only to override the inference grant or add other scopes"`
	Connectivity            *connectivityArg `json:"connectivity,omitempty" jsonschema:"outbound egress posture for the agent's sandboxes (default: none)"`
	Connectors              []connectorArg   `json:"connectors,omitempty" jsonschema:"credential connections the agent may use and the actions allowed — call the agents.connectors tool to list available connections"`
	MCPTools                []string         `json:"mcp_tools,omitempty" jsonschema:"governed MCP tools the agent may call through the gateway, as mcp:<server>:<tool>:use scopes — call the agents.mcp_tools tool to list what is available. The coarse mcp:invoke gate is derived automatically"`
	Environments            []environmentRef `json:"environments,omitempty" jsonschema:"sandbox environments to attach; mark one is_default. Omit for the built-in default environment. Call agents.environments.list for ids"`
	MaxConcurrentRunsPerPod int              `json:"max_concurrent_runs_per_pod,omitempty" jsonschema:"max concurrent runs per pod (0 = service default)"`
	Guardrails              *guardrailsArg   `json:"guardrails,omitempty" jsonschema:"loop-guard thresholds for this agent's runs; omitted fields take the platform defaults"`
}

// guardrailsArg is an agent type's loop-guard thresholds. Each warn is at least
// 1 and each pause is greater than its warn; there is no off switch.
type guardrailsArg struct {
	RepeatedCallWarn  *int `json:"repeated_call_warn,omitempty" jsonschema:"identical consecutive tool calls before kora warns the agent (default 4)"`
	RepeatedCallPause *int `json:"repeated_call_pause,omitempty" jsonschema:"identical consecutive tool calls before kora pauses the run; greater than repeated_call_warn (default 8)"`
	WaitRearmWarn     *int `json:"wait_rearm_warn,omitempty" jsonschema:"waits re-armed with no progress before kora warns the agent (default 6)"`
	WaitRearmPause    *int `json:"wait_rearm_pause,omitempty" jsonschema:"waits re-armed with no progress before kora pauses the run; greater than wait_rearm_warn (default 12)"`
}

// connectorArg is one credential connection the agent may use and the actions
// allowed on it. Call agents.connectors for valid connection names, actions,
// and the connection's resource-qualifier dimension (if any).
type connectorArg struct {
	Connection string   `json:"connection" jsonschema:"the registered connection name (e.g. github)"`
	Actions    []string `json:"actions" jsonschema:"adapter actions to allow on this connection (e.g. contents:read, issues:write; repo:read/repo:write grant everything)"`
	Resources  []string `json:"resources,omitempty" jsonschema:"optionally narrow the connector to specific upstream resources of the connection's qualifier dimension (e.g. bare repository names for a github connection). Omit for all resources — agents.connectors lists the dimension and any allowlist ceiling"`
}

// environmentRef attaches one environment to an agent.
type environmentRef struct {
	EnvironmentID string `json:"environment_id" jsonschema:"the environment id"`
	IsDefault     bool   `json:"is_default,omitempty" jsonschema:"sandboxes start in this environment unless the agent chooses another; a lone environment is the default"`
}

// connectivityArg is the agent's outbound egress posture.
type connectivityArg struct {
	Mode  string   `json:"mode" jsonschema:"egress posture: none (deny all egress), open (any host), or whitelist (only the listed hosts)"`
	Hosts []string `json:"hosts,omitempty" jsonschema:"hosts the agent may reach — required when mode is whitelist (e.g. api.github.com)"`
}

// updateAgentArgs is the input to agents.update. All mutable fields are optional
// pointers/slices: nil/omitted means "leave unchanged". agent_type_id selects the
// target and is ignored by the agents service when present in the body.
type updateAgentArgs struct {
	AgentTypeID             string            `json:"agent_type_id" jsonschema:"the agent type id to update"`
	Name                    *string           `json:"name,omitempty" jsonschema:"new display name"`
	Description             *string           `json:"description,omitempty" jsonschema:"new description"`
	SystemPrompt            *string           `json:"system_prompt,omitempty" jsonschema:"new system prompt"`
	Framework               *string           `json:"framework,omitempty" jsonschema:"new agent CLI framework"`
	ModelID                 *string           `json:"model_id,omitempty" jsonschema:"new inference model id — call the models.list tool first to get valid ids"`
	MaxConcurrentRunsPerPod *int              `json:"max_concurrent_runs_per_pod,omitempty" jsonschema:"new max concurrent runs per pod"`
	Guardrails              *guardrailsArg    `json:"guardrails,omitempty" jsonschema:"replacement loop-guard thresholds; omitted fields return to the platform defaults, an empty object restores all of them"`
	GrantScopes             []string          `json:"grant_scopes,omitempty" jsonschema:"replacement grant scopes. Optional — the server re-derives the inference scopes from model_id when you change the model; set this only to override"`
	Connectivity            *connectivityArg  `json:"connectivity,omitempty" jsonschema:"replacement egress posture for the agent's sandboxes"`
	Connectors              []connectorArg    `json:"connectors,omitempty" jsonschema:"replacement set of connectors — call the agents.connectors tool to list available connections"`
	MCPTools                []string          `json:"mcp_tools,omitempty" jsonschema:"replacement set of governed MCP tools the agent may call through the gateway, as mcp:<server>:<tool>:use scopes — call the agents.mcp_tools tool to list what is available. An empty array clears all MCP tool access; the coarse mcp:invoke gate is derived automatically"`
	Environments            *[]environmentRef `json:"environments,omitempty" jsonschema:"replacement set of attached environments; mark one is_default. An empty array leaves the built-in default. Call agents.environments.list for ids"`
}
