package credentials

import (
	"context"
	"net/http"
	"time"
)

// Credential is the minted or resolved secret plus how the egress gateway
// applies it on the outbound leg. The agent never sees this value.
type Credential struct {
	// Token is the raw minted or resolved secret (e.g. a GitHub installation
	// token). The gateway may reformat it per upstream protocol — for GitHub the
	// same token is injected as Basic auth for git smart-HTTP and as Bearer for
	// the REST API.
	Token string

	// Header and Value are the fully-formed header the gateway injects by default
	// (the flagship protocol for the connection — for GitHub, git over HTTPS,
	// i.e. Basic auth).
	Header string
	Value  string

	// ExpiresAt is when the credential stops being valid. Zero means
	// non-expiring (e.g. a static API key).
	ExpiresAt time.Time
}

// CredentialAdapter mints or resolves the credential for one connection, scoped
// to the calling identity. Implementations are selected by Connection.Adapter
// and must never return a credential broader than the connection's ceiling.
//
// An adapter also owns the translation from an inbound HTTP request to the
// policy action that authorizes it (ClassifyAction), since only the adapter
// knows the upstream's API shape — git smart-HTTP routes, REST verb mapping,
// the Kubernetes resource grammar, OAuth2's grant types. The egress gateway
// stays adapter-agnostic: it asks the adapter what policy scope this call
// requires, then evaluates that against the caller's grants.
//
// The adapter also enumerates the full set of actions it can ever return
// (Actions), so the gateway can publish the corresponding scope vocabulary
// (connection:<name>:<action>) to the policy service at startup. ClassifyAction
// must only return values from this set.
type CredentialAdapter interface {
	// Type reports which AdapterType this adapter serves.
	Type() AdapterType

	// Actions returns every action this adapter may classify a request as,
	// each paired with a human-readable description. The egress gateway uses
	// this to publish the connection:<name>:<action> scope vocabulary to the
	// policy service. Order is significant only for documentation; the gateway
	// does not depend on it.
	Actions() []ActionDeclaration

	// ClassifyAction inspects req and returns the policy action this call
	// requires (e.g. "repo:write", "objects:read"). The action is concatenated
	// with the connection name to produce the policy scope
	// "connection:<name>:<action>". An adapter that authorizes the entire
	// connection uniformly may return a single constant ("use", "invoke").
	// Returning ErrUnsupportedAction tells the gateway to deny the request.
	ClassifyAction(req *http.Request) (string, error)

	// Resolve returns the credential to apply for this call.
	Resolve(ctx context.Context, conn Connection, id CallerIdentity) (Credential, error)
}

// ActionDeclaration describes one action an adapter can classify. The Action
// is the value returned by ClassifyAction; Description is shown to admins in
// the connection dashboard and embedded in the published scope vocabulary.
type ActionDeclaration struct {
	Action      string
	Description string
}

// SecretResolver fetches raw secret material (e.g. a GitHub App private key) by
// the opaque reference stored on a Connection. The production implementation
// reads the per-tenant secret store; secret material is never exposed to the
// agent.
type SecretResolver interface {
	Resolve(ctx context.Context, ref string) ([]byte, error)
}
