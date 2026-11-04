package policygw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// The check API refuses a revoked delegation and passes a verified one to policy.
func TestHandleCheck_DelegationBlocklist(t *testing.T) {
	const agent = "spiffe://runkora.test/tenant/acme/agent/coder"
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
			c := NewController(NewService(eval, logging.NewLogger()), s.Verifier, tokentest.Blocklist{"jti-revoked": true}, logging.NewLogger())
			body, _ := json.Marshal(CheckRequest{Token: bearer, DelegationToken: tc.delegation, Resource: CheckResource{Type: "agents", PathSegments: []string{"types"}}, Action: "read"})
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/policy/check", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c.handleCheck(rec, req)

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
				t.Errorf("policy delegator = %q, want %q", person, tc.wantPerson)
			}
		})
	}
}
