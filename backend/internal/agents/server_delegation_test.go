package agents

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token/tokentest"
)

// checkRecorder allows every Check and records the last input.
type checkRecorder struct {
	policy.Evaluator
	last *policy.CheckInput
}

func (e *checkRecorder) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	e.last = &in
	return policy.Result{Allow: true}, nil
}

// Public routes refuse a revoked delegation and pass a verified one into the
// scope check.
func TestMiddleware_DelegationBlocklist(t *testing.T) {
	const agent = "spiffe://orbit.test/tenant/acme/agent/coder"
	s := tokentest.NewSigner(t)
	bearer := s.Agent(t, agent, "acme")
	cases := []struct {
		name       string
		delegation string
		wantStatus int
		wantPerson string
	}{
		{"delegated", s.Delegation(t, "jti-1", "person-1", agent, "run-1"), http.StatusOK, "person-1"},
		{"autonomous", "", http.StatusOK, ""},
		{"revoked", s.Delegation(t, "jti-revoked", "person-1", agent, "run-1"), http.StatusUnauthorized, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eval := &checkRecorder{}
			routes := []routescopes.RouteScope{{PathPrefix: "/api/v1/runs", Method: "GET", RequiredScope: "agents:runs:read"}}
			c := NewController(nil, nil, nil, nil, s.Verifier, tokentest.Blocklist{"jti-revoked": true}, eval, routes, "", logging.NewLoggerWithOutput(io.Discard))
			h := c.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tokentest.Request(http.MethodGet, "/api/v1/runs", bearer, tc.delegation))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var person string
			if eval.last != nil && eval.last.Delegation != nil {
				person = eval.last.Delegation.Sub
			}
			if eval.last == nil || person != tc.wantPerson {
				t.Errorf("scope check delegator = %q, want %q", person, tc.wantPerson)
			}
		})
	}
}
