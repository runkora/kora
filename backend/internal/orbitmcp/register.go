package orbitmcp

import (
	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools wires every domain's tools onto the server. Each domain lives in
// its own tools_<domain>.go file and registers its tools here. product is the
// configured product name for the descriptions that name it.
func registerTools(srv *sdk.Server, b Backends, jc *orbitjson.Client, product string) {
	registerAgentTools(srv, b, jc, product)
	registerEnvironmentTools(srv, b, jc, product)
	registerRunTools(srv, b, jc)
	registerModelTools(srv, b, jc, product)
	registerPolicyTools(srv, b, jc)
	registerBudgetTools(srv, b, jc)
	registerPATTools(srv, b, jc)
	registerIdentityTools(srv, b, jc)
	registerTriggerTools(srv, b, jc)
}
