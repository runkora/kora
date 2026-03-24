package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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
	svc, _, _, _ := newFullTestService(t)
	raw := svc.JWKSJSON()
	if len(raw) == 0 {
		t.Fatal("JWKSJSON returned empty bytes")
	}
}

func TestJWKSJSON_ContainsKty(t *testing.T) {
	svc, _, _, _ := newFullTestService(t)
	raw := svc.JWKSJSON()
	if !strings.Contains(string(raw), `"kty"`) {
		t.Errorf("JWKS JSON does not contain kty field: %s", raw)
	}
}

func TestJWKSJSON_ContainsRSA(t *testing.T) {
	svc, _, _, _ := newFullTestService(t)
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
	svc, sessions, _, _ := newFullTestService(t)

	r := xfccRequest("spiffe://orbit.local/tenant/platform-team/agent/agent-1")
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
	const spiffeID = "spiffe://orbit.local/tenant/platform-team/agent/agent-99"
	svc, _, _, _ := newFullTestService(t)

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
	svc, _, _, _ := newFullTestService(t)

	r := xfccRequest("spiffe://orbit.local/tenant/acme-corp/agent/deploy-bot")
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
	svc, _, _, _ := newFullTestService(t)

	r := httptest.NewRequest(http.MethodPost, "/exchange", nil) // no XFCC header
	_, err := svc.ExchangeSVID(context.Background(), r)
	if err == nil {
		t.Fatal("expected error when XFCC header is absent")
	}
}

func TestExchangeSVID_SessionPersistError(t *testing.T) {
	sessions := newMockSessionRepo()
	sessions.createErr = errors.New("db unavailable")
	svc := newTestService(t, sessions, newMockRefreshTokenRepo(), newMockBlocklistRepo())

	r := xfccRequest("spiffe://orbit.local/tenant/platform-team/agent/agent-1")
	_, err := svc.ExchangeSVID(context.Background(), r)
	if err == nil {
		t.Fatal("expected error when session persistence fails")
	}
}

// ── RefreshSession ────────────────────────────────────────────────────────────

func TestRefreshSession_Success(t *testing.T) {
	svc, sessions, refreshTokens, _ := newFullTestService(t)

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
	svc, sessions, refreshTokens, _ := newFullTestService(t)

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
	svc, _, _, _ := newFullTestService(t)
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
	svc, sessions, refreshTokens, _ := newFullTestService(t)

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
	svc, sessions, refreshTokens, _ := newFullTestService(t)

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
	svc, sessions, refreshTokens, _ := newFullTestService(t)

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
	svc := newTestService(t, sessions, refreshTokens, newMockBlocklistRepo())

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
	svc, sessions, _, blocklist := newFullTestService(t)

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
	token, err := svc.signToken(principal, jti, sessionID, now, ttl)
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
	svc, _, _, _ := newFullTestService(t)
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
	svc, sessions, _, blocklist := newFullTestService(t)

	principal := &Principal{
		ID:             "user-1",
		Type:           PrincipalTypeHuman,
		TenantID:       "tenant-1",
		IdentitySource: IdentitySourceFederated,
	}
	jti := "blocked-jti"
	token, err := svc.signToken(principal, jti, "sess-1", time.Now(), time.Hour)
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
	svc, _, _, _ := newFullTestService(t)

	principal := &Principal{
		ID:             "user-1",
		Type:           PrincipalTypeHuman,
		TenantID:       "tenant-1",
		IdentitySource: IdentitySourceFederated,
	}
	token, err := svc.signToken(principal, "jti-no-session", "sess-missing", time.Now(), time.Hour)
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
	svc, _, refreshTokens, _ := newFullTestService(t)

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
	svc, _, _, _ := newFullTestService(t)
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
	svc := newTestService(t, sessions, refreshTokens, newMockBlocklistRepo())

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
	svc, _, _, _ := newFullTestService(t)
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
	svc, _, _, _ := newFullTestService(t)
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
	svc, _, _, _ := newFullTestService(t)
	_, err := svc.PollDeviceAuth(context.Background(), SessionRequest{
		TenantID:   "tenant-1",
		DeviceCode: "any-code",
	})
	if !errors.Is(err, ErrOIDCNotConfigured) {
		t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
	}
}

