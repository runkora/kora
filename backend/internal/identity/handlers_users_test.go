package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
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
	// The caller's own id, so no directory scope is needed to reach the lookup.
	claims := humanClaims("t1", "unknown")
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

func TestHandleGetUser_OtherUserWithoutScope_Returns403(t *testing.T) {
	c, _, profiles := newTestControllerWithProfiles(t)
	c.eval = &usersReadEvaluator{}
	seedProfile(profiles, "user-2", "t1", "bob@example.com", "Bob")
	req := httptest.NewRequest(http.MethodGet, "/users/user-2", nil)
	req.SetPathValue("id", "user-2")
	req = req.WithContext(token.WithClaims(req.Context(), humanClaims("t1", "user-1")))
	w := httptest.NewRecorder()
	c.handleGetUser(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleGetUser_EvaluatorFailure_Returns500(t *testing.T) {
	c, _, profiles := newTestControllerWithProfiles(t)
	c.eval = &usersReadEvaluator{err: errors.New("bundle not loaded")}
	seedProfile(profiles, "user-2", "t1", "bob@example.com", "Bob")
	req := httptest.NewRequest(http.MethodGet, "/users/user-2", nil)
	req.SetPathValue("id", "user-2")
	req = req.WithContext(token.WithClaims(req.Context(), humanClaims("t1", "user-1")))
	w := httptest.NewRecorder()
	c.handleGetUser(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
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

func TestHandleListUsers_OverLimit_ClampsAndReflectsAppliedLimit(t *testing.T) {
	c, _, _ := newTestControllerWithProfiles(t)
	claims := humanClaims("t1", "caller")
	w := getWithQuery(t, c.handleListUsers, claims, "limit=500")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	var resp UserProfilesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The requested limit (500) exceeds maxUserProfileLimit (100). The response
	// must echo back what was actually applied, not the raw request value —
	// otherwise a caller has no way to tell it only got one page back.
	if resp.Limit != maxUserProfileLimit {
		t.Errorf("limit: got %d, want %d (the clamped value)", resp.Limit, maxUserProfileLimit)
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

// usersReadEvaluator answers Check with a fixed decision and records the scope asked for.
type usersReadEvaluator struct {
	mockPolicyEvaluator
	allow bool
	err   error
	asked string
}

func (e *usersReadEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	e.asked = in.ResourceType + ":" + strings.Join(in.PathSegments, ":") + ":" + in.Action
	return policy.Result{Allow: e.allow}, e.err
}

func TestAuthorizeUserRead(t *testing.T) {
	system := humanClaims("t1", "orbit-agents")
	system.Kora.PrincipalType = token.PrincipalTypeSystem
	boom := errors.New("bundle not loaded")

	cases := []struct {
		name    string
		claims  *token.PlatformClaims
		userID  string
		eval    *usersReadEvaluator
		wantErr error
		wantAsk bool
	}{
		{"own profile needs no scope", humanClaims("t1", "user-1"), "user-1", &usersReadEvaluator{}, nil, false},
		{"other profile with the scope", humanClaims("t1", "user-1"), "user-2", &usersReadEvaluator{allow: true}, nil, true},
		{"other profile without the scope", humanClaims("t1", "user-1"), "user-2", &usersReadEvaluator{}, ErrUserReadForbidden, true},
		{"evaluator failure fails closed", humanClaims("t1", "user-1"), "user-2", &usersReadEvaluator{err: boom}, boom, true},
		{"system principal", system, "user-2", &usersReadEvaluator{}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Controller{eval: tc.eval}
			err := c.authorizeUserRead(context.Background(), tc.claims, tc.userID)
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("authorizeUserRead = %v, want %v", err, tc.wantErr)
			}
			if tc.wantAsk && tc.eval.asked != UsersReadScope {
				t.Errorf("checked %q, want %q", tc.eval.asked, UsersReadScope)
			}
			if !tc.wantAsk && tc.eval.asked != "" {
				t.Errorf("checked %q, want no policy check", tc.eval.asked)
			}
		})
	}
}

func TestHandleResolveUserNames(t *testing.T) {
	tooMany := strings.TrimSuffix(strings.Repeat("u,", maxUserProfileLimit+1), ",")
	cases := []struct {
		name     string
		claims   *token.PlatformClaims
		query    string
		wantCode int
		wantIDs  []string
	}{
		{"no auth", nil, "ids=user-2", http.StatusUnauthorized, nil},
		{"no ids", humanClaims("t1", "user-1"), "", http.StatusBadRequest, nil},
		{"too many ids", humanClaims("t1", "user-1"), "ids=" + tooMany, http.StatusBadRequest, nil},
		{"names for known ids without the directory scope", humanClaims("t1", "user-1"), "ids=user-2,ghost,other-tenant", http.StatusOK, []string{"user-2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, profiles := newTestControllerWithProfiles(t)
			c.eval = &usersReadEvaluator{}
			seedProfile(profiles, "user-2", "t1", "bob@example.com", "Bob")
			seedProfile(profiles, "other-tenant", "t2", "eve@example.com", "Eve")
			w := getWithQuery(t, c.handleResolveUserNames, tc.claims, tc.query)
			if w.Code != tc.wantCode {
				t.Fatalf("status: got %d, want %d (body: %s)", w.Code, tc.wantCode, w.Body)
			}
			if tc.wantCode != http.StatusOK {
				return
			}
			if strings.Contains(w.Body.String(), "email") {
				t.Errorf("body %s carries email; names lookup returns display names only", w.Body)
			}
			var resp UserNamesResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			var got []string
			for _, u := range resp.Users {
				got = append(got, u.ID)
				if u.DisplayName != "Bob" {
					t.Errorf("display_name = %q, want Bob", u.DisplayName)
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.wantIDs, ",") {
				t.Errorf("ids = %v, want %v", got, tc.wantIDs)
			}
		})
	}
}
