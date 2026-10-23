package identity

import (
	"fmt"

	"github.com/runkora/kora/backend/internal/platform/config"
)

// ─── Configuration ────────────────────────────────────────────────────────────

// Config is the non-secret, YAML-sourced configuration for the identity service.
// OIDC configuration is NOT stored here — it is sourced per-tenant from the
// tenant registry file (Registry.TenantsPath).  See tenant_registry.go.
type Config struct {
	Token    TokenConfig    `yaml:"token"`
	OAuth    OAuthConfig    `yaml:"oauth"`
	Registry RegistryConfig `yaml:"registry"`
}

// RegistryConfig locates the operator-rendered files identity reads and
// watches. An empty path means no such file.
type RegistryConfig struct {
	// TenantsPath is the tenant registry (kora-tenants ConfigMap).
	TenantsPath string `yaml:"tenantsPath"`
	// ClientSecretsPath is the aggregated OIDC client secrets (kora-tenant-oidc-secrets).
	ClientSecretsPath string `yaml:"clientSecretsPath"`
	// OAuthClientsPath is the pre-registered OAuth clients (kora-tenant-oauth-clients).
	OAuthClientsPath string `yaml:"oauthClientsPath"`
}

// OAuthConfig controls the MCP OAuth authorization-server surface.
type OAuthConfig struct {
	// RequirePreRegistration, when true, disables open dynamic client registration
	// (RFC 7591): POST /oauth/register returns 403 and only pre-registered clients
	// work. Default false (zero value) — open DCR for public PKCE clients, the MCP
	// MVP posture. Set true for stricter deployments.
	RequirePreRegistration bool `yaml:"requirePreRegistration"`
}

// TokenConfig controls platform token lifetimes.
type TokenConfig struct {
	// AccessTokenTTLMinutes is the lifetime of a platform session token.
	AccessTokenTTLMinutes int `yaml:"accessTokenTtlMinutes"`

	// RefreshTokenTTLDays is the lifetime of a human refresh token.
	RefreshTokenTTLDays int `yaml:"refreshTokenTtlDays"`

	// PATDefaultTTLDays is the default lifetime of a Personal Access Token.
	PATDefaultTTLDays int `yaml:"patDefaultTtlDays"`

	// PATMaxTTLDays is the maximum lifetime a caller may request for a PAT.
	PATMaxTTLDays int `yaml:"patMaxTtlDays"`

	// Audience is the JWT `aud` claim value. Defaults to "kora-platform".
	// Override per environment to scope tokens to a specific cluster or deployment.
	Audience string `yaml:"audience"`

	// DelegationTokenTTLMinutes is the lifetime of a delegation token
	// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 3). Deliberately
	// short and never load-bearing for long runs: GET /identity/delegation/run/{run_id}
	// re-signs a fresh token from the live issuance record, so revocation bites
	// via the issuance tombstone and the JTI blocklist, not token expiry.
	DelegationTokenTTLMinutes int `yaml:"delegationTokenTtlMinutes"`
}

// LoadConfig loads the identity service configuration from
// internal/identity/config/base.yaml and the file the chart renders.
//
// OIDC configuration is resolved separately via loadTenantRegistry (called
// by NewService) — it is sourced per-tenant from the tenant registry file, not
// from service-level YAML.
//
// The RSA signing key is NOT loaded here; it is read by the service's
// initSigningKey method so that file-path env vars can be resolved relative to
// a known working directory at runtime.
func LoadConfig() (*Config, error) {
	cfg, err := config.Load[Config]("internal/identity", "base")
	if err != nil {
		return nil, fmt.Errorf("load identity config: %w", err)
	}
	return cfg, nil
}
