// Package credentials is the connection registry and credential-minting core of
// the Orbit credential broker. A Connection is a tenant-scoped registration of
// one external service (GitHub, AWS, an OAuth2 app, an API-key SaaS); a
// CredentialAdapter turns a Connection into a short-lived, scoped Credential
// that the egress gateway injects on the outbound leg. The agent never holds the
// secret.
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
// The set is closed in code (each constant maps to one subpackage under
// internal/credentials/adapters/) but the database column is intentionally an
// unconstrained TEXT — adding a new adapter must not require a schema
// migration. Validation happens at resolve time via ErrUnsupportedAdapter.
type AdapterType string

const (
	AdapterGitHubApp         AdapterType = "github_app"
	AdapterKubernetesBearer  AdapterType = "k8s_bearer"
	AdapterOAuth2ClientCreds AdapterType = "oauth2_client_credentials"
	AdapterAPIKey            AdapterType = "api_key"
	AdapterAWSSTS            AdapterType = "aws_sts"
	// AdapterPassthrough injects NO credential — the connection is governed
	// (egress posture + connection grant + audit) and the gateway re-originates
	// to the upstream unchanged. The "agent reaches a public, unauthenticated
	// resource" case (Pattern D), alongside minted (A), static-secret (B), and
	// native-endpoint (C). See the egress-gateway ADR.
	AdapterPassthrough AdapterType = "passthrough"
)

// Connection is a tenant-scoped registration of one external service. It names
// the adapter, the upstream host the gateway terminates and re-originates, a
// reference to the per-tenant secret store (never a raw secret), the ceiling of
// actions any grant may reach, and adapter-specific configuration.
type Connection struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	Name         string          `json:"name"` // agent-facing, e.g. "github"
	Adapter      AdapterType     `json:"adapter"`
	BaseHost     string          `json:"base_host"`     // e.g. "github.com"
	SecretRef    string          `json:"secret_ref"`    // reference, never a raw value
	ScopeCeiling []string        `json:"scope_ceiling"` // max actions any grant may reach
	Config       json.RawMessage `json:"config"`        // adapter-specific
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// CallerIdentity is the verified identity of the agent whose run is making the
// outbound call, as the egress gateway establishes it from the platform token.
// PrincipalID is normalized to the agent principal (authorize-as-agent); RunID
// is carried for attribution (attribute-as-run). See the ADR's identity model.
type CallerIdentity struct {
	TenantID    string
	PrincipalID string // agent principal, e.g. spiffe://.../agent/<slug>
	RunID       string
	Action      string // requested action, e.g. "repo:write"
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

	// ErrUnsupportedAction is returned by ClassifyAction when the request shape
	// has no policy mapping for this adapter (e.g. an unsupported verb on a
	// REST adapter). The gateway treats this as a deny.
	ErrUnsupportedAction = errors.New("unsupported action for adapter")
)
