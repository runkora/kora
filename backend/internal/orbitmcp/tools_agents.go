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
		map[string][]any{"framework": agentFrameworks},
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

	addTool(srv, "agents.tool_scopes",
		"List the selectable tool scopes for an agent type — each scope, what it does, and the tools it grants. "+
			"Use this to choose values for the tool_scopes field of agents.create / agents.update.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/tool-scopes", nil))
		})
}

// agentFrameworks is the closed set of agent CLI frameworks, surfaced as a
// JSON-Schema enum on the framework field. Mirrors the agents service source of
// truth (internal/agents.AgentFramework constants) — keep in sync if it changes.
var agentFrameworks = []any{"claude-code", "codex", "opencode"}

// ── tool argument types (JSON schema is generated from these) ─────────────────

type getAgentArgs struct {
	AgentTypeID string `json:"agent_type_id" jsonschema:"the agent type id"`
}

// createAgentArgs is the input to agents.create. The tenant is derived from the
// caller's token by the agents service; any tenant in the body is ignored.
type createAgentArgs struct {
	Name                    string   `json:"name" jsonschema:"display name for the agent type"`
	Slug                    string   `json:"slug" jsonschema:"unique slug within the tenant (lowercase, dash-separated)"`
	Description             string   `json:"description,omitempty" jsonschema:"short human description of what the agent does"`
	SystemPrompt            string   `json:"system_prompt,omitempty" jsonschema:"the system prompt the agent runs with"`
	Framework               string   `json:"framework,omitempty" jsonschema:"agent CLI framework"`
	ModelID                 string   `json:"model_id,omitempty" jsonschema:"inference model id — call the models.list tool first to get valid ids (e.g. anthropic/claude-sonnet-4-6)"`
	ToolScopes              []string `json:"tool_scopes,omitempty" jsonschema:"tool scopes the agent may use — call the agents.tool_scopes tool to list the valid scopes and what each grants"`
	GrantScopes             []string `json:"grant_scopes,omitempty" jsonschema:"platform grant scopes for the agent. Optional — the server derives the inference scopes from model_id automatically; set this only to override the inference grant or add other scopes"`
	MaxConcurrentRunsPerPod int      `json:"max_concurrent_runs_per_pod,omitempty" jsonschema:"max concurrent runs per pod (0 = service default)"`
}

// updateAgentArgs is the input to agents.update. All mutable fields are optional
// pointers/slices: nil/omitted means "leave unchanged". agent_type_id selects the
// target and is ignored by the agents service when present in the body.
type updateAgentArgs struct {
	AgentTypeID             string   `json:"agent_type_id" jsonschema:"the agent type id to update"`
	Name                    *string  `json:"name,omitempty" jsonschema:"new display name"`
	Description             *string  `json:"description,omitempty" jsonschema:"new description"`
	SystemPrompt            *string  `json:"system_prompt,omitempty" jsonschema:"new system prompt"`
	Framework               *string  `json:"framework,omitempty" jsonschema:"new agent CLI framework"`
	ModelID                 *string  `json:"model_id,omitempty" jsonschema:"new inference model id — call the models.list tool first to get valid ids"`
	MaxConcurrentRunsPerPod *int     `json:"max_concurrent_runs_per_pod,omitempty" jsonschema:"new max concurrent runs per pod"`
	ToolScopes              []string `json:"tool_scopes,omitempty" jsonschema:"replacement set of tool scopes — call the agents.tool_scopes tool to list valid scopes"`
	GrantScopes             []string `json:"grant_scopes,omitempty" jsonschema:"replacement grant scopes. Optional — the server re-derives the inference scopes from model_id when you change the model; set this only to override"`
}
