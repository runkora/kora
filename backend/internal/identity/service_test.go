package identity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ── hashToken ─────────────────────────────────────────────────────────────────

func TestHashToken_Deterministic(t *testing.T) {
	h1 := hashToken("some-token")
	h2 := hashToken("some-token")
	if h1 != h2 {
		t.Errorf("hashToken is not deterministic: %q != %q", h1, h2)
	}
}

func TestHashToken_UniquePerInput(t *testing.T) {
	if hashToken("token-a") == hashToken("token-b") {
		t.Error("different inputs produced the same hash")
	}
}

func TestHashToken_HexLowercase(t *testing.T) {
	h := hashToken("anything")
	if h != strings.ToLower(h) {
		t.Errorf("hash is not lowercase hex: %q", h)
	}
	// SHA-256 encodes to 64 hex characters.
	if len(h) != 64 {
		t.Errorf("expected 64-character hex hash, got %d: %q", len(h), h)
	}
}

// ── generateOpaqueToken ───────────────────────────────────────────────────────

func TestGenerateOpaqueToken_Unique(t *testing.T) {
	a, err := generateOpaqueToken()
	if err != nil {
		t.Fatalf("generateOpaqueToken: %v", err)
	}
	b, err := generateOpaqueToken()
	if err != nil {
		t.Fatalf("generateOpaqueToken: %v", err)
	}
	if a == b {
		t.Error("two generated tokens are identical")
	}
}

func TestGenerateOpaqueToken_NotEmpty(t *testing.T) {
	tok, err := generateOpaqueToken()
	if err != nil {
		t.Fatalf("generateOpaqueToken: %v", err)
	}
	if tok == "" {
		t.Error("generated token is empty")
	}
}

// ── JWKSJSON ──────────────────────────────────────────────────────────────────

func TestJWKSJSON_NonEmpty(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	raw := svc.JWKSJSON()
	if len(raw) == 0 {
		t.Fatal("JWKSJSON returned empty bytes")
	}
}

func TestJWKSJSON_ContainsKty(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	raw := svc.JWKSJSON()
	if !strings.Contains(string(raw), `"kty"`) {
		t.Errorf("JWKS JSON does not contain kty field: %s", raw)
	}
}

func TestJWKSJSON_ContainsRSA(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	raw := svc.JWKSJSON()
	if !strings.Contains(string(raw), `"RSA"`) {
		t.Errorf("JWKS JSON does not contain RSA key type: %s", raw)
	}
}

// ── ExchangeSVID ──────────────────────────────────────────────────────────────

// xfccRequest builds an *http.Request carrying a minimal valid XFCC header
// for the given SPIFFE URI, simulating what Envoy injects after mTLS
// termination. Tenant ID is NOT part of the XFCC header — it is passed
// separately to ExchangeSVID.
func xfccRequest(spiffeID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/exchange", nil)
	r.Header.Set("x-forwarded-client-cert", "URI="+spiffeID)
	return r
}

func TestExchangeSVID_Success(t *testing.T) {
	svc, sessions, _, _, _ := newFullTestService(t)

	r := xfccRequest("spiffe://orbit.test/tenant/platform-team/agent/agent-1")
	resp, err := svc.ExchangeSVID(context.Background(), r)
	if err != nil {
		t.Fatalf("ExchangeSVID: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.RefreshToken != "" {
		t.Error("agent sessions must not include a refresh token")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token type: got %q, want %q", resp.TokenType, "Bearer")
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expires_in should be positive, got %d", resp.ExpiresIn)
	}
	if resp.IssuedAt <= 0 {
		t.Errorf("issued_at should be a positive Unix timestamp, got %d", resp.IssuedAt)
	}

	// Session must be persisted.
	sessions.mu.Lock()
	count := len(sessions.sessions)
	sessions.mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 persisted session, got %d", count)
	}
}

