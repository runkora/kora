package agents

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/token"
)

func traceParentReq(runID string, claims *token.PlatformClaims) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/internal/runs/"+runID+"/trace-parent", http.NoBody)
	req.SetPathValue("id", runID)
	if claims == nil {
		return req
	}
	return req.WithContext(token.WithClaims(req.Context(), claims))
}

func principalClaims(typ token.PrincipalType, name string) *token.PlatformClaims {
	return &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: typ, DisplayName: name, TenantID: "t-1"}}
}

// Only the egress gateway may read a run's trace context, and a missing run is a 404.
func TestHandleGetRunTraceParent(t *testing.T) {
	const traceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive, TraceParent: traceParent})
	seedRun(t, repo, Run{ID: "run-2", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	egress := principalClaims(token.PrincipalTypeSystem, "egress-gateway")
	cases := []struct {
		name   string
		runID  string
		claims *token.PlatformClaims
		status int
		body   string
	}{
		{"egress gateway reads the traceparent", "run-1", egress, http.StatusOK, traceParent},
		{"run without a traceparent reads empty", "run-2", egress, http.StatusOK, `"trace_parent":""`},
		{"unknown run", "run-9", egress, http.StatusNotFound, ""},
		{"other system component", "run-1", principalClaims(token.PrincipalTypeSystem, "inference-gateway"), http.StatusForbidden, ""},
		{"agent principal", "run-1", principalClaims(token.PrincipalTypeAgent, "egress-gateway"), http.StatusForbidden, ""},
		{"no claims", "run-1", nil, http.StatusForbidden, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.handleGetRunTraceParent(w, traceParentReq(tc.runID, tc.claims))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.status, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.body) {
				t.Errorf("body = %s, want it to contain %q", w.Body.String(), tc.body)
			}
			if tc.status != http.StatusOK && strings.Contains(w.Body.String(), "4bf92f35") {
				t.Errorf("a refused read leaked the traceparent: %s", w.Body.String())
			}
		})
	}
}
