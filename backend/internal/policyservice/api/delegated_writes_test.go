package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// TestPolicyWritesRefuseDelegatedRuns locks that a delegated run can make no
// policy write. The controller has no service: a refused request never reaches it.
func TestPolicyWritesRefuseDelegatedRuns(t *testing.T) {
	c := &Controller{logger: logging.NewLoggerWithOutput(io.Discard)}
	cases := []struct {
		route   string
		handler http.HandlerFunc
		path    map[string]string
		body    string
	}{
		{"POST /grants", c.handleCreateGrant, nil, `{"principal_type":"AGENT","principal_id":"a","scopes":["inference:invoke"]}`},
		{"PATCH /grants/{id}", c.handleUpdateGrantBudget, map[string]string{"id": "g1"}, `{"budget":null}`},
		{"DELETE /grants/{id}", c.handleDeleteGrant, map[string]string{"id": "g1"}, ``},
		{"PUT /tenant-budget", c.handleUpsertTenantBudget, nil, `{"limit_usd":10,"reset_cadence":"monthly"}`},
		{"DELETE /tenant-budget", c.handleDeleteTenantBudget, nil, ``},
		{"PUT /roles", c.handleUpsertRole, nil, `{"role":"engineer","scopes":["inference:invoke"]}`},
		{"DELETE /roles/{role}", c.handleDeleteRole, map[string]string{"role": "engineer"}, ``},
		{"PATCH /roles/{role}/budget", c.handleUpdateRoleBudget, map[string]string{"role": "engineer"}, `{"budget":null}`},
		{"POST /routes/register", c.handleRegisterRoutes, nil, `{"service":"x","scopes":[{"scope":"x:y:read"}]}`},
	}
	human := &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, TenantID: "t1"}}
	for _, tc := range cases {
		t.Run(tc.route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(tc.body))
			for k, v := range tc.path {
				req.SetPathValue(k, v)
			}
			ctx := policy.WithDelegation(token.WithClaims(req.Context(), human), &policy.DelegationClaims{})
			w := httptest.NewRecorder()
			tc.handler(w, req.WithContext(ctx))
			if w.Code != http.StatusForbidden {
				t.Errorf("%s under delegation: status %d, want 403 (body %s)", tc.route, w.Code, w.Body)
			}
		})
	}
}
