package identity

import (
	"fmt"
	"os"
	"strconv"

	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ─── Configuration ────────────────────────────────────────────────────────────

// Config is the non-secret, YAML-sourced configuration for the identity service.
// OIDC configuration is NOT stored here — it is sourced per-tenant from the
// tenant registry file (ORBIT_TENANTS_CONFIG).  See tenant_registry.go.
type Config struct {
	Token TokenConfig `yaml:"token"`
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
// internal/identity/config/{environment}.yaml and applies safe defaults.
//
// OIDC configuration is resolved separately via loadTenantRegistry (called
// by NewService) — it is sourced per-tenant from ORBIT_TENANTS_CONFIG, not
// from service-level YAML.
//
// The RSA signing key is NOT loaded here; it is read by the service's
// initSigningKey method so that file-path env vars can be resolved relative to
// a known working directory at runtime.
func LoadConfig() (*Config, error) {
	cfg, err := config.Load[Config]("internal/identity", config.Environment())
	if err != nil {
		return nil, fmt.Errorf("load identity config: %w", err)
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

	// Explicit env var overrides — take precedence over YAML defaults.
	// Used by local dev to extend token TTLs without APP_ENV-based YAML branching.
	if v := os.Getenv("ORBIT_ACCESS_TOKEN_TTL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Token.AccessTokenTTLMinutes = n
		}
	}
	if v := os.Getenv("ORBIT_REFRESH_TOKEN_TTL_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Token.RefreshTokenTTLDays = n
		}
	}
	if v := os.Getenv("ORBIT_TOKEN_AUDIENCE"); v != "" {
		cfg.Token.Audience = v
	}

	return cfg, nil
}