func TestExchangeSVID_TokenParses(t *testing.T) {
	const spiffeID = "spiffe://orbit.test/tenant/platform-team/agent/agent-99"
	svc, _, _, _, _ := newFullTestService(t)

	resp, err := svc.ExchangeSVID(context.Background(), xfccRequest(spiffeID))
	if err != nil {
		t.Fatalf("ExchangeSVID: %v", err)
	}
	claims, err := svc.parseToken(resp.AccessToken)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	// JWT subject is the full SPIFFE URI — the stable principal identity.
	if claims.Subject != spiffeID {
		t.Errorf("sub: got %q, want %q", claims.Subject, spiffeID)
	}
	if claims.Orbit.TenantID != "platform-team" {
		t.Errorf("tenant_id: got %q, want %q", claims.Orbit.TenantID, "platform-team")
	}
	if claims.Orbit.PrincipalType != PrincipalTypeAgent {
		t.Errorf("principal_type: got %q, want %q", claims.Orbit.PrincipalType, PrincipalTypeAgent)
	}
	if claims.Orbit.IdentitySource != IdentitySourceAttested {
		t.Errorf("identity_source: got %q, want %q", claims.Orbit.IdentitySource, IdentitySourceAttested)
	}
}

func TestExchangeSVID_TenantFromSPIFFEPath(t *testing.T) {
	// Tenant ID is derived from the SPIFFE URI path — no caller override is allowed.
	svc, _, _, _, _ := newFullTestService(t)

	r := xfccRequest("spiffe://orbit.test/tenant/acme-corp/agent/deploy-bot")
	resp, err := svc.ExchangeSVID(context.Background(), r)
	if err != nil {
		t.Fatalf("ExchangeSVID: %v", err)
	}
	claims, err := svc.parseToken(resp.AccessToken)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if claims.Orbit.TenantID != "acme-corp" {
		t.Errorf("tenant_id: got %q, want %q", claims.Orbit.TenantID, "acme-corp")
	}
}

func TestExchangeSVID_MissingXFCC(t *testing.T) {
	// A request without x-forwarded-client-cert must be rejected — it means
	// the request did not arrive via the Envoy mTLS sidecar.
	svc, _, _, _, _ := newFullTestService(t)

	r := httptest.NewRequest(http.MethodPost, "/exchange", nil) // no XFCC header
	_, err := svc.ExchangeSVID(context.Background(), r)
	if err == nil {
		t.Fatal("expected error when XFCC header is absent")
	}
}

func TestExchangeSVID_SessionPersistError(t *testing.T) {
	sessions := newMockSessionRepo()
	sessions.createErr = errors.New("db unavailable")
	svc := newTestService(t, sessions, newMockRefreshTokenRepo(), newMockBlocklistRepo(), newMockPATRepo())

	r := xfccRequest("spiffe://orbit.test/tenant/platform-team/agent/agent-1")
	_, err := svc.ExchangeSVID(context.Background(), r)
	if err == nil {
		t.Fatal("expected error when session persistence fails")
	}
}

// ── RefreshSession ────────────────────────────────────────────────────────────

func TestRefreshSession_Success(t *testing.T) {
	svc, sessions, refreshTokens, _, _ := newFullTestService(t)

	rawRefresh := "original-refresh-token"
	seedSession(sessions, "session-1", "jti-1")
	seedRefreshToken(refreshTokens, "rt-1", "session-1", rawRefresh)

	resp, err := svc.RefreshSession(context.Background(), SessionRequest{
		TenantID:     "tenant-1",
		GrantType:    "refresh_token",
		RefreshToken: rawRefresh,
	})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh token (rotation)")
	}
	if resp.RefreshToken == rawRefresh {
		t.Error("rotated refresh token must differ from the original")
	}
}

func TestRefreshSession_OldTokenRevoked(t *testing.T) {
	// After a successful refresh, the original refresh token must be revoked.
	svc, sessions, refreshTokens, _, _ := newFullTestService(t)

	rawRefresh := "original-token"
	seedSession(sessions, "session-1", "jti-1")
	rt := seedRefreshToken(refreshTokens, "rt-1", "session-1", rawRefresh)

	_, err := svc.RefreshSession(context.Background(), SessionRequest{
		TenantID:     "tenant-1",
		GrantType:    "refresh_token",
		RefreshToken: rawRefresh,
	})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	refreshTokens.mu.Lock()
	revoked := rt.RevokedAt
	refreshTokens.mu.Unlock()

	if revoked == nil {
		t.Error("original refresh token was not revoked after rotation")
	}
}

func TestRefreshSession_TokenNotFound(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	_, err := svc.RefreshSession(context.Background(), SessionRequest{
		TenantID:     "tenant-1",
		GrantType:    "refresh_token",
		RefreshToken: "nonexistent-token",
	})
	if !errors.Is(err, ErrRefreshTokenNotFound) {
		t.Errorf("expected ErrRefreshTokenNotFound, got %v", err)
	}
}

