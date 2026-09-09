// Package passthrough implements the no-credential egress adapter.
//
// It serves hosts the agent is allowed to reach but that need no credential: a
// public API, a docs site, a package registry. The gateway re-originates the
// request unchanged and injects nothing.
//
// A passthrough connection cannot be registered. The broker holds this adapter
// directly and uses it only when no connection serves the host, so reaching a
// public host is granted by the egress whitelist alone.
package passthrough

import (
	"context"
	"net/http"

	"github.com/clear-route/orbit/internal/credentials"
)

// actionUse is the single action a passthrough call maps to. There is no
// credential and no per-verb distinction to draw.
const actionUse = "use"

// Adapter is the no-credential passthrough adapter. It has no dependencies:
// there is no secret to resolve and nothing to mint.
type Adapter struct{}

// New constructs a passthrough Adapter.
func New() *Adapter { return &Adapter{} }

// Type reports which AdapterType this adapter serves.
func (a *Adapter) Type() credentials.AdapterType { return credentials.AdapterPassthrough }

// Actions returns the single "use" action. Passthrough publishes no scope
// vocabulary, because no connection can be registered against it.
func (a *Adapter) Actions() []credentials.ActionDeclaration {
	return []credentials.ActionDeclaration{
		{Action: actionUse, Description: "Reach the host with no credential injected"},
	}
}

// ClassifyAction maps every request to "use". A passthrough host is authorized
// uniformly, regardless of method or path, and declares no qualifier dimension.
func (a *Adapter) ClassifyAction(_ *http.Request, _ credentials.Connection) (credentials.Classification, error) {
	return credentials.Classification{Action: actionUse}, nil
}

// Resolve returns an empty credential. The gateway's forward path treats an
// empty Header as "inject nothing".
func (a *Adapter) Resolve(_ context.Context, _ credentials.Connection, _ credentials.CallerIdentity) (credentials.Credential, error) {
	return credentials.Credential{}, nil
}
