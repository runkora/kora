package azure

import (
	"context"

	"github.com/runkora/kora/backend/internal/inferencegw"
)

// SystemOne refuses: azure serves no decision models, and a chat model is never
// bridged to one.
func (c *Connector) SystemOne(_ context.Context, _ inferencegw.SystemOneRequest) (*inferencegw.SystemOneResult, error) {
	return nil, inferencegw.NewAPINotServedError(c.name, "systemone")
}
