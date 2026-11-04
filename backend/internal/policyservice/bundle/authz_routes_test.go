package bundle_test

import (
	"testing"

	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
)

// routeMatchGlobal mirrors the shape of real route tables: an authenticated-only
// route next to a longer scoped prefix, and the reverse.
var routeMatchGlobal = []routes.RouteScope{
	{PathPrefix: "/api/v1/runs", Method: "POST", AuthenticatedOnly: true},
	{PathPrefix: "/api/v1/runs/", Method: "POST", RequiredScope: "agents:runs:write"},
	{PathPrefix: "/v1/analytics", Method: "GET", RequiredScope: "platform:analytics:read"},
	{PathPrefix: "/v1/analytics/me", Method: "GET", AuthenticatedOnly: true},
	{PathPrefix: "/v1/usage", Method: "GET", AuthenticatedOnly: true},
	{PathPrefix: "/v1/usage/agent-type", Method: "GET", RequiredScope: "agents:usage:read"},
	{PathPrefix: "/v1/admin", Method: "*", RequiredScope: "platform:admin"},
	{PathPrefix: "/v1/admin/me", Method: "GET", AuthenticatedOnly: true},
	{PathPrefix: "/v1/conflict", Method: "GET", RequiredScope: "inference:list"},
	{PathPrefix: "/v1/conflict", Method: "GET", AuthenticatedOnly: true},
	// A scope with the marker set is still a scoped route.
	{PathPrefix: "/v1/marked", Method: "GET", RequiredScope: "platform:analytics:read", AuthenticatedOnly: true},
}

// routeMatchTenant tries to shadow a global route and adds one of its own.
var routeMatchTenant = []routes.RouteScope{
	{PathPrefix: "/api/v1/runs/shadow", Method: "POST", AuthenticatedOnly: true},
	{PathPrefix: "/v1/thing", Method: "GET", AuthenticatedOnly: true},
}

type routeMatchCase struct {
	name       string
	method     string
	path       string
	delegation string
	wantAllow  bool
	wantReason string
}

// routeMatchCases run against both rego layers with an agent that holds only
// inference:list.
func routeMatchCases(t *testing.T) []routeMatchCase {
	t.Helper()
	return []routeMatchCase{
		{"authenticated route", "POST", "/api/v1/runs", "", true, "allow:authenticated"},
		{"longer scoped prefix wins", "POST", "/api/v1/runs/r1/messages", "", false, "deny:no_matching_grant"},
		{"longer authenticated prefix wins", "GET", "/v1/analytics/me", "", true, "allow:authenticated"},
		{"query string ignored", "GET", "/v1/analytics/me?days=7", "", true, "allow:authenticated"},
		{"scoped parent still scoped", "GET", "/v1/analytics/summary", "", false, "deny:no_matching_grant"},
		{"scoped child under authenticated parent", "GET", "/v1/usage/agent-type/coder", "", false, "deny:no_matching_grant"},
		{"authenticated parent", "GET", "/v1/usage", "", true, "allow:authenticated"},
		{"exact method beats longer wildcard-free prefix", "GET", "/v1/admin/me", "", true, "allow:authenticated"},
		{"wildcard applies to other methods", "POST", "/v1/admin/me", "", false, "deny:no_matching_grant"},
		{"unregistered path", "GET", "/api/v1/nonexistent", "", false, "deny:no_route_match"},
		{"prefix matches on segment boundary", "POST", "/api/v1/runsfoo", "", false, "deny:no_route_match"},
		{"equal prefixes that disagree", "GET", "/v1/conflict", "", false, "deny:no_route_match"},
		{"marker with a scope stays scoped", "GET", "/v1/marked", "", false, "deny:no_matching_grant"},
		{"tenant cannot shadow a global route", "POST", "/api/v1/runs/shadow", "", false, "deny:no_matching_grant"},
		{"tenant authenticated route", "GET", "/v1/thing", "", true, "allow:authenticated"},
		{"delegated request on authenticated route", "POST", "/api/v1/runs", signDelegationJWT(t, testDelegateID, nil), true, "allow:authenticated"},
		{"unbound delegation on authenticated route", "POST", "/api/v1/runs", signDelegationJWTWithoutRunID(t, testDelegateID), false, "deny:delegation_run_unbound"},
	}
}