func TestRefreshSession_TokenRevoked(t *testing.T) {
	svc, sessions, refreshTokens, _, _ := newFullTestService(t)

	rawRefresh := "revoked-token"
	seedSession(sessions, "session-1", "jti-1")
	rt := seedRefreshToken(refreshTokens, "rt-1", "session-1", rawRefresh)

	revokedAt := time.Now().Add(-time.Minute)
	refreshTokens.mu.Lock()
	rt.RevokedAt = &revokedAt
	refreshTokens.mu.Unlock()

	_, err := svc.RefreshSession(context.Background(), SessionRequest{
		TenantID:     "tenant-1",
		GrantType:    "refresh_token",
		RefreshToken: rawRefresh,
	})
	if !errors.Is(err, ErrRefreshTokenRevoked) {
		t.Errorf("expected ErrRefreshTokenRevoked, got %v", err)
	}
}

func TestRefreshSession_TokenExpired(t *testing.T) {
	svc, sessions, refreshTokens, _, _ := newFullTestService(t)

	rawRefresh := "expired-token"
	seedSession(sessions, "session-1", "jti-1")
	rt := seedRefreshToken(refreshTokens, "rt-1", "session-1", rawRefresh)

	refreshTokens.mu.Lock()
	rt.ExpiresAt = time.Now().Add(-time.Hour)
	refreshTokens.mu.Unlock()

	_, err := svc.RefreshSession(context.Background(), SessionRequest{
		TenantID:     "tenant-1",
		GrantType:    "refresh_token",
		RefreshToken: rawRefresh,
	})
	if !errors.Is(err, ErrRefreshTokenExpired) {
		t.Errorf("expected ErrRefreshTokenExpired, got %v", err)
	}
}

func TestRefreshSession_SessionRevoked(t *testing.T) {
	svc, sessions, refreshTokens, _, _ := newFullTestService(t)

	rawRefresh := "valid-token"
	s := seedSession(sessions, "session-1", "jti-1")
	seedRefreshToken(refreshTokens, "rt-1", "session-1", rawRefresh)

	revokedAt := time.Now().Add(-time.Minute)
	sessions.mu.Lock()
	s.RevokedAt = &revokedAt
	sessions.mu.Unlock()

	_, err := svc.RefreshSession(context.Background(), SessionRequest{
		TenantID:     "tenant-1",
		GrantType:    "refresh_token",
		RefreshToken: rawRefresh,
	})
	if !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("expected ErrTokenRevoked, got %v", err)
	}
}

func TestRefreshSession_DBErrorNotMasked(t *testing.T) {
	// A real DB error from GetRefreshTokenByHash must not be masked as
	// ErrRefreshTokenNotFound.
	sessions := newMockSessionRepo()
	refreshTokens := newMockRefreshTokenRepo()
	refreshTokens.getByHashErr = errors.New("connection reset by peer")
	svc := newTestService(t, sessions, refreshTokens, newMockBlocklistRepo(), newMockPATRepo())

	_, err := svc.RefreshSession(context.Background(), SessionRequest{
		TenantID:     "tenant-1",
		GrantType:    "refresh_token",
		RefreshToken: "any-token",
	})
	if err == nil {
		t.Fatal("expected error from DB failure")
	}
	if errors.Is(err, ErrRefreshTokenNotFound) {
		t.Error("DB error must not be masked as ErrRefreshTokenNotFound")
	}
}

// ── RevokeToken — access_token path ──────────────────────────────────────────

func TestRevokeToken_AccessToken_Success(t *testing.T) {
	svc, sessions, _, blocklist, _ := newFullTestService(t)

	// Build a valid signed token using the service's own key.
	principal := &Principal{
		ID:             "user-1",
		Type:           PrincipalTypeHuman,
		TenantID:       "tenant-1",
		DisplayName:    "Test User",
		IdentitySource: IdentitySourceFederated,
	}
	jti := "test-jti-1"
	sessionID := "test-session-1"
	now := time.Now()
	ttl := time.Hour
	token, err := svc.signToken(principal, jti, sessionID, "", now, ttl)
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	// Pre-seed the session so revocation can look it up.
	seedSession(sessions, sessionID, jti)

	err = svc.RevokeToken(context.Background(), RevokeRequest{
		Token:     token,
		TokenType: "access_token",
	})
	if err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}

	// JTI must be blocked.
	blocked, err := blocklist.IsBlocked(context.Background(), jti)
	if err != nil {
		t.Fatalf("IsBlocked: %v", err)
	}
	if !blocked {
		t.Error("JTI was not added to the blocklist after revocation")
	}
}

