package inferencegw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/golang-jwt/jwt/v5"
)

func agentClaims(tenantID string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "spiffe://orbit.test/tenant/t1/agent/coder"},
		Orbit: token.OrbitClaims{
			TenantID:      tenantID,
			PrincipalType: token.PrincipalTypeAgent,
		},
	}
}

func TestStripRunPrefix_RewritesPathAndStoresRunID(t *testing.T) {
	t.Parallel()
	var gotPath, gotRunID string
	h := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRunID, _ = r.Context().Value(runIDContextKey{}).(string)
	}))

	req := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotRunID != "run-42" {
		t.Errorf("run id = %q, want run-42", gotRunID)
	}
}

func TestStripRunPrefix_NonRunPathUntouched(t *testing.T) {
	t.Parallel()
	var gotPath string
	var hadRunID bool
	h := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, hadRunID = r.Context().Value(runIDContextKey{}).(string)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotPath != "/v1/messages" || hadRunID {
		t.Errorf("path = %q hadRunID = %v, want untouched path and no run id", gotPath, hadRunID)
	}
}

func TestRunIDFromContext_AgentOnly(t *testing.T) {
	t.Parallel()
	var agentRunID, humanRunID string

	hAgent := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		agentRunID = runIDFromContext(r.Context())
	}))
	reqAgent := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	reqAgent = reqAgent.WithContext(token.WithClaims(reqAgent.Context(), agentClaims("t1")))
	hAgent.ServeHTTP(httptest.NewRecorder(), reqAgent)
	if agentRunID != "run-42" {
		t.Errorf("agent run id = %q, want run-42", agentRunID)
	}

	hHuman := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		humanRunID = runIDFromContext(r.Context())
	}))
	reqHuman := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	reqHuman = reqHuman.WithContext(token.WithClaims(reqHuman.Context(), testClaims("t1")))
	hHuman.ServeHTTP(httptest.NewRecorder(), reqHuman)
	if humanRunID != "" {
		t.Errorf("human run id = %q, want empty (prefix ignored for non-agents)", humanRunID)
	}
}
