package orbitmcp

import (
	"context"
	"errors"
	"net/url"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerEnvironmentTools wires the sandbox environment tools. The agents service enforces
// agents:environments:read and :write and recompiles the grants of attached agents.
func registerEnvironmentTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	base := b.Agents + "/api/v1/environments"

	addTool(srv, "agents.environments.list",
		"List the sandbox environments in the caller's tenant. Use the ids in environments for agents.create and agents.update.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base, nil))
		})

	addTool(srv, "agents.environments.get",
		"Get a single environment by id: its sandbox image, size, egress hosts and connectors.",
		func(ctx context.Context, a environmentIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.ID), nil))
		})

	addTool(srv, "agents.environments.create",
		"Create a sandbox environment: a sandbox image, a size, and the egress hosts and credential connections "+
			"of the agents it is attached to. Call agents.sandbox_images.list for image names and agents.connectors "+
			"for connection names and actions.",
		func(ctx context.Context, a environmentArgs) (*sdk.CallToolResult, error) {
			if a.Name == "" {
				return toolError(errors.New("name is required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), base, a))
		})

	addTool(srv, "agents.environments.replace",
		"Replace an environment in full. Every field you omit is cleared, and an omitted size dimension takes the "+
			"default. Fetch it first with agents.environments.get. Edits apply to every agent it is attached to.",
		func(ctx context.Context, a replaceEnvironmentArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Put(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.ID), a.environmentArgs))
		})

	addTool(srv, "agents.environments.delete",
		"Delete an environment by id. Fails while any agent type has it attached.",
		func(ctx context.Context, a environmentIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.ID)))
		})

	addTool(srv, "agents.environments.sizes",
		"Get the default and maximum environment sizes (CPU, memory and local disk). An environment cannot ask for more than the maximum.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), b.Agents+"/api/v1/environment-sizes", nil))
		})

	addTool(srv, "agents.sandbox_images.list",
		"List the sandbox images registered in the caller's tenant, with whether each is valid and why not. "+
			"An environment names one by name; kora-base is the kora base image.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), b.Agents+"/api/v1/sandbox-images", nil))
		})
}

// ── tool argument types ───────────────────────────────────────────────────────

type environmentIDArgs struct {
	ID string `json:"id" jsonschema:"the environment id"`
}

type environmentArgs struct {
	Name         string         `json:"name" jsonschema:"the environment name (unique within the tenant)"`
	Description  string         `json:"description,omitempty" jsonschema:"what this environment is for; agents read it when they choose one"`
	SandboxImage string         `json:"sandbox_image,omitempty" jsonschema:"name of a valid sandbox image from agents.sandbox_images.list; omit for the kora base image"`
	Size         *sizeArg       `json:"size,omitempty" jsonschema:"CPU, memory and local disk; omitted values take the install default"`
	EgressHosts  []string       `json:"egress_hosts,omitempty" jsonschema:"hosts agents in this environment may reach (e.g. api.github.com)"`
	Connectors   []connectorArg `json:"connectors,omitempty" jsonschema:"credential connections and the actions allowed; call agents.connectors to list"`
}

type sizeArg struct {
	CPU    string `json:"cpu,omitempty" jsonschema:"CPU limit as a Kubernetes quantity (e.g. 2 or 500m)"`
	Memory string `json:"memory,omitempty" jsonschema:"memory limit as a Kubernetes quantity (e.g. 4Gi)"`
	Disk   string `json:"disk,omitempty" jsonschema:"local disk for build caches and tool installs (e.g. 10Gi)"`
}

type replaceEnvironmentArgs struct {
	ID string `json:"id" jsonschema:"the environment id to replace"`
	environmentArgs
}