func TestRevokeToken_AccessToken_InvalidJWT(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	err := svc.RevokeToken(context.Background(), RevokeRequest{
		Token:     "not.a.jwt",
		TokenType: "access_token",
	})
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

// TestRevokeToken_AccessToken_AlreadyBlocked verifies that revoking an access
// token whose JTI is already in the blocklist is idempotent — BlockJTI on the
// mock simply overwrites the key, so the service returns nil (RFC 7009 §2.1).
func TestRevokeToken_AccessToken_AlreadyBlocked(t *testing.T) {
	svc, sessions, _, blocklist, _ := newFullTestService(t)

	principal := &Principal{
		ID:             "user-1",
		Type:           PrincipalTypeHuman,
		TenantID:       "tenant-1",
		IdentitySource: IdentitySourceFederated,
	}
	jti := "blocked-jti"
	token, err := svc.signToken(principal, jti, "sess-1", "", time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	// Pre-seed the blocklist to simulate prior revocation.
	blocklist.mu.Lock()
	blocklist.blocked[jti] = true
	blocklist.mu.Unlock()
	// Session must exist so GetSessionByJTI returns it successfully.
	seedSession(sessions, "sess-1", jti)

	err = svc.RevokeToken(context.Background(), RevokeRequest{
		Token:     token,
		TokenType: "access_token",
	})
	// BlockJTI is idempotent — no error expected.
	if err != nil {
		t.Errorf("expected nil for idempotent revocation, got %v", err)
	}
}

func TestRevokeToken_AccessToken_SessionNotFoundIsOK(t *testing.T) {
	// revokeAccessToken tolerates ErrSessionNotFound from GetSessionByJTI —
	// the token may have been issued for a session that no longer exists.
	svc, _, _, _, _ := newFullTestService(t)

	principal := &Principal{
		ID:             "user-1",
		Type:           PrincipalTypeHuman,
		TenantID:       "tenant-1",
		IdentitySource: IdentitySourceFederated,
	}
	token, err := svc.signToken(principal, "jti-no-session", "sess-missing", "", time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	// No session seeded — GetSessionByJTI will return ErrSessionNotFound.
	err = svc.RevokeToken(context.Background(), RevokeRequest{
		Token:     token,
		TokenType: "access_token",
	})
	if err != nil {
		t.Errorf("expected nil error when session is not found, got %v", err)
	}
}

// ── RevokeToken — refresh_token path ─────────────────────────────────────────

func TestRevokeToken_RefreshToken_Success(t *testing.T) {
	svc, _, refreshTokens, _, _ := newFullTestService(t)

	rawRefresh := "opaque-refresh-1"
	rt := seedRefreshToken(refreshTokens, "rt-1", "session-1", rawRefresh)

	err := svc.RevokeToken(context.Background(), RevokeRequest{
		Token:     rawRefresh,
		TokenType: "refresh_token",
	})
	if err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}

	refreshTokens.mu.Lock()
	revoked := rt.RevokedAt
	refreshTokens.mu.Unlock()

	if revoked == nil {
		t.Error("refresh token was not marked as revoked")
	}
}

func TestRevokeToken_RefreshToken_NotFound(t *testing.T) {
	// RFC 7009 §2.1: the server must respond with 200/204 even when the token
	// never existed or was already revoked. Returning ErrRefreshTokenNotFound
	// here would be a protocol violation.
	svc, _, _, _, _ := newFullTestService(t)
	err := svc.RevokeToken(context.Background(), RevokeRequest{
		Token:     "nonexistent-refresh-token",
		TokenType: "refresh_token",
	})
	if err != nil {
		t.Errorf("expected nil (idempotent revocation), got %v", err)
	}
}

func TestRevokeToken_RefreshToken_DBErrorNotMasked(t *testing.T) {
	sessions := newMockSessionRepo()
	refreshTokens := newMockRefreshTokenRepo()
	refreshTokens.getByHashErr = errors.New("db timeout")
	svc := newTestService(t, sessions, refreshTokens, newMockBlocklistRepo(), newMockPATRepo())

	err := svc.RevokeToken(context.Background(), RevokeRequest{
		Token:     "any-token",
		TokenType: "refresh_token",
	})
	if err == nil {
		t.Fatal("expected error from DB failure")
	}
	if errors.Is(err, ErrRefreshTokenNotFound) {
		t.Error("DB error must not be masked as ErrRefreshTokenNotFound")
	}
}

// ── RevokeToken — unsupported token type ─────────────────────────────────────

func TestRevokeToken_UnsupportedTokenType(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	err := svc.RevokeToken(context.Background(), RevokeRequest{
		Token:     "any-token",
		TokenType: "id_token",
	})
	if !errors.Is(err, ErrUnsupportedTokenType) {
		t.Errorf("expected ErrUnsupportedTokenType, got %v", err)
	}
}

// ── InitiateDeviceAuth ────────────────────────────────────────────────────────

func TestInitiateDeviceAuth_NoOIDC(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	_, err := svc.InitiateDeviceAuth(context.Background(), DeviceAuthRequest{
		TenantID: "tenant-1",
	})
	if !errors.Is(err, ErrOIDCNotConfigured) {
		t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
	}
}

func TestInitiateDeviceAuth_WithOIDC_ReturnsDeviceCode(t *testing.T) {
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	resp, err := svc.InitiateDeviceAuth(context.Background(), DeviceAuthRequest{
		TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("InitiateDeviceAuth: %v", err)
	}
	if resp.DeviceCode == "" {
		t.Error("expected non-empty device_code")
	}
	if resp.UserCode == "" {
		t.Error("expected non-empty user_code")
	}
	if resp.VerificationURI == "" {
		t.Error("expected non-empty verification_uri")
	}
}

// ── PollDeviceAuth — error sentinel mapping ───────────────────────────────────

func TestPollDeviceAuth_AuthorizationPending(t *testing.T) {
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"authorization_pending"}`)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	_, err := svc.PollDeviceAuth(context.Background(), SessionRequest{
		TenantID:   "tenant-1",
		DeviceCode: "test-device-code",
	})
	if !errors.Is(err, ErrAuthorizationPending) {
		t.Errorf("expected ErrAuthorizationPending, got %v", err)
	}
}

func TestPollDeviceAuth_SlowDown(t *testing.T) {
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"slow_down"}`)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	_, err := svc.PollDeviceAuth(context.Background(), SessionRequest{
		TenantID:   "tenant-1",
		DeviceCode: "test-device-code",
	})
	if !errors.Is(err, ErrSlowDown) {
		t.Errorf("expected ErrSlowDown, got %v", err)
	}
}

func TestPollDeviceAuth_AccessDenied(t *testing.T) {
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"access_denied"}`)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	_, err := svc.PollDeviceAuth(context.Background(), SessionRequest{
		TenantID:   "tenant-1",
		DeviceCode: "test-device-code",
	})
	if !errors.Is(err, ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied, got %v", err)
	}
}

func TestPollDeviceAuth_ExpiredToken(t *testing.T) {
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"expired_token"}`)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	_, err := svc.PollDeviceAuth(context.Background(), SessionRequest{
		TenantID:   "tenant-1",
		DeviceCode: "test-device-code",
	})
	if !errors.Is(err, ErrDeviceCodeExpired) {
		t.Errorf("expected ErrDeviceCodeExpired, got %v", err)
	}
}

