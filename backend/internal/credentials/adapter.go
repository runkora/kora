package credentials

import (
	"context"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/connections"
)

// Credential is the minted or resolved secret plus how the egress gateway
// applies it on the outbound leg. The agent never sees this value.
type Credential struct {
	// Token is the raw minted or resolved secret, such as a GitHub installation
	// token. It is carried so a caller can render it for a protocol the default
	// Header/Value pair does not cover.
	Token string

	// Header and Value are the fully-formed header the gateway injects. They
	// carry the connection's default protocol, which for GitHub is git over
	// HTTPS.
	Header string
	Value  string

	// ExpiresAt is when the credential stops being valid. Zero means
	// non-expiring (e.g. a static API key).
	ExpiresAt time.Time
}

// CredentialAdapter mints or resolves the credential for one connection, scoped
// to the calling identity. Connection.Adapter selects the implementation, which
// must never return a credential broader than the connection's ceiling.
//
// The adapter also translates an inbound HTTP request into the policy action
// that authorizes it. Only it knows the upstream's API shape: git smart-HTTP
// routes, REST verb mapping, the Kubernetes resource grammar. The gateway asks
// what scope the call requires and evaluates that against the caller's grants,
// staying adapter-agnostic throughout.
//
// Actions enumerates everything ClassifyAction can ever return, which is what
// the gateway publishes as the connection:<name>:<action> vocabulary.
// ClassifyAction must return only values from that set.
type CredentialAdapter interface {
	// Type reports which AdapterType this adapter serves.
	Type() AdapterType

	// Actions returns every action this adapter may classify a request as,
	// each with a human-readable description. Order is presentational; the
	// gateway does not depend on it.
	Actions() []ActionDeclaration

	// ClassifyAction inspects req and returns the policy action this call
	// requires (e.g. "repo:write", "workloads:read") plus, for adapters with a
	// declared qualifier dimension, the resource qualifier extracted from the
	// request (e.g. the repository name). The broker builds the required scope
	// connection:<name>:<category>[:<qualifier>]:<verb> from the result. An
	// adapter that authorizes the entire connection uniformly may return a
	// single constant action ("use", "invoke") and an empty qualifier.
	//
	// conn is the connection the request routed to. Adapters read its config to
	// verify resource authority; the GitHub adapter, for instance, denies paths
	// naming a foreign repository owner. Extraction must be canonical: a request
	// naming a resource outside the connection's authority returns
	// ErrUnsupportedAction rather than an ambiguous qualifier. See
	// docs/adr/2026-07-22-connection-scope-qualifiers.md.
	ClassifyAction(req *http.Request, conn Connection) (Classification, error)

	// Resolve returns the credential to apply for this call.
	Resolve(ctx context.Context, conn Connection, id CallerIdentity) (Credential, error)
}

// Classification is the policy-facing result of mapping one inbound request:
// the action fragment ("<category>:<verb>") and the optional resource
// qualifier the request was attributed to (empty when the adapter has no
// qualifier dimension or the request names no resource).
type Classification struct {
	Action    string
	Qualifier string
}

// ActionDeclaration describes one action an adapter can classify. The Action
// is the value returned by ClassifyAction; Description is shown to admins in
// the connection dashboard and embedded in the published scope vocabulary.
// Requires lists same-connection action fragments the action confers when
// granted (published as vocabulary requires edges).
type ActionDeclaration struct {
	Action      string
	Description string
	Requires    []string
}

// DeclaredActions converts the shared adapter metadata table into the
// declaration type this package's interface returns. Every adapter publishes
// its vocabulary through here so no adapter can drop a field on the way across.
func DeclaredActions(adapter string) []ActionDeclaration {
	metas := connections.Actions(adapter)
	out := make([]ActionDeclaration, len(metas))
	for i, m := range metas {
		out[i] = ActionDeclaration{Action: m.Action, Description: m.Description, Requires: m.Requires}
	}
	return out
}

// SecretResolver fetches raw secret material, such as a GitHub App private key,
// by the opaque reference stored on a Connection. The production implementation
// reads the per-tenant secret store. Secret material never reaches the agent.
type SecretResolver interface {
	Resolve(ctx context.Context, ref string) ([]byte, error)
}
