package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// newTestController wires up a Controller backed by a Service with mock repos.
// It returns the controller, the mock repos, and the service for sign/parse
// access.
func newTestController(t *testing.T) (
	c *Controller,
	svc *Service,
	sessions *mockSessionRepository,
	refreshTokens *mockRefreshTokenRepository,
	blocklist *mockJTIBlocklistRepository,
) {
	t.Helper()
	svc, sessions, refreshTokens, blocklist = newFullTestService(t)
	c = NewController(svc, logging.NewLoggerWithOutput(io.Discard))
	return
}

// newTestControllerWithOIDC returns a Controller backed by a Service with OIDC configured.
func newTestControllerWithOIDC(t *testing.T, issuerURL string) *Controller {
	t.Helper()
	svc := newTestServiceWithOIDC(t, issuerURL,
		newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(),
	)
	return NewController(svc, logging.NewLoggerWithOutput(io.Discard))
}

// postJSON performs an HTTP POST with a JSON body against the given handler.
func postJSON(t *testing.T, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// postForm performs an HTTP POST with an application/x-www-form-urlencoded body.
func postForm(t *testing.T, handler http.HandlerFunc, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// getRequest performs an HTTP GET against the given handler.
func getRequest(t *testing.T, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// decodeErrorCode decodes the "code" field from a JSON error response body.
func decodeErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var v struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decodeErrorCode: %v (body: %s)", err, body)
	}
	return v.Code
}

// ── handleInitiateDeviceAuth ──────────────────────────────────────────────────

func TestHandleInitiateDeviceAuth_MissingTenantID(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleInitiateDeviceAuth, map[string]string{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	code := decodeErrorCode(t, w.Body.Bytes())
	if code != "missing_required_field" {
		t.Errorf("error code: got %q, want %q", code, "missing_required_field")
	}
}

func TestHandleInitiateDeviceAuth_OIDCNotConfigured(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleInitiateDeviceAuth, map[string]string{"tenant_id": "t1"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	code := decodeErrorCode(t, w.Body.Bytes())
	if code != "oidc_not_configured" {
		t.Errorf("error code: got %q, want %q", code, "oidc_not_configured")
	}
}

func TestHandleInitiateDeviceAuth_Success(t *testing.T) {
	issuer, _ := newOIDCTestServer(t)
	c := newTestControllerWithOIDC(t, issuer)
	w := postJSON(t, c.handleInitiateDeviceAuth, map[string]string{"tenant_id": "t1"})
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp DeviceAuthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DeviceCode == "" {
		t.Error("expected non-empty device_code")
	}
	if resp.UserCode == "" {
		t.Error("expected non-empty user_code")
	}
}

func TestHandleInitiateDeviceAuth_InvalidJSON(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.handleInitiateDeviceAuth(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ── handleSession — validation ────────────────────────────────────────────────

func TestHandleSession_MissingTenantID(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleSession, map[string]string{"grant_type": "refresh_token"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSession_MissingGrantType(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleSession, map[string]string{"tenant_id": "t1"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	code := decodeErrorCode(t, w.Body.Bytes())
	if code != "missing_required_field" {
		t.Errorf("error code: got %q, want %q", code, "missing_required_field")
	}
}

func TestHandleSession_UnsupportedGrantType(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":  "t1",
		"grant_type": "password",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	code := decodeErrorCode(t, w.Body.Bytes())
	if code != "invalid_field" {
		t.Errorf("error code: got %q, want %q", code, "invalid_field")
	}
}

func TestHandleSession_DeviceCode_MissingDeviceCode(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":  "t1",
		"grant_type": "urn:ietf:params:oauth:grant-type:device_code",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	code := decodeErrorCode(t, w.Body.Bytes())
	if code != "missing_required_field" {
		t.Errorf("error code: got %q, want %q", code, "missing_required_field")
	}
}

func TestHandleSession_RefreshToken_MissingToken(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":  "t1",
		"grant_type": "refresh_token",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	code := decodeErrorCode(t, w.Body.Bytes())
	if code != "missing_required_field" {
		t.Errorf("error code: got %q, want %q", code, "missing_required_field")
	}
}

// ── handleSession — error mapping via handleSessionError ──────────────────────

func TestHandleSession_AuthorizationPending_Returns202(t *testing.T) {
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"authorization_pending"}`)
	c := newTestControllerWithOIDC(t, issuer)

	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":   "t1",
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": "test-device-code",
	})
	if w.Code != http.StatusAccepted {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusAccepted, w.Body)
	}
	code := decodeErrorCode(t, w.Body.Bytes())
	if code != "authorization_pending" {
		t.Errorf("code: got %q, want %q", code, "authorization_pending")
	}
}

func TestHandleSession_SlowDown_Returns202(t *testing.T) {
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"slow_down"}`)
	c := newTestControllerWithOIDC(t, issuer)

	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":   "t1",
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": "test-device-code",
	})
	if w.Code != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusAccepted)
	}
	code := decodeErrorCode(t, w.Body.Bytes())
	if code != "slow_down" {
		t.Errorf("code: got %q, want %q", code, "slow_down")
	}
}

func TestHandleSession_AccessDenied_Returns403(t *testing.T) {
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"access_denied"}`)
	c := newTestControllerWithOIDC(t, issuer)

	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":   "t1",
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": "test-device-code",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleSession_DeviceCodeExpired_Returns400(t *testing.T) {
	issuer, setResp := newOIDCTestServer(t)
	setResp(http.StatusBadRequest, `{"error":"expired_token"}`)
	c := newTestControllerWithOIDC(t, issuer)

	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":   "t1",
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": "test-device-code",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSession_RefreshTokenNotFound_Returns401(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":     "t1",
		"grant_type":    "refresh_token",
		"refresh_token": "nonexistent",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSession_RefreshTokenRevoked_Returns401(t *testing.T) {
	c, svc, sessions, refreshTokens, _ := newTestController(t)
	_ = svc

	rawToken := "revoked-refresh"
	seedSession(sessions, "sess-1", "jti-1")
	rt := seedRefreshToken(refreshTokens, "rt-1", "sess-1", rawToken)
	revokedAt := time.Now().Add(-time.Minute)
	refreshTokens.mu.Lock()
	rt.RevokedAt = &revokedAt
	refreshTokens.mu.Unlock()

	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":     "t1",
		"grant_type":    "refresh_token",
		"refresh_token": rawToken,
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusUnauthorized, w.Body)
	}
}

func TestHandleSession_TokenRevoked_Returns401(t *testing.T) {
	c, _, sessions, refreshTokens, _ := newTestController(t)

	rawToken := "valid-refresh"
	s := seedSession(sessions, "sess-1", "jti-1")
	seedRefreshToken(refreshTokens, "rt-1", "sess-1", rawToken)
	revokedAt := time.Now().Add(-time.Minute)
	sessions.mu.Lock()
	s.RevokedAt = &revokedAt
	sessions.mu.Unlock()

	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":     "t1",
		"grant_type":    "refresh_token",
		"refresh_token": rawToken,
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSession_RefreshToken_Success(t *testing.T) {
	c, _, sessions, refreshTokens, _ := newTestController(t)

	rawToken := "valid-refresh-token"
	seedSession(sessions, "sess-1", "jti-1")
	seedRefreshToken(refreshTokens, "rt-1", "sess-1", rawToken)

	w := postJSON(t, c.handleSession, map[string]string{
		"tenant_id":     "t1",
		"grant_type":    "refresh_token",
		"refresh_token": rawToken,
	})
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp SessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type: got %q, want Bearer", resp.TokenType)
	}
}

// ── handleExchange ────────────────────────────────────────────────────────────

// postJSONWithXFCC is like postJSON but also sets an x-forwarded-client-cert
// header on the request, simulating the Envoy sidecar injection.
func postJSONWithXFCC(t *testing.T, handler http.HandlerFunc, body interface{}, xfcc string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if xfcc != "" {
		req.Header.Set("x-forwarded-client-cert", xfcc)
	}
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestHandleExchange_MissingXFCC_Returns401(t *testing.T) {
	// No x-forwarded-client-cert header means the request did not arrive via
	// the Envoy mTLS sidecar — must be rejected with 401.
	c, _, _, _, _ := newTestController(t)
	w := postJSONWithXFCC(t, c.handleExchange, map[string]string{}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusUnauthorized, w.Body)
	}
}

func TestHandleExchange_InvalidXFCC_Returns401(t *testing.T) {
	// A malformed XFCC header (no URI= field) must be rejected with 401.
	c, _, _, _, _ := newTestController(t)
	w := postJSONWithXFCC(t, c.handleExchange,
		map[string]string{},
		"Hash=abc123;Cert=something",
	)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusUnauthorized, w.Body)
	}
}

func TestHandleExchange_Success(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	xfcc := "URI=spiffe://orbit.local/tenant/platform-team/agent/agent-1"
	w := postJSONWithXFCC(t, c.handleExchange, map[string]string{}, xfcc)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp SessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.RefreshToken != "" {
		t.Error("agent exchange must not return a refresh_token")
	}
}

// ── handleRevoke ──────────────────────────────────────────────────────────────

func TestHandleRevoke_MissingToken(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postForm(t, c.handleRevoke, map[string]string{"token_type_hint": "access_token"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRevoke_MissingTokenType(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := postForm(t, c.handleRevoke, map[string]string{"token": "some-token"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRevoke_InvalidToken_Returns204(t *testing.T) {
	// RFC 7009 §2.1: revocation endpoint must return success for invalid tokens
	c, _, _, _, _ := newTestController(t)
	w := postForm(t, c.handleRevoke, map[string]string{
		"token":           "not.a.jwt",
		"token_type_hint": "access_token",
	})
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusNoContent, w.Body)
	}
}

func TestHandleRevoke_RefreshTokenNotFound_IsIdempotent(t *testing.T) {
	// RFC 7009 §2.1: revocation of an unknown or already-revoked refresh token
	// must succeed (idempotent) — the client's goal (token unusable) is already met.
	c, _, _, _, _ := newTestController(t)
	w := postForm(t, c.handleRevoke, map[string]string{
		"token":           "nonexistent-refresh-token",
		"token_type_hint": "refresh_token",
	})
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleRevoke_AccessToken_Success(t *testing.T) {
	c, svc, sessions, _, _ := newTestController(t)

	principal := &Principal{
		ID:             "user-1",
		Type:           PrincipalTypeHuman,
		TenantID:       "t1",
		IdentitySource: IdentitySourceFederated,
	}
	jti := "revoke-jti-1"
	sessionID := "revoke-sess-1"
	token, err := svc.signToken(principal, jti, sessionID, time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	seedSession(sessions, sessionID, jti)

	w := postForm(t, c.handleRevoke, map[string]string{
		"token":           token,
		"token_type_hint": "access_token",
	})
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusNoContent, w.Body)
	}
}

func TestHandleRevoke_RefreshToken_Success(t *testing.T) {
	c, _, sessions, refreshTokens, _ := newTestController(t)

	rawToken := "opaque-refresh-to-revoke"
	seedSession(sessions, "sess-1", "jti-1")
	seedRefreshToken(refreshTokens, "rt-1", "sess-1", rawToken)

	w := postForm(t, c.handleRevoke, map[string]string{
		"token":           rawToken,
		"token_type_hint": "refresh_token",
	})
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusNoContent, w.Body)
	}
}

// Revoking an already-blocked access token must be idempotent — RFC 7009 §2.1
// states the server SHOULD respond with 200 even if the token is already
// invalid.  Our implementation returns 204 No Content on success.
func TestHandleRevoke_AlreadyBlockedAccessToken_IsIdempotent(t *testing.T) {
	c, svc, sessions, _, blocklist := newTestController(t)

	principal := &Principal{
		ID:             "user-1",
		Type:           PrincipalTypeHuman,
		TenantID:       "t1",
		IdentitySource: IdentitySourceFederated,
	}
	jti := "already-blocked-jti"
	token, err := svc.signToken(principal, jti, "sess-1", time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	// Pre-seed the blocklist to simulate a JTI that was blocked in a prior call.
	blocklist.mu.Lock()
	blocklist.blocked[jti] = true
	blocklist.mu.Unlock()
	// Session must still exist so GetSessionByJTI succeeds.
	seedSession(sessions, "sess-1", jti)

	w := postForm(t, c.handleRevoke, map[string]string{
		"token":           token,
		"token_type_hint": "access_token",
	})
	// BlockJTI is idempotent; the service returns nil and the handler 204.
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusNoContent, w.Body)
	}
}

func TestHandleRevoke_UnsupportedTokenType_Returns400(t *testing.T) {
	// token_type_hint values other than "access_token" / "refresh_token" must
	// be rejected with HTTP 400 and the invalid_field error code.
	c, _, _, _, _ := newTestController(t)
	w := postForm(t, c.handleRevoke, map[string]string{
		"token":           "some-token-value",
		"token_type_hint": "id_token",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := body["code"].(string); got != string(api.ErrCodeInvalidField) {
		t.Errorf("error code: got %q, want %q", got, api.ErrCodeInvalidField)
	}
}

// ── handleJWKS ────────────────────────────────────────────────────────────────

func TestHandleJWKS_StatusOK(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := getRequest(t, c.handleJWKS)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleJWKS_ContentType(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := getRequest(t, c.handleJWKS)
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}
}

func TestHandleJWKS_CacheControlHeader(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := getRequest(t, c.handleJWKS)
	cc := w.Header().Get("Cache-Control")
	if cc != "public, max-age=3600" {
		t.Errorf("Cache-Control: got %q, want %q", cc, "public, max-age=3600")
	}
}

func TestHandleJWKS_ValidJSON(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := getRequest(t, c.handleJWKS)
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("JWKS response is not valid JSON: %v", err)
	}
}

func TestHandleJWKS_ContainsKeys(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	w := getRequest(t, c.handleJWKS)
	var body struct {
		Keys []interface{} `json:"keys"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode JWKS: %v", err)
	}
	if len(body.Keys) == 0 {
		t.Error("JWKS keys array is empty")
	}
}

// ── Controller metadata ───────────────────────────────────────────────────────

func TestController_GroupedBy(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	if got := c.GroupedBy(); got != "/identity" {
		t.Errorf("GroupedBy: got %q, want %q", got, "/identity")
	}
}

func TestController_Middleware_IsNil(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	if c.Middleware() != nil {
		t.Error("Middleware should return nil for the identity controller")
	}
}

// ── ErrTokenRevoked routing sanity check ─────────────────────────────────────

// handleSessionError is exercised above via the full handler path.  This test
// directly calls the helper so the error→status mapping is explicit.
func TestHandleSessionError_ErrTokenRevoked_Returns401(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	c.handleSessionError(w, req, ErrTokenRevoked)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSessionError_ErrSessionNotFound_Returns401(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	c.handleSessionError(w, req, ErrSessionNotFound)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSessionError_ErrRefreshTokenExpired_Returns401(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	c.handleSessionError(w, req, ErrRefreshTokenExpired)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSessionError_UnknownError_Returns500(t *testing.T) {
	c, _, _, _, _ := newTestController(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	c.handleSessionError(w, req, errors.New("unexpected internal failure"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
