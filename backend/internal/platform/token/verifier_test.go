package token

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

// ── Test Helpers ──────────────────────────────────────────────────────────────

// mockJWKSServer creates a test HTTP server that serves a JWKS endpoint with
// a single RSA key. Returns the server, the private key for signing test tokens,
// and the JWKS URL.
func mockJWKSServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey, string) {
	t.Helper()

	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	// Create JWKS response
	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"kid": "test-key-1",
				"alg": "RS256",
				"n":   base64URLEncode(privateKey.PublicKey.N.Bytes()),
				"e":   base64URLEncode(bigIntToBytes(int64(privateKey.PublicKey.E))),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))

	t.Cleanup(server.Close)

	return server, privateKey, server.URL
}

// signToken creates a signed JWT with the given claims and private key.
func signToken(t *testing.T, claims jwt.Claims, privateKey *rsa.PrivateKey) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key-1"

	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// Helper functions for JWKS encoding
func base64URLEncode(data []byte) string {
	return jwt.NewNumericDate(time.Now()).String() // placeholder - use proper base64 URL encoding
}

func bigIntToBytes(n int64) []byte {
	return []byte{byte(n >> 16), byte(n >> 8), byte(n)}
}

// ── NewVerifier ───────────────────────────────────────────────────────────────

func TestNewVerifier_CreatesVerifier(t *testing.T) {
	server, _, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	if verifier == nil {
		t.Fatal("expected non-nil verifier")
	}
	if verifier.inner == nil {
		t.Fatal("expected non-nil inner verifier")
	}
}

func TestNewVerifierFromConfig(t *testing.T) {
	server, _, jwksURL := mockJWKSServer(t)
	defer server.Close()

	cfg := Config{
		JWKSURL:  jwksURL,
		Issuer:   "orbit",
		Audience: "orbit-platform",
	}

	verifier := NewVerifierFromConfig(context.Background(), cfg)

	if verifier == nil {
		t.Fatal("expected non-nil verifier")
	}
	if verifier.inner == nil {
		t.Fatal("expected non-nil inner verifier")
	}
}

// ── Verify ────────────────────────────────────────────────────────────────────

func TestVerify_ValidToken_Success(t *testing.T) {
	server, privateKey, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	// Create valid platform claims
	now := time.Now()
	claims := &PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "orbit",
			Audience:  jwt.ClaimStrings{"orbit-platform"},
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
		Orbit: OrbitClaims{
			SessionID:      "session-abc",
			PrincipalType:  PrincipalTypeHuman,
			TenantID:       "tenant-1",
			IdentitySource: IdentitySourceFederated,
			DisplayName:    "Test User",
		},
	}

	token := signToken(t, claims, privateKey)

	// Verify token
	result, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil claims")
	}
	if result.Subject != "user-123" {
		t.Errorf("subject: got %q, want user-123", result.Subject)
	}
	if result.Orbit.SessionID != "session-abc" {
		t.Errorf("session_id: got %q, want session-abc", result.Orbit.SessionID)
	}
	if result.Orbit.TenantID != "tenant-1" {
		t.Errorf("tenant_id: got %q, want tenant-1", result.Orbit.TenantID)
	}
	if result.Orbit.PrincipalType != PrincipalTypeHuman {
		t.Errorf("principal_type: got %q, want HUMAN", result.Orbit.PrincipalType)
	}
}

func TestVerify_ExpiredToken_Fails(t *testing.T) {
	server, privateKey, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	// Create expired token
	now := time.Now()
	claims := &PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "orbit",
			Audience:  jwt.ClaimStrings{"orbit-platform"},
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Hour)), // expired 1 hour ago
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
		},
		Orbit: OrbitClaims{
			SessionID:     "session-abc",
			PrincipalType: PrincipalTypeHuman,
			TenantID:      "tenant-1",
		},
	}

	token := signToken(t, claims, privateKey)

	// Verify should fail
	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestVerify_WrongIssuer_Fails(t *testing.T) {
	server, privateKey, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	// Create token with wrong issuer
	now := time.Now()
	claims := &PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "evil-issuer", // wrong issuer
			Audience:  jwt.ClaimStrings{"orbit-platform"},
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Orbit: OrbitClaims{
			SessionID:     "session-abc",
			PrincipalType: PrincipalTypeHuman,
			TenantID:      "tenant-1",
		},
	}

	token := signToken(t, claims, privateKey)

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for wrong issuer, got nil")
	}
}

func TestVerify_WrongAudience_Fails(t *testing.T) {
	server, privateKey, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	// Create token with wrong audience
	now := time.Now()
	claims := &PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "orbit",
			Audience:  jwt.ClaimStrings{"wrong-audience"}, // wrong audience
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Orbit: OrbitClaims{
			SessionID:     "session-abc",
			PrincipalType: PrincipalTypeHuman,
			TenantID:      "tenant-1",
		},
	}

	token := signToken(t, claims, privateKey)

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for wrong audience, got nil")
	}
}

