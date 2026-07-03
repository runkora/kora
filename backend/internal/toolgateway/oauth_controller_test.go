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

// requestWithHost builds a request as the ingress would present it: the client
// host in X-Forwarded-Host and the scheme in X-Forwarded-Proto.
func requestWithHost(method, path, host string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("X-Forwarded-Host", host)
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}

func TestPRMHandler_DerivesFromRequestHost(t *testing.T) {
	c := NewOAuthMetadataController("/v1", logging.NewLogger())
	rec := httptest.NewRecorder()
	c.handleProtectedResourceMetadata(rec, requestWithHost(http.MethodGet, "/v1/.well-known/oauth-protected-resource", "mcp.example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var m protectedResourceMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Resource != "https://mcp.example.com/v1/mcp" {
		t.Errorf("resource = %q, want derived from host", m.Resource)
	}
	if len(m.AuthorizationServers) != 1 || m.AuthorizationServers[0] != "https://mcp.example.com/api/v1/identity" {
		t.Errorf("authorization_servers = %v, want derived from host", m.AuthorizationServers)
	}
}

// TestChallengeMiddleware confirms a 401 gains the RFC 9728 WWW-Authenticate
// pointer built from the request host, and a 200 does not.
func TestChallengeMiddleware(t *testing.T) {
	mwHandler := mw.MiddlewareChain(challengeMiddleware("/v1"))

	h401 := mwHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	rec := httptest.NewRecorder()
	h401.ServeHTTP(rec, requestWithHost(http.MethodPost, "/v1/mcp", "mcp.example.com"))
	wa := rec.Header().Get("WWW-Authenticate")
	want := "https://mcp.example.com/v1/.well-known/oauth-protected-resource"
	if !strings.Contains(wa, want) || !strings.HasPrefix(wa, "Bearer ") {
		t.Errorf("401 WWW-Authenticate = %q, want it to reference %q", wa, want)
	}

	h200 := mwHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec2 := httptest.NewRecorder()
	h200.ServeHTTP(rec2, requestWithHost(http.MethodPost, "/v1/mcp", "mcp.example.com"))
	if rec2.Header().Get("WWW-Authenticate") != "" {
		t.Errorf("200 should carry no WWW-Authenticate, got %q", rec2.Header().Get("WWW-Authenticate"))
	}
}
