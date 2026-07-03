package toolgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

func TestProtectedResourceMetadataURL(t *testing.T) {
	got := ProtectedResourceMetadataURL("https://gw.orbit.sh/v1/mcp")
	want := "https://gw.orbit.sh/v1/.well-known/oauth-protected-resource"
	if got != want {
		t.Errorf("ProtectedResourceMetadataURL = %q, want %q", got, want)
	}
}

func TestPRMHandler(t *testing.T) {
	c := NewOAuthMetadataController("https://gw.orbit.sh/v1/mcp", "https://id.orbit.sh/api/v1/identity", logging.NewLogger())
	rec := httptest.NewRecorder()
	c.handleProtectedResourceMetadata(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var m protectedResourceMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Resource != "https://gw.orbit.sh/v1/mcp" {
		t.Errorf("resource = %q", m.Resource)
	}
	if len(m.AuthorizationServers) != 1 || m.AuthorizationServers[0] != "https://id.orbit.sh/api/v1/identity" {
		t.Errorf("authorization_servers = %v", m.AuthorizationServers)
	}
}

// TestChallengeMiddleware confirms a 401 from the wrapped handler gains the RFC
// 9728 WWW-Authenticate pointer, and a 200 does not.
func TestChallengeMiddleware(t *testing.T) {
	prmURL := "https://gw.orbit.sh/v1/.well-known/oauth-protected-resource"
	mwHandler := mw.MiddlewareChain(challengeMiddleware(prmURL))

	// 401 case
	h401 := mwHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	rec := httptest.NewRecorder()
	h401.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcp", nil))
	wa := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(wa, prmURL) || !strings.HasPrefix(wa, "Bearer ") {
		t.Errorf("401 WWW-Authenticate = %q, want it to reference the PRM URL", wa)
	}

	// 200 case — no challenge
	h200 := mwHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec2 := httptest.NewRecorder()
	h200.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/v1/mcp", nil))
	if rec2.Header().Get("WWW-Authenticate") != "" {
		t.Errorf("200 should carry no WWW-Authenticate, got %q", rec2.Header().Get("WWW-Authenticate"))
	}
}