func TestGateway_RouteMatching(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:      jwksSrv.URL + "/jwks",
		tenantIDs:    []string{testTenantID},
		grants:       map[string][]*grants.Grant{testTenantID: {agentGrant(testAgentID, "inference:list")}},
		globalRoutes: routeMatchGlobal,
		tenantRoutes: map[string][]routes.RouteScope{testTenantID: routeMatchTenant},
	})
	bearer := "Bearer " + signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)

	for _, tc := range routeMatchCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{":authority": "api.example.com", "authorization": bearer}
			if tc.delegation != "" {
				headers["x-kora-delegation-token"] = tc.delegation
			}
			allow, reason := evalGateway(t, pq, tc.method, tc.path, headers)
			if allow != tc.wantAllow || reason != tc.wantReason {
				t.Errorf("got allow=%v reason=%q, want allow=%v reason=%q", allow, reason, tc.wantAllow, tc.wantReason)
			}
		})
	}
}

func TestAuthz_Layer1_RouteMatching(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	pq := compileAndPrepare(t, bundleOpts{
		jwksURL:      jwksSrv.URL + "/jwks",
		grants:       []*grants.Grant{agentGrant(testAgentID, "inference:list")},
		routes:       routeMatchGlobal,
		tenantRoutes: routeMatchTenant,
	})
	bearer := "Bearer " + signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)

	for _, tc := range routeMatchCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{"authorization": bearer}
			if tc.delegation != "" {
				headers["x-kora-delegation-token"] = tc.delegation
			}
			allow, reason := evalLayer1(t, pq, tc.method, tc.path, headers)
			if allow != tc.wantAllow || reason != tc.wantReason {
				t.Errorf("got allow=%v reason=%q, want allow=%v reason=%q", allow, reason, tc.wantAllow, tc.wantReason)
			}
		})
	}
}

// TestAuthenticatedRoute_DelegationRequired keeps the fail-closed marker on
// authenticated-only routes: a delegated agent without a delegation is denied.
func TestAuthenticatedRoute_DelegationRequired(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	grant := delegatedAgentGrant(testAgentID, "inference:list")
	bearer := "Bearer " + signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)

	gw := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:      jwksSrv.URL + "/jwks",
		tenantIDs:    []string{testTenantID},
		grants:       map[string][]*grants.Grant{testTenantID: {grant}},
		globalRoutes: routeMatchGlobal,
	})
	sidecar := compileAndPrepare(t, bundleOpts{
		jwksURL: jwksSrv.URL + "/jwks",
		grants:  []*grants.Grant{grant},
		routes:  routeMatchGlobal,
	})

	for name, eval := range map[string]func() (bool, string){
		"gateway": func() (bool, string) {
			return evalGateway(t, gw, "POST", "/api/v1/runs", map[string]string{":authority": "api.example.com", "authorization": bearer})
		},
		"sidecar": func() (bool, string) {
			return evalLayer1(t, sidecar, "POST", "/api/v1/runs", map[string]string{"authorization": bearer})
		},
	} {
		t.Run(name, func(t *testing.T) {
			allow, reason := eval()
			if allow || reason != "deny:delegation_required" {
				t.Errorf("got allow=%v reason=%q, want deny:delegation_required", allow, reason)
			}
		})
	}
}

// TestAuthz_Layer1_AuthenticatedRouteNeverAuthorizesConnect: egress is decided
// by the agent's egress grants, never by an authenticated-only route.
func TestAuthz_Layer1_AuthenticatedRouteNeverAuthorizesConnect(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	pq := compileAndPrepare(t, bundleOpts{
		jwksURL: jwksSrv.URL + "/jwks",
		grants:  []*grants.Grant{agentGrant(testAgentID, "inference:list")},
		routes:  []routes.RouteScope{{PathPrefix: "evil.example.com:443", Method: "*", AuthenticatedOnly: true}},
	})
	bearer := "Bearer " + signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)

	allow, reason := evalLayer1Connect(t, pq, "evil.example.com:443", map[string]string{"authorization": bearer})
	if allow || reason == "allow:authenticated" {
		t.Errorf("got allow=%v reason=%q, want CONNECT denied", allow, reason)
	}
}