// ── InitiateLogin ─────────────────────────────────────────────────────────────

func TestInitiateLogin_NoOIDC(t *testing.T) {
	svc, _, _, _ := newFullTestService(t)
	_, err := svc.InitiateLogin(context.Background(), LoginRequest{TenantID: "tenant-1"})
	if !errors.Is(err, ErrOIDCNotConfigured) {
		t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
	}
}

func TestInitiateLogin_Success_ReturnsAuthURL(t *testing.T) {
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	resp, err := svc.InitiateLogin(context.Background(), LoginRequest{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("InitiateLogin: %v", err)
	}
	if resp.AuthorizationURL == "" {
		t.Fatal("expected non-empty authorization_url")
	}

	u, err := url.Parse(resp.AuthorizationURL)
	if err != nil {
		t.Fatalf("parse authorization_url: %v", err)
	}
	q := u.Query()

	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type: got %q, want %q", got, "code")
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method: got %q, want %q", got, "S256")
	}
	if q.Get("code_challenge") == "" {
		t.Error("expected non-empty code_challenge")
	}
	if q.Get("state") == "" {
		t.Error("expected non-empty state")
	}
	if got := q.Get("client_id"); got != "test-client" {
		t.Errorf("client_id: got %q, want %q", got, "test-client")
	}
	if q.Get("redirect_uri") == "" {
		t.Error("expected non-empty redirect_uri")
	}
}

