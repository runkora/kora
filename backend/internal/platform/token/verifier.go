package token

import (
	"context"
	"fmt"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

// Verifier verifies platform-issued JWTs using the identity service's public
// key set fetched from the JWKS endpoint.
//
// Keys are fetched lazily on the first Verify call and cached; go-oidc/v3
// handles automatic key rotation when a token presents an unknown key ID.
//
// No private key is distributed to services — the identity service is the
// sole holder of the RSA private key. All consumers verify via the public
// JWKS endpoint at:
//
//	<identity-base-url>/api/v1/identity/.well-known/jwks.json
type Verifier struct {
	inner *gooidc.IDTokenVerifier
}

// NewVerifier constructs a Verifier that fetches keys from jwksURL and
// enforces issuer and audience claims on every token it verifies.
//
// jwksURL   — full URL of the identity service JWKS endpoint, e.g.:
//
//	http://orbit-identity.orbit-system.svc.cluster.local/api/v1/identity/.well-known/jwks.json
//
// issuer    — must match the "iss" claim in platform tokens (default: "orbit")
// audience  — must match the "aud" claim in platform tokens (default: "orbit-platform")
func NewVerifier(ctx context.Context, jwksURL, issuer, audience string) *Verifier {
	keySet := gooidc.NewRemoteKeySet(ctx, jwksURL)
	inner := gooidc.NewVerifier(issuer, keySet, &gooidc.Config{
		ClientID: audience,
	})
	return &Verifier{inner: inner}
}

// Verify parses rawToken, verifies its signature against the remote JWKS,
// checks issuer, audience, and expiry, then returns the embedded PlatformClaims.
//
// This performs a lightweight second parse on the inference gateway side —
// the authoritative validation (signature, expiry, JTI blocklist) is done
// by Envoy (Layer 1) before requests reach application services. In local
// Tier-1 development (no cluster, no Envoy), this is the sole validation path.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*PlatformClaims, error) {
	idToken, err := v.inner.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify platform token: %w", err)
	}

	var claims PlatformClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("extract platform claims: %w", err)
	}

	return &claims, nil
}
