package identity

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// ─── Configuration ────────────────────────────────────────────────────────────

// Config is the non-secret, YAML-sourced configuration for the identity service.
// OIDC configuration is NOT stored here — it is sourced per-tenant from the
// tenant registry file (ORBIT_TENANTS_CONFIG).  See tenant_registry.go.
type Config struct {
	Token         TokenConfig         `yaml:"token"`
	OAuth         OAuthConfig         `yaml:"oauth"`
	OIDCDiscovery OIDCDiscoveryConfig `yaml:"oidcDiscovery"`
}

// OIDCDiscoveryConfig controls retry/backoff for OIDC provider discovery
// performed eagerly at startup (initTenantOIDCProviders). Mirrors
// identity-proxy's own startup-retry knobs (cmd/identity-proxy/config) —
// a transient DNS/network blip at boot (e.g. a dev-environment vcluster's
// CoreDNS still rolling out a custom split-horizon zone) must not
// permanently crash the whole service on the very first attempt.
type OIDCDiscoveryConfig struct {
	// ReadyTimeoutSecs bounds how long startup retries a single tenant's
	// discovery before giving up and failing the service.
	ReadyTimeoutSecs int `yaml:"readyTimeoutSecs"`

	// BackoffInitialMs is the first retry delay.
	BackoffInitialMs int `yaml:"backoffInitialMs"`

	// BackoffMaxSecs caps the exponentially-doubling retry delay.
	BackoffMaxSecs int `yaml:"backoffMaxSecs"`
}

// ReadyTimeout returns the maximum time to retry a single tenant's OIDC
// discovery at startup.
func (c OIDCDiscoveryConfig) ReadyTimeout() time.Duration {
	return time.Duration(c.ReadyTimeoutSecs) * time.Second
}

// BackoffInitial returns the starting retry interval.
func (c OIDCDiscoveryConfig) BackoffInitial() time.Duration {
	return time.Duration(c.BackoffInitialMs) * time.Millisecond
}

// BackoffMax returns the ceiling on retry intervals.
func (c OIDCDiscoveryConfig) BackoffMax() time.Duration {
	return time.Duration(c.BackoffMaxSecs) * time.Second
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

	// Audience is the JWT `aud` claim value. Defaults to "orbit-platform".
	// Override per environment to scope tokens to a specific cluster or deployment.
	Audience string `yaml:"audience"`
}

// LoadConfig loads the identity service configuration from
// internal/identity/config/base.yaml and applies environment variable overrides.
//
// All defaults live in base.yaml. Runtime overrides are applied via ORBIT_*
// environment variables — no environment-specific YAML files are used.
//
// OIDC configuration is resolved separately via loadTenantRegistry (called
// by NewService) — it is sourced per-tenant from ORBIT_TENANTS_CONFIG, not
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

	// Env var overrides — take precedence over base.yaml values.
	// Use these in local dev to extend token TTLs (e.g. ORBIT_ACCESS_TOKEN_TTL_MINUTES=60).
	overrideInt(&cfg.Token.AccessTokenTTLMinutes, "ORBIT_ACCESS_TOKEN_TTL_MINUTES")
	overrideInt(&cfg.Token.RefreshTokenTTLDays, "ORBIT_REFRESH_TOKEN_TTL_DAYS")
	overrideInt(&cfg.Token.PATDefaultTTLDays, "ORBIT_PAT_DEFAULT_TTL_DAYS")
	overrideInt(&cfg.Token.PATMaxTTLDays, "ORBIT_PAT_MAX_TTL_DAYS")
	if os.Getenv("ORBIT_MCP_REQUIRE_PRE_REGISTRATION") == "true" {
		cfg.OAuth.RequirePreRegistration = true
	}
	if v := os.Getenv("ORBIT_TOKEN_AUDIENCE"); v != "" {
		cfg.Token.Audience = v
	}
	overrideInt(&cfg.OIDCDiscovery.ReadyTimeoutSecs, "ORBIT_OIDC_DISCOVERY_READY_TIMEOUT_SECS")
	overrideInt(&cfg.OIDCDiscovery.BackoffInitialMs, "ORBIT_OIDC_DISCOVERY_BACKOFF_INITIAL_MS")
	overrideInt(&cfg.OIDCDiscovery.BackoffMaxSecs, "ORBIT_OIDC_DISCOVERY_BACKOFF_MAX_SECS")

	return cfg, nil
}

// overrideInt sets *dst to the positive integer value of the named environment
// variable, if the variable is set and parses successfully.
func overrideInt(dst *int, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			*dst = n
		}
	}
}
