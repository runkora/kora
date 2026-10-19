// Package tokentest signs platform and delegation tokens for tests, with a
// verifier over the same key.
package tokentest

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// Signer signs tokens with one RSA key.
type Signer struct {
	key      *rsa.PrivateKey
	Verifier *token.Verifier
}

// NewSigner returns a Signer and a verifier that accepts its tokens.
func NewSigner(t *testing.T) *Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &Signer{key: key, Verifier: token.NewStaticVerifier(&key.PublicKey, token.DefaultIssuer, token.DefaultAudience)}
}

// Sign signs claims with the platform issuer and audience and a one-hour expiry.
func (s *Signer) Sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	claims["iss"] = token.DefaultIssuer
	claims["aud"] = token.DefaultAudience
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(s.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return raw
}

// Agent signs an AGENT platform token for the subject in the tenant.
func (s *Signer) Agent(t *testing.T, subject, tenantID string) string {
	t.Helper()
	return s.Sign(t, jwt.MapClaims{"sub": subject, "jti": "platform-" + subject, "orbit": map[string]any{"tenant_id": tenantID, "principal_type": "AGENT"}})
}

// Delegation signs a delegation token: person delegates to agent for one run.
func (s *Signer) Delegation(t *testing.T, jti, person, agent, runID string) string {
	t.Helper()
	return s.Sign(t, jwt.MapClaims{"sub": person, "act": map[string]string{"sub": agent}, "jti": jti, "run_id": runID, "delegator_principal_type": "HUMAN"})
}

// Blocklist reports the listed JTIs as revoked.
type Blocklist map[string]bool

// IsBlocked reports whether jti is revoked.
func (b Blocklist) IsBlocked(_ context.Context, jti string) (bool, error) {
	return b[jti], nil
}

// Request builds a request with the bearer and, when non-empty, the delegation.
func Request(method, target, bearer, delegation string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	r.Header.Set("Authorization", "Bearer "+bearer)
	if delegation != "" {
		r.Header.Set("X-Orbit-Delegation-Token", delegation)
	}
	return r
}
