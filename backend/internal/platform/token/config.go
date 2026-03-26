package token

import "os"

const (
	// defaultJWKSURL is the in-cluster JWKS endpoint of the identity service.
	// Operators override this via ORBIT_TOKEN_JWKS_URL (e.g. for local dev).
	defaultJWKSURL = "http://orbit-identity.orbit-system.svc.cluster.local/api/v1/identity/.well-known/jwks.json"

	// DefaultIssuer is the "iss" claim value stamped into every platform JWT.
	// The identity service signs with this value; all consumers verify against it.
	DefaultIssuer = "orbit"

	// DefaultAudience is the "aud" claim value stamped into every platform JWT.
	// The identity service signs with this value; all consumers verify against it.
	DefaultAudience = "orbit-platform"
)

// Config holds the parameters needed to construct a Verifier.
// All fields have production-ready defaults and can be overridden via
// environment variables, making this the single place any service needs to
// touch to configure JWT verification.
type Config struct {
	// JWKSURL is the full URL of the identity service JWKS endpoint.
	// Default: in-cluster URL. Override: ORBIT_TOKEN_JWKS_URL.
	JWKSURL string

	// Issuer is the expected "iss" claim value in platform tokens.
	// Default: "orbit". Override: ORBIT_TOKEN_ISSUER.
	Issuer string

	// Audience is the expected "aud" claim value in platform tokens.
	// Default: "orbit-platform". Override: ORBIT_TOKEN_AUDIENCE.
	Audience string
}

// LoadConfig returns a Config populated from environment variables, falling
// back to production-safe defaults. It never fails — missing env vars simply
// use the defaults.
func LoadConfig() Config {
	return Config{
		JWKSURL:  envOr("ORBIT_TOKEN_JWKS_URL", defaultJWKSURL),
		Issuer:   envOr("ORBIT_TOKEN_ISSUER", DefaultIssuer),
		Audience: envOr("ORBIT_TOKEN_AUDIENCE", DefaultAudience),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
