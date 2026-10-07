package token

import (
	"fmt"

	"github.com/clear-route/orbit/internal/platform/config"
)

const (
	// DefaultIssuer is the "iss" claim value stamped into every platform JWT.
	// The identity service signs with this value; all consumers verify against it.
	DefaultIssuer = "orbit"

	// DefaultAudience is the "aud" claim value stamped into every platform JWT.
	// The identity service signs with this value; all consumers verify against it.
	DefaultAudience = "orbit-platform"
)

// Config holds the parameters needed to construct a Verifier.
type Config struct {
	// JWKSURL is the full URL of the identity service JWKS endpoint.
	JWKSURL string `yaml:"jwksURL"`

	// Issuer is the expected "iss" claim value in platform tokens.
	Issuer string `yaml:"issuer"`

	// Audience is the expected "aud" claim value in platform tokens.
	Audience string `yaml:"audience"`
}

// LoadConfig loads internal/platform/token/config/base.yaml and the file the
// chart renders. It panics on a malformed file, which fails startup.
func LoadConfig() Config {
	cfg, err := config.LoadService[Config]("internal/platform/token", config.Environment())
	if err != nil {
		panic(fmt.Errorf("token: load config: %w", err))
	}
	return *cfg
}
