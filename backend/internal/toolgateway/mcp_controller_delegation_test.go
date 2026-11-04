package toolgateway

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/policy"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
)

// staticBlocklist reports the listed JTIs as revoked.
type staticBlocklist map[string]bool

func (b staticBlocklist) IsBlocked(_ context.Context, jti string) (bool, error) {
	return b[jti], nil
}

// testSigner signs tokens with one RSA key and serves it as a JWKS.
type testSigner struct {
	key *rsa.PrivateKey
	url string
}

func newTestSigner(t *testing.T) *testSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	jwks, _ := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)
	return &testSigner{key: key, url: srv.URL}
}

func (s *testSigner) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	claims["iss"] = platformtoken.DefaultIssuer
	claims["aud"] = platformtoken.DefaultAudience
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	raw, err := tok.SignedString(s.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return raw
}

// The MCP route verifies the delegation token and refuses a revoked one before
// any tool decision runs.
func TestMCPMiddleware_DelegationBlocklist(t *testing.T) {
	const agent = "spiffe://runkora.test/tenant/acme/agent/coder"
	signer := newTestSigner(t)
	verifier := platformtoken.NewVerifier(context.Background(), signer.url, platformtoken.DefaultIssuer, platformtoken.DefaultAudience)
	platform := signer.sign(t, jwt.MapClaims{"sub": agent, "kora": map[string]any{"tenant_id": "acme", "principal_type": "AGENT"}})
	delegation := signer.sign(t, jwt.MapClaims{"sub": "person-1", "act": map[string]string{"sub": agent}, "jti": "jti-1", "run_id": "run-1", "delegator_principal_type": "HUMAN"})

	cases := []struct {
		name           string
		blocked        staticBlocklist
		wantStatus     int
		wantDelegation bool
	}{
		{"active delegation reaches the handler", staticBlocklist{}, http.StatusOK, true},
		{"revoked delegation refused", staticBlocklist{"jti-1": true}, http.StatusUnauthorized, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewMCPController(verifier, &fakeEval{}, &memCatalog{}, &Registry{loaded: true}, nil, tc.blocked, "/v1", testLogger())
			var seen *policy.DelegationClaims
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = policy.DelegationFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/mcp/kora", nil)
			req.Header.Set("Authorization", "Bearer "+platform)
			req.Header.Set("X-Kora-Delegation-Token", delegation)
			rec := httptest.NewRecorder()
			c.Middleware()(next).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantDelegation != (seen != nil && seen.JTI == "jti-1") {
				t.Errorf("delegation in context = %+v, want present %v", seen, tc.wantDelegation)
			}
		})
	}
}
