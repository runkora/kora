package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/token"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// getWithQuery sends a GET with claims and optional query string.
func getWithQuery(t *testing.T, handler http.HandlerFunc, claims *token.PlatformClaims, query string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/"
	if query != "" {
		target = "/?" + query
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if claims != nil {
		req = req.WithContext(token.WithClaims(req.Context(), claims))
	}
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// seedProfile seeds a UserProfile directly into the mock repo.
func seedProfile(profiles *mockUserProfileRepository, id, tenantID, email, displayName string) {
	_ = profiles.Upsert(context.Background(), UserProfile{
		ID:          id,
		TenantID:    tenantID,
		Email:       email,
		DisplayName: displayName,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})
}

// ── handleGetUser ─────────────────────────────────────────────────────────────

func TestHandleGetUser_NoAuth_Returns401(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	req := httptest.NewRequest(http.MethodGet, "/users/user-1", nil)
	req.SetPathValue("id", "user-1")
	w := httptest.NewRecorder()
	c.handleGetUser(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetUser_MissingID_Returns400(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "user-1")
	req := httptest.NewRequest(http.MethodGet, "/users/", nil)
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	// Don't set path value — simulates missing {id}.
	w := httptest.NewRecorder()
	c.handleGetUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetUser_NotFound_Returns404(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "user-1")
	req := httptest.NewRequest(http.MethodGet, "/users/unknown", nil)
	req.SetPathValue("id", "unknown")
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleGetUser(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGetUser_Success_Returns200(t *testing.T) {
	c, _, profiles := newTestControllerWithProfiles(t)
	seedProfile(profiles, "user-1", "t1", "alice@example.com", "Alice")
	claims := humanClaims("t1", "user-1")
	req := httptest.NewRequest(http.MethodGet, "/users/user-1", nil)
	req.SetPathValue("id", "user-1")
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleGetUser(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp UserProfileResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Email != "alice@example.com" {
		t.Errorf("email: got %q, want %q", resp.Email, "alice@example.com")
	}
	if resp.DisplayName != "Alice" {
		t.Errorf("display_name: got %q, want %q", resp.DisplayName, "Alice")
	}
}

func TestHandleGetUser_TenantIsolation_Returns404(t *testing.T) {
	c, _, profiles := newTestControllerWithProfiles(t)
	// Seed profile in a different tenant.
	seedProfile(profiles, "user-1", "other-tenant", "alice@example.com", "Alice")
	claims := humanClaims("t1", "user-1")
	req := httptest.NewRequest(http.MethodGet, "/users/user-1", nil)
	req.SetPathValue("id", "user-1")
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleGetUser(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d — cross-tenant profile must not be visible", w.Code, http.StatusNotFound)
	}
}

// ── handleListUsers ───────────────────────────────────────────────────────────

func TestHandleListUsers_NoAuth_Returns401(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	w := getWithQuery(t, c.handleListUsers, nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleListUsers_EmptyTenant_Returns200WithEmptySlice(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "user-1")
	w := getWithQuery(t, c.handleListUsers, claims, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp UserProfilesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Users == nil {
		t.Error("users must not be nil — expect empty slice, not null")
	}
	if len(resp.Users) != 0 {
		t.Errorf("expected 0 users, got %d", len(resp.Users))
	}
}

func TestHandleListUsers_ReturnsUsersForTenant(t *testing.T) {
	c, _, profiles := newTestControllerWithProfiles(t)
	seedProfile(profiles, "user-1", "t1", "alice@example.com", "Alice")
	seedProfile(profiles, "user-2", "t1", "bob@example.com", "Bob")
	seedProfile(profiles, "user-x", "other-tenant", "eve@example.com", "Eve") // different tenant

	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleListUsers, claims, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp UserProfilesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Users) != 2 {
		t.Errorf("expected 2 users for t1, got %d", len(resp.Users))
	}
}

func TestHandleListUsers_SearchFilter(t *testing.T) {
	c, _, profiles := newTestControllerWithProfiles(t)
	seedProfile(profiles, "user-1", "t1", "alice@example.com", "Alice Smith")
	seedProfile(profiles, "user-2", "t1", "bob@example.com", "Bob Jones")

	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleListUsers, claims, "search=alice")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp UserProfilesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Users) != 1 {
		t.Errorf("expected 1 user matching 'alice', got %d", len(resp.Users))
	}
	if len(resp.Users) == 1 && resp.Users[0].Email != "alice@example.com" {
		t.Errorf("email: got %q, want %q", resp.Users[0].Email, "alice@example.com")
	}
}

func TestHandleListUsers_IDFilter(t *testing.T) {
	c, _, profiles := newTestControllerWithProfiles(t)
	seedProfile(profiles, "user-1", "t1", "alice@example.com", "Alice")
	seedProfile(profiles, "user-2", "t1", "bob@example.com", "Bob")
	seedProfile(profiles, "user-3", "t1", "carol@example.com", "Carol")

	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleListUsers, claims, "ids=user-1,user-3")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp UserProfilesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Users) != 2 {
		t.Errorf("expected 2 users, got %d", len(resp.Users))
	}
}

func TestHandleListUsers_InvalidPage_Returns400(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleListUsers, claims, "page=abc")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleListUsers_InvalidLimit_Returns400(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleListUsers, claims, "limit=0")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ── handleGetPATAttribution ───────────────────────────────────────────────────

func TestHandleGetPATAttribution_NoAuth_Returns401(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	w := getWithQuery(t, c.handleGetPATAttribution, nil, "ids=pat-1")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetPATAttribution_EmptyIDs_Returns200EmptyMap(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleGetPATAttribution, claims, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp PATAttributionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Attribution) != 0 {
		t.Errorf("expected empty attribution map, got %d entries", len(resp.Attribution))
	}
}

func TestHandleGetPATAttribution_TooManyIDs_Returns400(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "caller")
	ids := make([]byte, 0, 101*37)
	for i := range 101 {
		if i > 0 {
			ids = append(ids, ',')
		}
		ids = append(ids, []byte("00000000-0000-0000-0000-000000000000")...)
	}
	w := getWithQuery(t, c.handleGetPATAttribution, claims, "ids="+string(ids))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetPATAttribution_ResolvesTokenName(t *testing.T) {
	c, pats, profiles := newTestControllerWithProfiles(t)
	now := time.Now()
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{ID: "pat-1", TenantID: "t1", UserID: "user-1", Name: "ci-token", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	pats.mu.Unlock()
	seedProfile(profiles, "user-1", "t1", "alice@example.com", "Alice")

	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleGetPATAttribution, claims, "ids=pat-1")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp PATAttributionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	entry, ok := resp.Attribution["pat-1"]
	if !ok {
		t.Fatal("expected attribution entry for pat-1")
	}
	if entry.PATName != "ci-token" {
		t.Errorf("pat_name: got %q, want %q", entry.PATName, "ci-token")
	}
	if entry.Owner == nil {
		t.Fatal("expected owner to be set")
	}
	if entry.Owner.Email != "alice@example.com" {
		t.Errorf("owner email: got %q, want %q", entry.Owner.Email, "alice@example.com")
	}
}

func TestHandleGetPATAttribution_MissingPAT_OmittedFromResponse(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "caller")
	// Request attribution for a PAT that doesn't exist in the repo.
	w := getWithQuery(t, c.handleGetPATAttribution, claims, "ids=non-existent-pat")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp PATAttributionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Attribution) != 0 {
		t.Errorf("expected empty attribution for unknown PAT, got %d entries", len(resp.Attribution))
	}
}

func TestHandleGetPATAttribution_TenantIsolation(t *testing.T) {
	c, pats, _ := newTestControllerWithProfiles(t)
	now := time.Now()
	// PAT belongs to a different tenant.
	pats.mu.Lock()
	pats.pats["pat-x"] = &PAT{ID: "pat-x", TenantID: "other-tenant", UserID: "user-x", Name: "foreign", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	pats.mu.Unlock()

	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleGetPATAttribution, claims, "ids=pat-x")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp PATAttributionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp.Attribution["pat-x"]; ok {
		t.Error("cross-tenant PAT must not appear in attribution response")
	}
}
