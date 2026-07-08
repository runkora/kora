package orbitmcp

import (
	"context"
	"errors"
	"net/url"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerEnvironmentTools wires the agent-environment domain: reusable
// connectivity/connector bundles that agent types attach by reference. Reads
// require agents:environments:read, writes agents:environments:write; the agents
// service enforces the real scope and propagates edits to referencing agents.
func registerEnvironmentTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	base := b.Agents + "/api/v1/environments"

	addTool(srv, "agents.environments.list",
		"List the reusable connectivity/connector environments in the caller's tenant. Use this to choose an "+
			"environment_id for agents.create / agents.update.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base, nil))
		})

	addTool(srv, "agents.environments.get",
		"Get a single environment by id — its egress hosts and connectors.",
		func(ctx context.Context, a environmentIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.ID), nil))
		})

	addTool(srv, "agents.environments.create",
		"Create a reusable environment: a named bundle of egress hosts and credential connections that agent "+
			"types can attach by reference. Call agents.connectors for valid connection names and actions.",
		func(ctx context.Context, a environmentArgs) (*sdk.CallToolResult, error) {
			if a.Name == "" {
				return toolError(errors.New("name is required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), base, a))
		})

	addTool(srv, "agents.environments.replace",
		"Replace an environment in full (the complete bundle — all fields you omit are cleared). Fetch it first "+
			"with agents.environments.get. Edits propagate to every agent type that references it.",
		func(ctx context.Context, a replaceEnvironmentArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			body := environmentArgs{
				Name: a.Name, Description: a.Description, EgressHosts: a.EgressHosts, Connectors: a.Connectors,
			}
			return jsonResult(jc.Put(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.ID), body))
		})

	addTool(srv, "agents.environments.delete",
		"Delete an environment by id. Fails if any agent type still references it.",
		func(ctx context.Context, a environmentIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.ID)))
		})
}

// ── tool argument types ───────────────────────────────────────────────────────

type environmentIDArgs struct {
	ID string `json:"id" jsonschema:"the environment id"`
}

type environmentArgs struct {
	Name        string         `json:"name" jsonschema:"the environment name (unique within the tenant)"`
	Description string         `json:"description,omitempty" jsonschema:"what this environment is for"`
	EgressHosts []string       `json:"egress_hosts,omitempty" jsonschema:"hosts agents in this environment may reach (e.g. api.github.com)"`
	Connectors  []connectorArg `json:"connectors,omitempty" jsonschema:"credential connections and the actions allowed — call agents.connectors to list"`
}

type replaceEnvironmentArgs struct {
	ID          string         `json:"id" jsonschema:"the environment id to replace"`
	Name        string         `json:"name" jsonschema:"the environment name"`
	Description string         `json:"description,omitempty" jsonschema:"what this environment is for"`
	EgressHosts []string       `json:"egress_hosts,omitempty" jsonschema:"hosts agents in this environment may reach"`
	Connectors  []connectorArg `json:"connectors,omitempty" jsonschema:"credential connections and the actions allowed"`
}
