package identity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/clear-route/orbit/internal/platform/token"
)

// ── handleGetPreferences ──────────────────────────────────────────────────────

func TestHandleGetPreferences_NoAuth_Returns401(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	req := httptest.NewRequest(http.MethodGet, "/preferences", nil)
	w := httptest.NewRecorder()
	c.handleGetPreferences(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetPreferences_NoRow_ReturnsEmptyDoc(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")
	req := httptest.NewRequest(http.MethodGet, "/preferences", nil)
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleGetPreferences(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty document, got %v", got)
	}
}

func TestHandleGetPreferences_ExistingRow_ReturnsDoc(t *testing.T) {
	c, prefs := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")
	stored := json.RawMessage(`{"analytics":{"defaultPeriod":"30d"}}`)
	if err := prefs.Upsert(t.Context(), claims.Subject, claims.Orbit.TenantID, stored); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/preferences", nil)
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleGetPreferences(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Errorf("response body is not valid JSON: %s", w.Body.String())
	}
}

// ── handlePutPreferences ──────────────────────────────────────────────────────

func TestHandlePutPreferences_NoAuth_Returns401(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	req := httptest.NewRequest(http.MethodPut, "/preferences", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	c.handlePutPreferences(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandlePutPreferences_InvalidJSON_Returns400(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")
	req := httptest.NewRequest(http.MethodPut, "/preferences", strings.NewReader(`not-json`))
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handlePutPreferences(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlePutPreferences_ValidDoc_Returns204(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")
	body := `{"analytics":{"defaultPeriod":"7d"}}`
	req := httptest.NewRequest(http.MethodPut, "/preferences", strings.NewReader(body))
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handlePutPreferences(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandlePutPreferences_DocTooLarge_Returns400(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")
	// Build a document that exceeds 64 KB.
	// Wrap in valid JSON: {"x":"<huge string>"}
	payload := `{"x":"` + strings.Repeat("a", maxPreferencesDocBytes) + `"}`
	req := httptest.NewRequest(http.MethodPut, "/preferences", bytes.NewReader([]byte(payload)))
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handlePutPreferences(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlePutPreferences_BodyExceedsHTTPLimit_Returns400(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")
	// Exceed maxPreferencesBodyBytes (maxPreferencesDocBytes + 1024) to trigger
	// http.MaxBytesReader before the JSON validator runs.
	payload := strings.Repeat("a", int(maxPreferencesBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPut, "/preferences", strings.NewReader(payload))
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handlePutPreferences(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlePutPreferences_ThenGet_ReturnsSameDoc(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")
	body := `{"analytics":{"defaultPeriod":"30d"}}`

	putReq := httptest.NewRequest(http.MethodPut, "/preferences", strings.NewReader(body))
	putReq = putReq.WithContext(token.WithClaims(putReq.Context(), claims))
	putW := httptest.NewRecorder()
	c.handlePutPreferences(putW, putReq)
	if putW.Code != http.StatusNoContent {
		t.Fatalf("PUT status: got %d, want %d", putW.Code, http.StatusNoContent)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/preferences", nil)
	getReq = getReq.WithContext(token.WithClaims(getReq.Context(), claims))
	getW := httptest.NewRecorder()
	c.handleGetPreferences(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET status: got %d, want %d", getW.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(getW.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}
	analytics, ok := got["analytics"].(map[string]any)
	if !ok {
		t.Fatalf("missing analytics key in response")
	}
	if analytics["defaultPeriod"] != "30d" {
		t.Errorf("analytics.defaultPeriod: got %v, want 30d", analytics["defaultPeriod"])
	}
}

// ── handleDeletePreferences ───────────────────────────────────────────────────

func TestHandleDeletePreferences_NoAuth_Returns401(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	req := httptest.NewRequest(http.MethodDelete, "/preferences", nil)
	w := httptest.NewRecorder()
	c.handleDeletePreferences(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleDeletePreferences_Returns204(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")
	req := httptest.NewRequest(http.MethodDelete, "/preferences", nil)
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleDeletePreferences(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleDeletePreferences_AfterPut_GetReturnsEmpty(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")

	// Write a document.
	putReq := httptest.NewRequest(http.MethodPut, "/preferences", strings.NewReader(`{"analytics":{"defaultPeriod":"90d"}}`))
	putReq = putReq.WithContext(token.WithClaims(putReq.Context(), claims))
	c.handlePutPreferences(httptest.NewRecorder(), putReq)

	// Delete it.
	delReq := httptest.NewRequest(http.MethodDelete, "/preferences", nil)
	delReq = delReq.WithContext(token.WithClaims(delReq.Context(), claims))
	delW := httptest.NewRecorder()
	c.handleDeletePreferences(delW, delReq)
	if delW.Code != http.StatusNoContent {
		t.Fatalf("DELETE status: got %d, want %d", delW.Code, http.StatusNoContent)
	}

	// GET should now return {}.
	getReq := httptest.NewRequest(http.MethodGet, "/preferences", nil)
	getReq = getReq.WithContext(token.WithClaims(getReq.Context(), claims))
	getW := httptest.NewRecorder()
	c.handleGetPreferences(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET status: got %d, want %d", getW.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(getW.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty document after delete, got %v", got)
	}
}

// ── Concurrent first-write race ───────────────────────────────────────────────

func TestHandlePreferences_ConcurrentFirstWrite_NoDataRace(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claims := humanClaims("t1", "user-1")

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			body := `{"analytics":{"defaultPeriod":"30d"}}`
			req := httptest.NewRequest(http.MethodPut, "/preferences", strings.NewReader(body))
			req = req.WithContext(token.WithClaims(req.Context(), claims))
			c.handlePutPreferences(httptest.NewRecorder(), req)
		}(i)
	}
	wg.Wait()

	// Final state must be valid — exactly one writer wins each round.
	getReq := httptest.NewRequest(http.MethodGet, "/preferences", nil)
	getReq = getReq.WithContext(token.WithClaims(getReq.Context(), claims))
	getW := httptest.NewRecorder()
	c.handleGetPreferences(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET status: got %d, want %d", getW.Code, http.StatusOK)
	}
	if !json.Valid(getW.Body.Bytes()) {
		t.Errorf("stored document is not valid JSON after concurrent writes: %s", getW.Body.String())
	}
}

// ── Cross-tenant isolation ────────────────────────────────────────────────────

func TestHandlePreferences_CrossTenantIsolation(t *testing.T) {
	c, _ := newTestControllerWithPrefs(t)
	claimsT1 := humanClaims("t1", "user-1")
	claimsT2 := humanClaims("t2", "user-1")

	// Write preference for tenant t1.
	putReq := httptest.NewRequest(http.MethodPut, "/preferences", strings.NewReader(`{"analytics":{"defaultPeriod":"30d"}}`))
	putReq = putReq.WithContext(token.WithClaims(putReq.Context(), claimsT1))
	c.handlePutPreferences(httptest.NewRecorder(), putReq)

	// GET from tenant t2 — must return empty.
	getReq := httptest.NewRequest(http.MethodGet, "/preferences", nil)
	getReq = getReq.WithContext(token.WithClaims(getReq.Context(), claimsT2))
	getW := httptest.NewRecorder()
	c.handleGetPreferences(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET (t2) status: got %d, want %d", getW.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(getW.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("cross-tenant leakage: t2 saw t1's preferences")
	}
}
