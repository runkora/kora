package identity

import (
	"fmt"
	"os"

	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ─── Configuration ────────────────────────────────────────────────────────────

// Config is the non-secret, YAML-sourced configuration for the identity service.
// Secrets (client credentials, signing key) are loaded from environment variables
// by LoadConfig and stored in the unexported fields; they are never written to YAML.
type Config struct {
	OIDC  OIDCConfig  `yaml:"oidc"`
	Token TokenConfig `yaml:"token"`
}

// OIDCConfig holds OIDC provider settings.
//
// IssuerURL may be set in YAML or overridden via ORBIT_OIDC_ISSUER_URL.
// ClientID and ClientSecret must be supplied via environment variables
// (ORBIT_OIDC_CLIENT_ID, ORBIT_OIDC_CLIENT_SECRET) — never in YAML.
type OIDCConfig struct {
	// IssuerURL is the OIDC provider's issuer URL used for discovery.
	// Leave empty to disable federated authentication.
	IssuerURL string `yaml:"issuerUrl"`

	// Scopes is the list of OAuth2 scopes requested during authentication.
	Scopes []string `yaml:"scopes"`

	// ClientID and ClientSecret are populated by LoadConfig from environment
	// variables — the yaml:"-" tag ensures they are never read from or written
	// to YAML files.
	ClientID     string `yaml:"-"`
	ClientSecret string `yaml:"-"`
}

// TokenConfig controls platform token lifetimes.
type TokenConfig struct {
	// AccessTokenTTLMinutes is the lifetime of a platform session token.
	AccessTokenTTLMinutes int `yaml:"accessTokenTtlMinutes"`

	// RefreshTokenTTLDays is the lifetime of a human refresh token.
	RefreshTokenTTLDays int `yaml:"refreshTokenTtlDays"`

	// Audience is the JWT `aud` claim value. Defaults to "orbit-platform".
	// Override per environment to scope tokens to a specific cluster or deployment.
	Audience string `yaml:"audience"`
}

// LoadConfig loads the identity service configuration from
// internal/identity/config/{environment}.yaml and merges in any secret values
// from environment variables.
//
// Precedence for OIDC settings:
//  1. ORBIT_OIDC_ISSUER_URL  — overrides the YAML issuerUrl when non-empty
//  2. ORBIT_OIDC_CLIENT_ID   — required for confidential/public client flows
//  3. ORBIT_OIDC_CLIENT_SECRET — required for confidential clients (omit for public)
//
// The RSA signing key is NOT loaded here; it is read by the service's
// initSigningKey method so that file-path env vars can be resolved relative to
// a known working directory at runtime.
func LoadConfig() (*Config, error) {
	cfg, err := config.Load[Config]("internal/identity", config.Environment())
	if err != nil {
		return nil, fmt.Errorf("load identity config: %w", err)
	}

	// Allow the issuer URL to be overridden via environment variable without
	// editing YAML files — useful for local dev and CI where you want to point
	// at a real provider (e.g. Entra) without committing the URL to config.
	if envIssuer := os.Getenv("ORBIT_OIDC_ISSUER_URL"); envIssuer != "" {
		cfg.OIDC.IssuerURL = envIssuer
	}

	// Client credentials are secrets and must never appear in YAML.
	cfg.OIDC.ClientID = os.Getenv("ORBIT_OIDC_CLIENT_ID")
	cfg.OIDC.ClientSecret = os.Getenv("ORBIT_OIDC_CLIENT_SECRET")

	// Validate that ClientID is set if OIDC is configured.
	if cfg.OIDC.IssuerURL != "" && cfg.OIDC.ClientID == "" {
		return nil, fmt.Errorf("ORBIT_OIDC_CLIENT_ID must be set when OIDC issuer is configured")
	}

	// Apply safe defaults for token lifetimes when YAML omits them.
	if cfg.Token.AccessTokenTTLMinutes == 0 {
		cfg.Token.AccessTokenTTLMinutes = 60
	}
	if cfg.Token.RefreshTokenTTLDays == 0 {
		cfg.Token.RefreshTokenTTLDays = 30
	}
	if cfg.Token.Audience == "" {
		cfg.Token.Audience = token.DefaultAudience
	}

	return cfg, nil
}
