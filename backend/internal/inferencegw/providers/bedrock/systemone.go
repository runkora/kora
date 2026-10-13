package bedrock

import (
	"context"

	"github.com/clear-route/orbit/internal/inferencegw"
)

// SystemOne refuses: bedrock serves no decision models, and a chat model is never
// bridged to one.
func (c *Connector) SystemOne(_ context.Context, _ inferencegw.SystemOneRequest) (*inferencegw.SystemOneResult, error) {
	return nil, inferencegw.NewAPINotServedError(c.providerName, "systemone")
}