func TestVerify_MalformedToken_Fails(t *testing.T) {
	server, _, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	malformedTokens := []string{
		"",                          // empty
		"not.a.jwt",                 // malformed structure
		"invalid-base64!.data.here", // invalid base64
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.invalid", // invalid signature
	}

	for _, token := range malformedTokens {
		_, err := verifier.Verify(context.Background(), token)
		if err == nil {
			t.Errorf("expected error for malformed token %q, got nil", token)
		}
	}
}

func TestVerify_PATToken_ExtractsPATID(t *testing.T) {
	server, privateKey, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	// Create PAT token
	now := time.Now()
	claims := &PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "orbit",
			Audience:  jwt.ClaimStrings{"orbit-platform"},
			Subject:   "user-who-created-pat",
			ExpiresAt: jwt.NewNumericDate(now.Add(30 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Orbit: OrbitClaims{
			SessionID:      "session-xyz",
			PrincipalType:  PrincipalTypePAT,
			TenantID:       "tenant-1",
			IdentitySource: IdentitySourcePlatform,
			DisplayName:    "ci-token",
			PatID:          "pat-uuid-123",
		},
	}

	token := signToken(t, claims, privateKey)

	result, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if result.Orbit.PrincipalType != PrincipalTypePAT {
		t.Errorf("principal_type: got %q, want PAT", result.Orbit.PrincipalType)
	}
	if result.Orbit.PatID != "pat-uuid-123" {
		t.Errorf("pat_id: got %q, want pat-uuid-123", result.Orbit.PatID)
	}
	// Subject should still be the creating user for attribution
	if result.Subject != "user-who-created-pat" {
		t.Errorf("subject: got %q, want user-who-created-pat", result.Subject)
	}
}

func TestVerify_AgentToken(t *testing.T) {
	server, privateKey, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	now := time.Now()
	claims := &PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "orbit",
			Audience:  jwt.ClaimStrings{"orbit-platform"},
			Subject:   "agent-456",
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Orbit: OrbitClaims{
			SessionID:      "session-agent-run",
			PrincipalType:  PrincipalTypeAgent,
			TenantID:       "tenant-1",
			IdentitySource: IdentitySourceAttested,
			DisplayName:    "Claude Code Agent",
		},
	}

	token := signToken(t, claims, privateKey)

	result, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if result.Orbit.PrincipalType != PrincipalTypeAgent {
		t.Errorf("principal_type: got %q, want AGENT", result.Orbit.PrincipalType)
	}
	if result.Orbit.IdentitySource != IdentitySourceAttested {
		t.Errorf("identity_source: got %q, want ATTESTED", result.Orbit.IdentitySource)
	}
}

// ── VerifyAndExtract ──────────────────────────────────────────────────────────

func TestVerifyAndExtract_CustomClaims(t *testing.T) {
	server, privateKey, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	// Create token with custom claims structure (e.g., delegation token)
	type CustomClaims struct {
		jwt.RegisteredClaims
		DelegatedFrom string `json:"delegated_from"`
		Scope         string `json:"scope"`
	}

	now := time.Now()
	claims := &CustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "orbit",
			Audience:  jwt.ClaimStrings{"orbit-platform"},
			Subject:   "agent-789",
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		DelegatedFrom: "user-123",
		Scope:         "inference:invoke",
	}

	token := signToken(t, claims, privateKey)

	var result CustomClaims
	err := verifier.VerifyAndExtract(context.Background(), token, &result)
	if err != nil {
		t.Fatalf("VerifyAndExtract failed: %v", err)
	}

	if result.Subject != "agent-789" {
		t.Errorf("subject: got %q, want agent-789", result.Subject)
	}
	if result.DelegatedFrom != "user-123" {
		t.Errorf("delegated_from: got %q, want user-123", result.DelegatedFrom)
	}
	if result.Scope != "inference:invoke" {
		t.Errorf("scope: got %q, want inference:invoke", result.Scope)
	}
}

func TestVerifyAndExtract_InvalidToken_Fails(t *testing.T) {
	server, _, jwksURL := mockJWKSServer(t)
	defer server.Close()

	verifier := NewVerifier(context.Background(), jwksURL, "orbit", "orbit-platform")

	var claims PlatformClaims
	err := verifier.VerifyAndExtract(context.Background(), "invalid-token", &claims)
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
}

// ── Principal Type Tests ──────────────────────────────────────────────────────

func TestPrincipalTypes(t *testing.T) {
	tests := []struct {
		name string
		pt   PrincipalType
		want string
	}{
		{"human", PrincipalTypeHuman, "HUMAN"},
		{"agent", PrincipalTypeAgent, "AGENT"},
		{"service", PrincipalTypeService, "SERVICE"},
		{"system", PrincipalTypeSystem, "SYSTEM"},
		{"pat", PrincipalTypePAT, "PAT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.pt) != tt.want {
				t.Errorf("PrincipalType: got %q, want %q", tt.pt, tt.want)
			}
		})
	}
}

func TestIdentitySources(t *testing.T) {
	tests := []struct {
		name string
		is   IdentitySource
		want string
	}{
		{"federated", IdentitySourceFederated, "FEDERATED"},
		{"attested", IdentitySourceAttested, "ATTESTED"},
		{"platform", IdentitySourcePlatform, "PLATFORM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.is) != tt.want {
				t.Errorf("IdentitySource: got %q, want %q", tt.is, tt.want)
			}
		})
	}
}
