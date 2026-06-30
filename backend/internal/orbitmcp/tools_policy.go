package orbitmcp

import (
	"context"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPolicyTools wires the policy domain: the caller's authorization view.
// Grant/role write tools land in a later phase (escalation-sensitive surface).
func registerPolicyTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	addTool(srv, "policy.scopes",
		"List the calling principal's full effective scope set (grants + roles).",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), b.Policy+"/api/v1/policy/effective-scopes", nil))
		})
}