func TestPollDeviceAuth_NoOIDC(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	_, err := svc.PollDeviceAuth(context.Background(), SessionRequest{
		TenantID:   "tenant-1",
		DeviceCode: "any-code",
	})
	if !errors.Is(err, ErrOIDCNotConfigured) {
		t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
	}
}

// ── ExchangeIDToken ───────────────────────────────────────────────────────────

func TestExchangeIDToken_Success(t *testing.T) {
	issuer, issueIDToken := newSigningOIDCTestServer(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	idToken := issueIDToken("test-client", "user-sub-1", "alice@example.com", "Alice Example")

	resp, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
		TenantID: "tenant-1",
		IDToken:  idToken,
	})
	if err != nil {
		t.Fatalf("ExchangeIDToken: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
}

func TestExchangeIDToken_InvalidToken(t *testing.T) {
	issuer, _ := newSigningOIDCTestServer(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	_, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
		TenantID: "tenant-1",
		IDToken:  "not.a.valid.jwt",
	})
	if !errors.Is(err, ErrIDTokenInvalid) {
		t.Errorf("expected ErrIDTokenInvalid, got %v", err)
	}
}

func TestExchangeIDToken_UnknownTenant(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)

	_, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
		TenantID: "nonexistent-tenant",
		IDToken:  "any.token.value",
	})
	if !errors.Is(err, ErrOIDCNotConfigured) {
		t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
	}
}

