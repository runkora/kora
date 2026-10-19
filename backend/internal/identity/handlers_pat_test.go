package identity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// humanClaims returns a minimal set of HUMAN PlatformClaims for the given
// tenant and user.
func humanClaims(tenantID, userID string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Orbit: token.OrbitClaims{
			TenantID:      tenantID,
			PrincipalType: token.PrincipalTypeHuman,
			DisplayName:   "Test User",
		},
	}
}

// patTokenClaims returns PlatformClaims with PrincipalTypePAT set.
func patTokenClaims(tenantID, userID string) *token.PlatformClaims {
	c := humanClaims(tenantID, userID)
	c.Orbit.PrincipalType = token.PrincipalTypePAT
	return c
}

// postJSONWithClaims sends a POST with a JSON body and the given claims in context.
func postJSONWithClaims(t *testing.T, handler http.HandlerFunc, claims *token.PlatformClaims, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if claims != nil {
		req = req.WithContext(token.WithClaims(req.Context(), claims))
	}
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// getWithClaims sends a GET with the given claims in context.
func getWithClaims(t *testing.T, handler http.HandlerFunc, claims *token.PlatformClaims) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if claims != nil {
		req = req.WithContext(token.WithClaims(req.Context(), claims))
	}
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// deleteWithClaims sends a DELETE with the given claims and path value in context.
func deleteWithClaims(t *testing.T, handler http.HandlerFunc, claims *token.PlatformClaims, patID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/tokens/"+patID, nil)
	req.SetPathValue("id", patID)
	if claims != nil {
		req = req.WithContext(token.WithClaims(req.Context(), claims))
	}
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// ── handleCreatePAT ───────────────────────────────────────────────────────────

func TestHandleCreatePAT_NoAuth_Returns401(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	w := postJSONWithClaims(t, c.handleCreatePAT, nil, map[string]any{"name": "my-pat"})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleCreatePAT_PATToken_Returns403(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := patTokenClaims("t1", "user-1")
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": "my-pat"})
	// The rejectPATMiddleware is wired via Boot(); calling the handler directly
	// bypasses it. Verify the handler itself allows it — the middleware test
	// covers the 403 case.
	// This test confirms the handler itself does not block PAT principals.
	if w.Code == http.StatusUnauthorized {
		t.Errorf("handler should not reject based on principal type (middleware does that), got 401")
	}
}

func TestHandleCreatePAT_MissingName_Returns400(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": ""})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	if code := decodeErrorCode(t, w.Body.Bytes()); code != "missing_required_field" {
		t.Errorf("error code: got %q, want %q", code, "missing_required_field")
	}
}

func TestHandleCreatePAT_NameTooLong_Returns400(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	longName := make([]byte, 129)
	for i := range longName {
		longName[i] = 'x'
	}
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": string(longName)})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreatePAT_NegativeTTL_Returns400(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": "my-pat", "expires_in_days": -1})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreatePAT_TTLOverMax_Returns400(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	// PATMaxTTLDays is 365 in tests; 366 must be rejected.
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": "my-pat", "expires_in_days": 366})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreatePAT_Success_Returns201(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": "ci-token", "expires_in_days": 30, "scopes": []string{"inference:invoke"}})
	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusCreated, w.Body)
	}

	var resp CreatePATResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if resp.Name != "ci-token" {
		t.Errorf("name: got %q, want %q", resp.Name, "ci-token")
	}
	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.ExpiresAt == "" {
		t.Error("expected non-empty expires_at")
	}
}

func TestHandleCreatePAT_TokenPrefixed(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": "ci-token", "scopes": []string{"inference:invoke"}})
	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body: %s)", w.Code, w.Body)
	}
	var resp CreatePATResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	prefix := PatTokenPrefix
	if len(resp.Token) < len(prefix) || resp.Token[:len(prefix)] != prefix {
		t.Errorf("token should be prefixed with %q, got: %q", prefix, resp.Token[:min(10, len(resp.Token))])
	}
}

func TestHandleCreatePAT_TooManyScopes_Returns400(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	scopes := make([]string, 101)
	for i := range scopes {
		scopes[i] = "inference:invoke"
	}
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": "my-pat", "scopes": scopes})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body)
	}
}

func TestHandleCreatePAT_InvalidScopeName_Returns400(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": "my-pat", "scopes": []string{"INVALID SCOPE!"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body)
	}
}

func TestHandleCreatePAT_DefaultTTL_WhenZero(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	// No expires_in_days — service should default to PATDefaultTTLDays (90).
	w := postJSONWithClaims(t, c.handleCreatePAT, claims, map[string]any{"name": "default-ttl-token", "scopes": []string{"inference:invoke"}})
	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201 (body: %s)", w.Code, w.Body)
	}
}

// ── handleListPATs ────────────────────────────────────────────────────────────

func TestHandleListPATs_NoAuth_Returns401(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	w := getWithClaims(t, c.handleListPATs, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleListPATs_EmptyList_ReturnsEmptySlice(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	w := getWithClaims(t, c.handleListPATs, claims)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}

	var resp ListPATsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Tokens == nil {
		t.Error("tokens field should not be nil — expect empty slice, not null")
	}
	if len(resp.Tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(resp.Tokens))
	}
}