func TestInitiateLogin_Success_VerifierStoredInPKCE(t *testing.T) {
	// After InitiateLogin the PKCE verifier must be stored in the repo under
	// the same state key that appears in the authorization URL.
	issuer, _ := newOIDCTestServer(t)
	pkceRepo := newMockPKCERepo()
	svc := newTestServiceWithOIDCAndPKCE(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
		pkceRepo,
	)

	resp, err := svc.InitiateLogin(context.Background(), LoginRequest{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("InitiateLogin: %v", err)
	}

	u, err := url.Parse(resp.AuthorizationURL)
	if err != nil {
		t.Fatalf("parse authorization_url: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("state is empty in authorization_url")
	}

	pkceRepo.mu.Lock()
	_, stored := pkceRepo.verifiers[state]
	pkceRepo.mu.Unlock()

	if !stored {
		t.Errorf("no PKCE verifier stored for state %q", state)
	}
}

func TestInitiateLogin_Success_UniqueStatePerCall(t *testing.T) {
	// Each call must produce a distinct state token.
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	r1, err := svc.InitiateLogin(context.Background(), LoginRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("first InitiateLogin: %v", err)
	}
	r2, err := svc.InitiateLogin(context.Background(), LoginRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("second InitiateLogin: %v", err)
	}

	u1, _ := url.Parse(r1.AuthorizationURL)
	u2, _ := url.Parse(r2.AuthorizationURL)
	s1 := u1.Query().Get("state")
	s2 := u2.Query().Get("state")

	if s1 == s2 {
		t.Errorf("two InitiateLogin calls produced identical state values: %q", s1)
	}
}

func TestInitiateLogin_StoreError(t *testing.T) {
	// If Redis is unavailable, InitiateLogin must surface the error rather than
	// returning a URL whose state will never be resolved.
	issuer, _ := newOIDCTestServer(t)
	pkceRepo := newMockPKCERepo()
	pkceRepo.storeErr = errors.New("redis connection refused")
	svc := newTestServiceWithOIDCAndPKCE(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
		pkceRepo,
	)

	_, err := svc.InitiateLogin(context.Background(), LoginRequest{TenantID: "tenant-1"})
	if err == nil {
		t.Fatal("expected error when PKCE store fails")
	}
}

// ── ExchangeAuthorizationCode ─────────────────────────────────────────────────

func TestExchangeAuthorizationCode_NoOIDC(t *testing.T) {
	svc, _, _, _ := newFullTestService(t)
	_, err := svc.ExchangeAuthorizationCode(context.Background(), SessionRequest{
		TenantID:  "tenant-1",
		GrantType: "authorization_code",
		Code:      "some-code",
		State:     "some-state",
	})
	if !errors.Is(err, ErrOIDCNotConfigured) {
		t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
	}
}

func TestExchangeAuthorizationCode_StateMismatch(t *testing.T) {
	// A state value not present in the PKCE store must return ErrStateMismatch.
	issuer, _ := newOIDCTestServer(t)
	svc := newTestServiceWithOIDC(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)

	_, err := svc.ExchangeAuthorizationCode(context.Background(), SessionRequest{
		TenantID:  "tenant-1",
		GrantType: "authorization_code",
		Code:      "any-code",
		State:     "nonexistent-state",
	})
	if !errors.Is(err, ErrStateMismatch) {
		t.Errorf("expected ErrStateMismatch, got %v", err)
	}
}

func TestExchangeAuthorizationCode_VerifierConsumedAfterExchange(t *testing.T) {
	// GetVerifier is single-use: after exchange the state must no longer exist.
	issuer, setResp := newOIDCTestServer(t)
	// Token endpoint returns an error (no id_token available in test server) —
	// that is fine; we only want to verify the PKCE store was consumed.
	setResp(http.StatusBadRequest, `{"error":"invalid_grant"}`)
	pkceRepo := newMockPKCERepo()
	svc := newTestServiceWithOIDCAndPKCE(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
		pkceRepo,
	)

	// Seed a verifier manually.
	const state = "test-state-consume"
	pkceRepo.mu.Lock()
	pkceRepo.verifiers[state] = "dGVzdHZlcmlmaWVy"
	pkceRepo.mu.Unlock()

	// The exchange will fail at the token endpoint — but the verifier must be
	// consumed (deleted) before that error is returned.
	_, _ = svc.ExchangeAuthorizationCode(context.Background(), SessionRequest{
		TenantID:  "tenant-1",
		GrantType: "authorization_code",
		Code:      "any-code",
		State:     state,
	})

	pkceRepo.mu.Lock()
	_, stillPresent := pkceRepo.verifiers[state]
	pkceRepo.mu.Unlock()

	if stillPresent {
		t.Error("PKCE verifier was not consumed after exchange attempt")
	}
}

func TestExchangeAuthorizationCode_TokenEndpointError(t *testing.T) {
	// An OAuth error from the token endpoint must be propagated, not swallowed.
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"invalid_grant"}`)
	pkceRepo := newMockPKCERepo()
	svc := newTestServiceWithOIDCAndPKCE(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
		pkceRepo,
	)

	const state = "test-state-err"
	pkceRepo.mu.Lock()
	pkceRepo.verifiers[state] = "dGVzdHZlcmlmaWVy"
	pkceRepo.mu.Unlock()

	_, err := svc.ExchangeAuthorizationCode(context.Background(), SessionRequest{
		TenantID:  "tenant-1",
		GrantType: "authorization_code",
		Code:      "bad-code",
		State:     state,
	})
	if err == nil {
		t.Fatal("expected error from token endpoint failure")
	}
}

func TestExchangeAuthorizationCode_GetVerifierError(t *testing.T) {
	// A non-not-found error from the PKCE store (e.g. Redis down) must be
	// propagated as-is rather than mapped to ErrStateMismatch.
	issuer, _ := newOIDCTestServer(t)
	pkceRepo := newMockPKCERepo()
	pkceRepo.getErr = errors.New("redis unavailable")
	svc := newTestServiceWithOIDCAndPKCE(t, issuer,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
		pkceRepo,
	)

	_, err := svc.ExchangeAuthorizationCode(context.Background(), SessionRequest{
		TenantID:  "tenant-1",
		GrantType: "authorization_code",
		Code:      "any-code",
		State:     "any-state",
	})
	if err == nil {
		t.Fatal("expected error from PKCE store failure")
	}
	if errors.Is(err, ErrStateMismatch) {
		t.Error("Redis error must not be mapped to ErrStateMismatch")
	}
}
