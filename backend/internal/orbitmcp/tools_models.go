package orbitmcp

import (
	"context"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerModelTools wires the models domain: the inference catalog exposed by
// the inference gateway.
func registerModelTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	addTool(srv, "models.list",
		"List the inference models available through Orbit, with provider and capability metadata.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), b.Inference+"/v1/models", nil))
		})
}