func TestHandleListPATs_ReturnsPATs(t *testing.T) {
	c, _, _, _, _, pats := newTestController(t)
	claims := humanClaims("t1", "user-1")

	// Seed two PATs for user-1 and one for a different user.
	now := time.Now().UTC()
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{ID: "pat-1", TenantID: "t1", UserID: "user-1", Name: "first", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}
	pats.pats["pat-2"] = &PAT{ID: "pat-2", TenantID: "t1", UserID: "user-1", Name: "second", ExpiresAt: now.Add(48 * time.Hour), CreatedAt: now}
	pats.pats["pat-3"] = &PAT{ID: "pat-3", TenantID: "t1", UserID: "user-2", Name: "other", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}
	pats.mu.Unlock()

	w := getWithClaims(t, c.handleListPATs, claims)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}

	var resp ListPATsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Tokens) != 2 {
		t.Errorf("expected 2 tokens for user-1, got %d", len(resp.Tokens))
	}
}

func TestHandleListPATs_RevokedPATIncluded(t *testing.T) {
	c, _, _, _, _, pats := newTestController(t)
	claims := humanClaims("t1", "user-1")

	now := time.Now().UTC()
	revokedAt := now.Add(-time.Hour)
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{ID: "pat-1", TenantID: "t1", UserID: "user-1", Name: "active", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}
	pats.pats["pat-2"] = &PAT{ID: "pat-2", TenantID: "t1", UserID: "user-1", Name: "revoked", ExpiresAt: now.Add(48 * time.Hour), CreatedAt: now, RevokedAt: &revokedAt}
	pats.mu.Unlock()

	w := getWithClaims(t, c.handleListPATs, claims)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}

	var resp ListPATsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Tokens) != 2 {
		t.Errorf("expected 2 tokens (active + revoked), got %d", len(resp.Tokens))
	}
	var foundRevoked bool
	for _, tok := range resp.Tokens {
		if tok.ID == "pat-2" {
			foundRevoked = true
			if tok.RevokedAt == nil {
				t.Error("expected revoked_at to be populated for revoked PAT")
			}
		}
	}
	if !foundRevoked {
		t.Error("revoked PAT was not present in list response")
	}
}

// ── handleRevokePAT ───────────────────────────────────────────────────────────

func TestHandleRevokePAT_NoAuth_Returns401(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	w := deleteWithClaims(t, c.handleRevokePAT, nil, "pat-1")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleRevokePAT_MissingID_Returns400(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	// Don't set path value — simulates missing {id}.
	req := httptest.NewRequest(http.MethodDelete, "/tokens/", nil)
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleRevokePAT(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRevokePAT_NotFound_Returns404(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := humanClaims("t1", "user-1")
	// No PAT seeded — mock repo returns ErrPATNotFound.
	w := deleteWithClaims(t, c.handleRevokePAT, claims, "non-existent-pat")
	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusNotFound, w.Body)
	}
}

func TestHandleRevokePAT_CrossUserPAT_Returns404(t *testing.T) {
	c, _, _, _, _, pats := newTestController(t)
	// Seed a PAT belonging to user-2.
	now := time.Now().UTC()
	pats.mu.Lock()
	pats.pats["pat-other"] = &PAT{ID: "pat-other", TenantID: "t1", UserID: "user-2", Name: "other", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}
	pats.mu.Unlock()

	// user-1 tries to revoke user-2's PAT — must get 404, not 403.
	claims := humanClaims("t1", "user-1")
	w := deleteWithClaims(t, c.handleRevokePAT, claims, "pat-other")
	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404 for cross-user token (got %d, body: %s)", w.Code, w.Code, w.Body)
	}
}

func TestHandleRevokePAT_Success_Returns204(t *testing.T) {
	c, _, _, _, blocklist, pats := newTestController(t)
	claims := humanClaims("t1", "user-1")

	now := time.Now().UTC()
	jti := "jti-to-block"
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{
		ID:        "pat-1",
		TenantID:  "t1",
		UserID:    "user-1",
		Name:      "my-pat",
		JTI:       jti,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	pats.mu.Unlock()

	w := deleteWithClaims(t, c.handleRevokePAT, claims, "pat-1")
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusNoContent, w.Body)
	}

	// JTI must be blocklisted.
	blocklist.mu.Lock()
	blocked := blocklist.blocked[jti]
	blocklist.mu.Unlock()
	if !blocked {
		t.Error("expected JTI to be added to blocklist after revocation")
	}
}

// rejectPATMiddleware is wired in Boot(); simulate it here to verify it blocks
// PAT principals from mutating PATs.
func TestRejectPATMiddleware_BlocksPATPrincipal(t *testing.T) {
	c, _, _, _, _, _ := newTestController(t)
	claims := patTokenClaims("t1", "user-1")

	// Wrap the handler with rejectPATMiddleware to mirror the Boot() wiring.
	chain := mw.MiddlewareChain(rejectPATMiddleware(c.logger))
	handler := chain(http.HandlerFunc(c.handleCreatePAT))

	raw, _ := json.Marshal(map[string]any{"name": "my-pat"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusForbidden, w.Body)
	}
}
