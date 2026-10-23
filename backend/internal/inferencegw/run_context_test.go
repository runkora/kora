package inferencegw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func agentClaims(tenantID string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "spiffe://runkora.test/tenant/t1/agent/coder"},
		Kora: token.KoraClaims{
			TenantID:      tenantID,
			PrincipalType: token.PrincipalTypeAgent,
		},
	}
}

// TestRunIDFromContext_ShimBehaviour proves the gateway's internal shim reads
// the run identity that the platform StripRunPrefix middleware stored, with
// the AGENT-principal-only guard intact. The mechanism itself is tested in
// internal/platform/middleware/run_context_test.go.
func TestRunIDFromContext_ShimBehaviour(t *testing.T) {
	t.Parallel()
	var agentRunID, humanRunID string

	hAgent := mw.StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		agentRunID = runIDFromContext(r.Context())
	}))
	reqAgent := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	reqAgent = reqAgent.WithContext(token.WithClaims(reqAgent.Context(), agentClaims("t1")))
	hAgent.ServeHTTP(httptest.NewRecorder(), reqAgent)
	if agentRunID != "run-42" {
		t.Errorf("agent run id = %q, want run-42", agentRunID)
	}

	hHuman := mw.StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		humanRunID = runIDFromContext(r.Context())
	}))
	reqHuman := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	reqHuman = reqHuman.WithContext(token.WithClaims(reqHuman.Context(), testClaims("t1")))
	hHuman.ServeHTTP(httptest.NewRecorder(), reqHuman)
	if humanRunID != "" {
		t.Errorf("human run id = %q, want empty (prefix ignored for non-agents)", humanRunID)
	}
}
