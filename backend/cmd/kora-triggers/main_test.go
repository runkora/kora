package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token/tokentest"
)

// Fire has its own authenticated-only entry, so the triggers:write entry for creation never applies to it.
func TestRouteScopes_Fire(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{"POST", "/api/v1/trigger-fires/3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43", ""},
		{"POST", "/api/v1/triggers", "triggers:write"},
	}
	for _, tc := range tests {
		got, ok := mw.ResolveRequiredScope(routeScopes(), tc.method, tc.path)
		if !ok || got != tc.want {
			t.Errorf("%s %s resolved to %q (matched %v), want %q", tc.method, tc.path, got, ok, tc.want)
		}
	}
	// The published table is the one enforced in-process, so the edge sees fire as authenticated-only too.
	if !slices.Contains(routeScopes(), routescopes.RouteScope{PathPrefix: "/api/v1/trigger-fires", Method: "POST", RequiredScope: ""}) {
		t.Error("the fire route must be published as authenticated-only")
	}
}

// The triggers auth chain puts a verified delegation into context and refuses a
// revoked or forged one.
func TestAuthMiddleware_Delegation(t *testing.T) {
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
		{"forged", "not-a-jwt", http.StatusUnauthorized, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var person string
			auth := authMiddleware(s.Verifier, tokentest.Blocklist{"jti-revoked": true}, logging.NewLogger())
			h := mw.MiddlewareChain(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if d := policy.DelegationFromContext(r.Context()); d != nil {
					person = d.Sub
				}
			}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tokentest.Request(http.MethodGet, "/api/v1/triggers", bearer, tc.delegation))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body)
			}
			if person != tc.wantPerson {
				t.Errorf("delegator = %q, want %q", person, tc.wantPerson)
			}
		})
	}
}