func TestExchangeIDToken_PropagatesRoles(t *testing.T) {
	issuer, _, key := newSigningOIDCTestServerWithKey(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	// Issue an ID token that includes a roles claim.
	idToken := issueIDTokenWithRoles(t, issuer, "test-client", "user-sub-1", "alice@example.com", "Alice", []string{"tenant-admin", "analyst"}, key)

	resp, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
		TenantID: "tenant-1",
		IDToken:  idToken,
	})
	if err != nil {
		t.Fatalf("ExchangeIDToken: %v", err)
	}

	// Decode the access token (unverified — trusted from our own service)
	// and check that orbit.claims.roles was populated.
	parts := strings.Split(resp.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3-part JWT, got %d parts", len(parts))
	}

	// jwt.MapClaims.ParseUnverified is fine here — we issued the token.
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	tok, _, err := parser.ParseUnverified(resp.AccessToken, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}

	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected MapClaims")
	}

	orbit, ok := claims["orbit"].(map[string]any)
	if !ok {
		t.Fatal("expected orbit claims object")
	}

	orbitClaims, ok := orbit["claims"].(map[string]any)
	if !ok {
		t.Fatal("expected orbit.claims map")
	}

	rolesRaw, ok := orbitClaims["roles"]
	if !ok {
		t.Fatal("expected orbit.claims.roles to be set")
	}

	rolesStr, ok := rolesRaw.(string)
	if !ok {
		t.Fatalf("expected roles to be a string, got %T", rolesRaw)
	}

	if rolesStr != "tenant-admin,analyst" {
		t.Errorf("expected roles %q, got %q", "tenant-admin,analyst", rolesStr)
	}
}

func TestExchangeIDToken_PropagatesGroupsAsRoles(t *testing.T) {
	// OIDC providers like Dex (LDAP connector) use the "groups" claim instead
	// of "roles". The identity service should merge groups into roles.
	issuer, _, key := newSigningOIDCTestServerWithKey(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	idToken := issueIDTokenWithGroups(t, issuer, "test-client", "user-sub-1", "alice@example.com", "Alice", []string{"platform-admin", "users"}, key)

	resp, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
		TenantID: "tenant-1",
		IDToken:  idToken,
	})
	if err != nil {
		t.Fatalf("ExchangeIDToken: %v", err)
	}

	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	tok, _, err := parser.ParseUnverified(resp.AccessToken, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}

	claims := tok.Claims.(jwt.MapClaims)
	orbit := claims["orbit"].(map[string]any)
	orbitClaims := orbit["claims"].(map[string]any)

	rolesStr, ok := orbitClaims["roles"].(string)
	if !ok {
		t.Fatal("expected orbit.claims.roles to be set as a string")
	}

	if rolesStr != "platform-admin,users" {
		t.Errorf("expected roles %q, got %q", "platform-admin,users", rolesStr)
	}
}

func TestExchangeIDToken_NoRolesOmitsClaimField(t *testing.T) {
	// When the OIDC token has no roles claim, the platform token should NOT
	// have a roles field in orbit.claims (avoids empty string pollution).
	issuer, issueIDToken := newSigningOIDCTestServer(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	idToken := issueIDToken("test-client", "user-sub-1", "bob@example.com", "Bob")

	resp, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
		TenantID: "tenant-1",
		IDToken:  idToken,
	})
	if err != nil {
		t.Fatalf("ExchangeIDToken: %v", err)
	}

	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	tok, _, err := parser.ParseUnverified(resp.AccessToken, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}

	claims := tok.Claims.(jwt.MapClaims)
	orbit := claims["orbit"].(map[string]any)
	orbitClaims, ok := orbit["claims"].(map[string]any)
	if !ok {
		t.Fatal("expected orbit.claims map")
	}

	if _, hasRoles := orbitClaims["roles"]; hasRoles {
		t.Error("expected no roles field when OIDC token has no roles claim")
	}
}

// ── DiscoverTenantByEmail ─────────────────────────────────────────────────────

