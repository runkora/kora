package koramcp

import (
	"context"
	"errors"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runkora/kora/backend/internal/platform/clients/korajson"
)

// registerGraderTools wires the read side of Graders: an agent type's Graders and their
// results. Both tools forward the caller's token to kora-agents, which applies the same
// visibility rules as REST, so an agent the caller cannot see is not found. Creating,
// changing and deleting Graders, and starting regrades, are not exposed.
func registerGraderTools(srv *sdk.Server, b Backends, jc *korajson.Client) {
	base := b.Agents + "/api/v1/agent-types"

	addTool(srv, "graders.list",
		"List the Graders of an agent type, with their results. Returns what GET /api/v1/agent-types/{id}/graders returns. "+
			"To list the runs a Grader passed or failed, call runs.list with grader and verdict.",
		func(ctx context.Context, a gradersListArgs) (*sdk.CallToolResult, error) {
			if a.AgentTypeID == "" {
				return toolError(errors.New("agent_type_id is required")), nil
			}
			return jsonResult(jc.Get(ctx, caller(ctx), base, "/{id}/graders", nil, a.AgentTypeID))
		})

	addTool(srv, "graders.get",
		"Get one Grader of an agent type by name. Returns what GET /api/v1/agent-types/{id}/graders/{name} returns.",
		func(ctx context.Context, a gradersGetArgs) (*sdk.CallToolResult, error) {
			if a.AgentTypeID == "" || a.Name == "" {
				return toolError(errors.New("agent_type_id and name are required")), nil
			}
			return jsonResult(jc.Get(ctx, caller(ctx), base, "/{id}/graders/{name}", nil, a.AgentTypeID, a.Name))
		})
}

type gradersListArgs struct {
	AgentTypeID string `json:"agent_type_id" jsonschema:"the agent type id — call the agents.list tool for ids"`
}

type gradersGetArgs struct {
	AgentTypeID string `json:"agent_type_id" jsonschema:"the agent type id — call the agents.list tool for ids"`
	Name        string `json:"name" jsonschema:"the Grader name — call graders.list for names"`
}
