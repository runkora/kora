package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/runkora/kora/backend/internal/platform/connections"
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

// Adapter serves one credential mechanism. It is registered once per mechanism
// and opens a Connector for each connection of that type.
//
// The adapter owns the translation from an inbound request to the policy action
// that authorizes it, because only it knows the upstream's API shape: git
// smart-HTTP routes, REST verb mapping, the Kubernetes resource grammar. The
// gateway asks what scope a call requires and evaluates that against the
// caller's grants, staying adapter-agnostic throughout.
type Adapter interface {
	// Type reports which AdapterType this adapter serves.
	Type() AdapterType

	// Actions returns every action a Connector of this adapter may classify a
	// request as, each with a human-readable description. It is what the
	// gateway publishes as the connection:<name>:<action> vocabulary, and
	// Classify must return only values from this set. Order is presentational.
	Actions() []ActionDeclaration

	// Open binds the adapter to one connection, decoding and validating its
	// configuration. A Connector that exists is one whose configuration was
	// accepted, so neither Classify nor Mint revisits it. Configuration
	// failures wrap ErrInvalidConfig.
	//
	// A Connector belongs to the request that opened it and must not be cached
	// across requests. The registry hot-reloads a connection in place under a
	// key that does not change, so a cached Connector would keep serving the
	// configuration a CR edit had already replaced.
	Open(conn Connection) (Connector, error)
}

// Connector is one adapter bound to one connection, with its configuration
// already resolved. It holds that configuration, so nothing downstream of Open
// reads raw JSON.
type Connector interface {
	// Classify inspects req and returns the policy action this call requires
	// (e.g. "repo:write", "workloads:read") plus, for a connection with a
	// qualifier dimension, the resource the request names (e.g. the
	// repository). The broker builds the required scope
	// connection:<name>:<category>[:<qualifier>]:<verb> from the result. A
	// connection authorized uniformly returns a constant action ("use") and an
	// empty qualifier.
	//
	// Extraction is canonical: a request naming a resource outside the
	// connection's authority returns ErrUnsupportedAction rather than an
	// ambiguous qualifier. See
	// docs/adr/2026-07-22-connection-scope-qualifiers.md.
	Classify(req *http.Request) (Classification, error)

	// Mint returns the credential to apply for this call.
	Mint(ctx context.Context, id CallerIdentity) (Credential, error)
}

// RequestOwners is what a request names that can tie it to the upstream
// account it addresses. See docs/adr/2026-10-15-same-host-connection-selection.md.
type RequestOwners struct {
	// Owners are the normalized accounts the request names.
	Owners []string
	// NodeIDs are the global node ids a GraphQL document names.
	NodeIDs []string
	// RepoIDs are the numeric repository ids a REST path names in place of an owner.
	RepoIDs []string
	// Subject is the part of the request read for owners, such as "request path".
	Subject string
}

// DecodeConfig reads a connection's adapter configuration into out. It is the
// one place adapter config is decoded, so every adapter fails the same way. A
// connection with no config leaves out at its zero value, which the adapter
// then validates. Keys out does not declare are ignored, which is how the
// platform-owned settings share the same object.
func DecodeConfig(conn Connection, out any) error {
	if len(conn.Config) == 0 {
		return nil
	}
	if err := json.Unmarshal(conn.Config, out); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return nil
}

// Classification is the policy-facing result of mapping one inbound request:
// the action fragment ("<category>:<verb>") and the optional resource
// qualifier the request was attributed to (empty when the adapter has no
// qualifier dimension or the request names no resource).
type Classification struct {
	Action    string
	Qualifier string
	// Alternates are further actions that also authorize this request, for a
	// route the path alone cannot attribute to one category. The broker checks
	// Action and each alternate against the caller's grant, denies only when
	// none is held, and mints the credential for every one that is.
	Alternates []string
	// AlsoRequired are further actions the request needs as well as Action
	// (or an alternate), for a request that spans categories. The broker
	// denies unless the grant holds every one, and mints the credential for
	// all of them.
	AlsoRequired []string
}

// ActionDeclaration describes one action an adapter can classify. The Action
// is the value returned by Classify; Description is shown to admins in
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