func TestDiscoverTenantByEmail_Success(t *testing.T) {
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithDomains(t, issuer, []string{"acme.com", "acme.io"})

	disc, err := svc.DiscoverTenantByEmail(context.Background(), "alice@acme.com")
	if err != nil {
		t.Fatalf("DiscoverTenantByEmail: %v", err)
	}
	if disc.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", disc.TenantID, "tenant-1")
	}
	if disc.ClientID != "test-client" {
		t.Errorf("ClientID = %q, want %q", disc.ClientID, "test-client")
	}
}

func TestDiscoverTenantByEmail_CaseInsensitive(t *testing.T) {
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithDomains(t, issuer, []string{"acme.com"})

	_, err := svc.DiscoverTenantByEmail(context.Background(), "alice@ACME.COM")
	if err != nil {
		t.Errorf("expected case-insensitive match, got %v", err)
	}
}

func TestDiscoverTenantByEmail_UnknownDomain(t *testing.T) {
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithDomains(t, issuer, []string{"acme.com"})

	_, err := svc.DiscoverTenantByEmail(context.Background(), "bob@other.com")
	if !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("expected ErrTenantNotFound, got %v", err)
	}
}

func TestDiscoverTenantByEmail_NoAtSign(t *testing.T) {
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithDomains(t, issuer, []string{"acme.com"})

	_, err := svc.DiscoverTenantByEmail(context.Background(), "notanemail")
	if !errors.Is(err, ErrInvalidEmailFormat) {
		t.Errorf("expected ErrInvalidEmailFormat, got %v", err)
	}
}

func TestDiscoverTenantByEmail_EmptyDomain(t *testing.T) {
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithDomains(t, issuer, []string{"acme.com"})

	_, err := svc.DiscoverTenantByEmail(context.Background(), "alice@")
	if !errors.Is(err, ErrInvalidEmailFormat) {
		t.Errorf("expected ErrInvalidEmailFormat, got %v", err)
	}
}

// ── discoverTenantOIDCWithRetry ─────────────────────────────────────────────────
//
// Regression coverage for a live CrashLoopBackOff: orbit-identity's OIDC
// discovery at startup used to be a single fatal attempt (initTenantOIDCProviders'
// old contract), which crashed the whole binary on any transient DNS/network
// blip — e.g. a dev-environment vcluster's CoreDNS still rolling out a custom
// split-horizon zone. discoverTenantOIDCWithRetry must retry with backoff
// instead of failing on the first attempt.

func TestDiscoverTenantOIDCWithRetry_RecoversFromTransientFailure(t *testing.T) {
	issuer, attempts := newFlakyOIDCTestServer(t, 2) // fails twice, succeeds on the 3rd
	svc := &Service{
		httpClient: http.DefaultClient,
		logger:     logging.NewLoggerWithOutput(io.Discard),
		cfg: &Config{
			OIDCDiscovery: OIDCDiscoveryConfig{
				ReadyTimeoutSecs: 5,
				BackoffInitialMs: 10,
				BackoffMaxSecs:   1,
			},
		},
	}

	state, err := svc.discoverTenantOIDCWithRetry(context.Background(), "tenant-1", TenantOIDCConfig{IssuerURL: issuer})
	if err != nil {
		t.Fatalf("discoverTenantOIDCWithRetry: %v", err)
	}
	if state == nil || state.provider == nil {
		t.Fatal("expected a resolved provider state")
	}
	if got := attempts(); got < 3 {
		t.Errorf("attempts = %d, want at least 3 (2 failures + 1 success)", got)
	}
}

func TestDiscoverTenantOIDCWithRetry_GivesUpAfterReadyTimeout(t *testing.T) {
	issuer, attempts := newFlakyOIDCTestServer(t, 1<<30) // always fails
	svc := &Service{
		httpClient: http.DefaultClient,
		logger:     logging.NewLoggerWithOutput(io.Discard),
		cfg: &Config{
			OIDCDiscovery: OIDCDiscoveryConfig{
				ReadyTimeoutSecs: 0, // give up immediately after the first failure
				BackoffInitialMs: 10,
				BackoffMaxSecs:   1,
			},
		},
	}

	_, err := svc.discoverTenantOIDCWithRetry(context.Background(), "tenant-1", TenantOIDCConfig{IssuerURL: issuer})
	if err == nil {
		t.Fatal("expected an error once the ready timeout elapses")
	}
	if !strings.Contains(err.Error(), "timed out after") {
		t.Errorf("error = %v, want a timeout error", err)
	}
	if got := attempts(); got == 0 {
		t.Error("expected at least one discovery attempt before giving up")
	}
}
