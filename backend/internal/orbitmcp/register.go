package orbitmcp

import (
	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools wires every domain's tools onto the server. Each domain lives in
// its own tools_<domain>.go file and registers its tools here.
func registerTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	registerAgentTools(srv, b, jc)
	registerRunTools(srv, b, jc)
	registerModelTools(srv, b, jc)
	registerPolicyTools(srv, b, jc)
}
