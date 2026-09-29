// Package credentials is the connection registry and credential-minting core of
// the Orbit credential broker. A Connection is a tenant-scoped registration of
// one external service; an Adapter opens a Connector that mints a short-lived,
// Credential the egress gateway injects on the outbound leg. The agent never
// holds the secret.
//
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
package credentials

import (
	"encoding/json"
	"errors"
	"time"
)

// AdapterType identifies the credential mechanism a connection uses. One
// adapter implementation serves every connection of that type, so adding a
// service that fits an existing adapter is a registration, not new code.
//
// Each constant maps to one subpackage under internal/credentials/adapters/.
// The stored value is unconstrained text so a new adapter needs no schema
// change; an unknown value is rejected at resolve time with
// ErrUnsupportedAdapter.
type AdapterType string

const (
	AdapterGitHubApp        AdapterType = "github_app"
	AdapterKubernetesBearer AdapterType = "kubernetes"

	// AdapterPassthrough injects no credential. It is not registrable: the
	// broker holds the passthrough adapter directly and uses it for hosts with
	// no connection, which the egress whitelist alone authorizes.
	AdapterPassthrough AdapterType = "passthrough"
)

// Connection is a tenant-scoped registration of one external service. It names
// the adapter, the upstream host the gateway terminates and re-originates, a
// reference to the per-tenant secret store (never a raw secret), and
// adapter-specific configuration.
//
// Connections come from the operator-aggregated registry the gateway mounts
// (configmap.go). The registry reader derives ID as "tenant|name" to keep the
// github_app adapter's token cache key stable. CreatedAt and UpdatedAt are zero
// there; they exist for API shape.
type Connection struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Name      string          `json:"name"` // agent-facing, e.g. "github"
	Adapter   AdapterType     `json:"adapter"`
	BaseHost  string          `json:"base_host"`  // e.g. "github.com"
	SecretRef string          `json:"secret_ref"` // reference, never a raw value
	Config    json.RawMessage `json:"config"`     // adapter-specific keys only

	// CACert is the PEM authority the gateway verifies this upstream against,
	// for an upstream not signed by a public root. It is a property of the
	// connection, not of any adapter: the gateway owns the outbound TLS leg,
	// and no adapter dials the upstream.
	CACert    string    `json:"ca_cert,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CallerIdentity is the verified identity of the agent whose run is making the
// outbound call, as the egress gateway establishes it from the platform token.
// PrincipalID is normalized to the agent principal (authorize-as-agent); RunID
// is carried for attribution (attribute-as-run). See the ADR's identity model.
type CallerIdentity struct {
	TenantID    string
	PrincipalID string // agent principal, e.g. spiffe://.../agent/<slug>
	RunID       string
	Action      string // requested action, e.g. "contents:write"
	// Qualifier is the resource the request was attributed to (e.g. the
	// repository name), empty when none. Adapters that support it mint the
	// credential narrowed to this resource.
	Qualifier string
	// AlsoAuthorized lists the classification's alternates that the caller's
	// grant also holds, and its AlsoRequired actions. Adapters widen the
	// credential by exactly these and no others.
	AlsoAuthorized []string
}

// Sentinel errors. Callers may test for these with errors.Is.
var (
	// ErrConnectionNotFound is returned when no connection matches the lookup.
	ErrConnectionNotFound = errors.New("connection not found")

	// ErrUnsupportedAdapter is returned when a connection names an adapter type
	// with no registered implementation.
	ErrUnsupportedAdapter = errors.New("unsupported credential adapter")

	// ErrInvalidConfig is returned when a connection's adapter-specific config is
	// absent or malformed.
	ErrInvalidConfig = errors.New("invalid connection config")

	// ErrUnsupportedAction is returned by Classify when the request shape
	// has no policy mapping for this adapter (e.g. an unsupported verb on a
	// REST adapter). The gateway treats this as a deny.
	ErrUnsupportedAction = errors.New("unsupported action for adapter")

	// ErrCredentialScopeRejected is returned by Mint when the
	// upstream refuses to issue a credential for the requested scope. Orbit's
	// policy allowed the call, but the underlying credential (a GitHub App
	// installation, say) does not itself hold that access. The broker maps this
	// to a client-facing forbidden, distinct from an internal mint failure.
	ErrCredentialScopeRejected = errors.New("upstream refused to issue a credential for the requested scope")
)
