// Package passthrough implements a no-credential egress adapter.
//
// A passthrough connection is still fully governed — the agent must hold the
// egress posture (egress:<host>:connect) AND a connection grant
// (connection:<name>:use), and every call is audited — but the gateway injects
// NO credential and simply re-originates the request to the upstream. It is the
// "agent reaches a public, unauthenticated resource" case (a curl to a public
// API that needs no auth): Pattern D, alongside the minted (A), static-secret
// (B), and native-endpoint (C) patterns in
// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
//
// Each connection-type lives in its own subpackage so adding a new external
// service mechanism is additive and never widens the surface of the parent
// package.
package passthrough

import (
	"context"
	"net/http"

	"github.com/clear-route/orbit/internal/credentials"
)

// actionUse is the single, uniform action a passthrough connection authorizes.
// There is no credential and no per-verb distinction to draw, so every request
// maps to connection:<name>:use.
const actionUse = "use"

// Adapter is the no-auth passthrough credential adapter. It has no
// dependencies — there is no secret to resolve and nothing to mint.
type Adapter struct{}

// New constructs a passthrough Adapter.
func New() *Adapter { return &Adapter{} }

// Type reports which AdapterType this adapter serves.
func (a *Adapter) Type() credentials.AdapterType { return credentials.AdapterPassthrough }

// Actions publishes the single "use" action to the connection:<name>:<action>
// scope vocabulary.
func (a *Adapter) Actions() []credentials.ActionDeclaration {
	return []credentials.ActionDeclaration{
		{Action: actionUse, Description: "Reach the connection's host with no credential injected"},
	}
}

// ClassifyAction maps every request to the uniform "use" action — a passthrough
// connection authorizes its whole host uniformly, regardless of method or path.
func (a *Adapter) ClassifyAction(_ *http.Request) (string, error) {
	return actionUse, nil
}

// Resolve returns an empty credential: nothing is injected on the outbound leg.
// The gateway's forward path treats an empty Header as "inject nothing" and
// re-originates the request to the upstream unchanged.
func (a *Adapter) Resolve(_ context.Context, _ credentials.Connection, _ credentials.CallerIdentity) (credentials.Credential, error) {
	return credentials.Credential{}, nil
}
