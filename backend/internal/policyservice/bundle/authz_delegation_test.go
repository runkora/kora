package bundle_test

import (
	"strings"
	"testing"

	jwt "github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/open-policy-agent/opa/v1/rego"
)

// ═══════════════════════════════════════════════════════════════════════════
// Delegation fail-closed marker (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 2): a grant carrying delegation_required is a ceiling, never a
// standing entitlement. Autonomous allow paths refuse the principal; the
// delegated path's token-scope leg is satisfied when the delegation carries no
// scopes (v1 tokens carry none — authority is evaluated live).
// ═══════════════════════════════════════════════════════════════════════════

// delegatedAgentGrant builds an agent grant stamped with the fail-closed marker.
func delegatedAgentGrant(principalID string, scopes ...string) *grants.Grant {
	g := agentGrant(principalID, scopes...)
	g.DelegationRequired = true
	return g
}

// (a) A marked agent with no delegation context is denied everything, even
// though its grant covers the required scope.
func TestAuthz_DelegationRequired_NoDelegation_Denied(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{delegatedAgentGrant(testAgentID, "inference:invoke")},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny: a delegation-required agent must have no standing access")
	}
	if reason != "deny:delegation_required" {
		t.Errorf("expected deny:delegation_required, got %q", reason)
	}
}

// (b) A marked agent with delegation present and an UNSCOPED delegation token
// (v1 tokens carry no scope claim) is allowed when the agent-grant leg and the
// delegator leg are both satisfied — the token-scope leg defers to live evaluation.
func TestAuthz_DelegationRequired_UnscopedDelegation_FullChain_Allowed(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{
			delegatedAgentGrant(testAgentID, "inference:invoke"),
			humanGrant(testDelegateID, "inference:invoke"),
		},
	})

	for _, tc := range []struct {
		name   string
		scopes []string
	}{
		{"empty scope slice", []string{}},
		{"nil scope slice", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delegation := &policy.DelegationClaims{Sub: testDelegateID, DelegatorPrincipalType: "HUMAN", Scopes: tc.scopes}
			allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
			if !allow {
				t.Errorf("expected allow for unscoped delegation with both other legs satisfied, got deny (reason: %s)", reason)
			}
			if !strings.HasPrefix(reason, "delegation:") {
				t.Errorf("expected delegation reason, got %q", reason)
			}
		})
	}
}

// A scoped delegation token still intersects: the token-scope leg only becomes
// unconditional when the delegation carries no scopes.
func TestAuthz_DelegationRequired_ScopedDelegation_StillIntersects(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{
			delegatedAgentGrant(testAgentID, "inference:invoke"),
			humanGrant(testDelegateID, "inference:invoke"),
		},
	})

	delegation := &policy.DelegationClaims{Sub: testDelegateID, DelegatorPrincipalType: "HUMAN", Scopes: []string{"inference:list"}} // wrong scope
	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny: a scoped delegation token must still cover the required scope")
	}
	if reason != "deny:no_delegation_scope" {
		t.Errorf("expected deny:no_delegation_scope, got %q", reason)
	}
}

// Unscoped delegation does not bypass the delegator leg: the delegator must
// still hold the scope live.
func TestAuthz_DelegationRequired_UnscopedDelegation_DelegatorStillRequired(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{delegatedAgentGrant(testAgentID, "inference:invoke")},
	})

	delegation := &policy.DelegationClaims{Sub: testDelegateID, DelegatorPrincipalType: "HUMAN", Scopes: []string{}}
	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny: the delegator leg must hold even for unscoped tokens")
	}
	if reason != "deny:no_delegator_grant" {
		t.Errorf("expected deny:no_delegator_grant, got %q", reason)
	}
}

// Unscoped delegation does not bypass the agent-grant leg either.
func TestAuthz_DelegationRequired_UnscopedDelegation_AgentGrantStillRequired(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{humanGrant(testDelegateID, "inference:invoke")},
	})

	delegation := &policy.DelegationClaims{Sub: testDelegateID, DelegatorPrincipalType: "HUMAN", Scopes: []string{}}
	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny: the agent-grant leg must hold even for unscoped tokens")
	}
	if reason != "deny:no_agent_grant" {
		t.Errorf("expected deny:no_agent_grant, got %q", reason)
	}
}

// (c) An unmarked agent is byte-for-byte unaffected: its autonomous path still
// allows, including when a marked agent's grant coexists in the same bundle.
func TestAuthz_DelegationRequired_UnmarkedAgentUnaffected(t *testing.T) {
	otherAgentID := "spiffe://orbit.test/tenant/test-tenant/agent/other-agent"
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{
			agentGrant(testAgentID, "inference:invoke"),
			delegatedAgentGrant(otherAgentID, "inference:invoke"),
		},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", nil, "invoke")
	if !allow {
		t.Errorf("expected allow for unmarked agent, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "grant:") {
		t.Errorf("expected grant reason, got %q", reason)
	}
}

// A principal holding BOTH a marked and an unmarked grant is treated as
// delegation-required: any marked grant fail-closes the whole principal.
func TestAuthz_DelegationRequired_MixedGrants_FailClosed(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{
			agentGrant(testAgentID, "inference:invoke"),
			delegatedAgentGrant(testAgentID, "inference:list"),
		},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny: any delegation-required grant fail-closes the principal")
	}
	if reason != "deny:delegation_required" {
		t.Errorf("expected deny:delegation_required, got %q", reason)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// (d) Egress: the autonomous egress allow refuses a marked agent (Layer 1
// CONNECT with real JWT verification against a local JWKS server).
// ═══════════════════════════════════════════════════════════════════════════

// evalLayer1Connect evaluates the policy with a Layer-1 (Envoy CheckRequest)
// CONNECT input: the egress posture check reads the :authority host, not a path.
func evalLayer1Connect(t *testing.T, pq rego.PreparedEvalQuery, host string, headers map[string]string) (bool, string) {
	t.Helper()
	if headers == nil {
		headers = map[string]string{}
	}
	input := map[string]any{
		"attributes": map[string]any{
			"request": map[string]any{
				"http": map[string]any{
					"method":  "CONNECT",
					"host":    host,
					"path":    host,
					"headers": headers,
				},
			},
		},
	}
	return evalPolicy(t, pq, input)
}

func TestAuthz_Egress_DelegationRequired_Denied(t *testing.T) {
	jwksSrv := startJWKSServer(t)

	pq := compileAndPrepare(t, bundleOpts{
		jwksURL: jwksSrv.URL + "/jwks",
		grants:  []*grants.Grant{delegatedAgentGrant(testAgentID, "egress:api.github.com:connect")},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)
	allow, reason := evalLayer1Connect(t, pq, "api.github.com:443", map[string]string{
		"authorization": "Bearer " + mainJWT,
	})
	if allow {
		t.Error("expected deny: autonomous egress must refuse a delegation-required agent")
	}
	if reason != "deny:delegation_required" {
		t.Errorf("expected deny:delegation_required, got %q", reason)
	}
}

func TestAuthz_Egress_UnmarkedAgent_StillAllowed(t *testing.T) {
	jwksSrv := startJWKSServer(t)

	pq := compileAndPrepare(t, bundleOpts{
		jwksURL: jwksSrv.URL + "/jwks",
		grants:  []*grants.Grant{agentGrant(testAgentID, "egress:api.github.com:connect")},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)
	allow, reason := evalLayer1Connect(t, pq, "api.github.com:443", map[string]string{
		"authorization": "Bearer " + mainJWT,
	})
	if !allow {
		t.Errorf("expected allow for unmarked agent egress, got deny (reason: %s)", reason)
	}
	if reason != "egress:api.github.com" {
		t.Errorf("expected egress:api.github.com reason, got %q", reason)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Gateway mirrors (authz_gateway.rego)
// ═══════════════════════════════════════════════════════════════════════════

func TestGateway_DelegationRequired_NoDelegation_Denied(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/marked-agent"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}, DelegationRequired: true},
			},
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization": "Bearer " + mainJWT,
	})
	if allow {
		t.Error("expected deny at the gateway for a delegation-required agent without delegation")
	}
	if reason != "deny:delegation_required" {
		t.Errorf("expected deny:delegation_required, got %q", reason)
	}
}

func TestGateway_DelegationRequired_UnscopedDelegation_FullChain_Allowed(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/marked-agent-2"
	delegatorID := "user-delegator-unscoped"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}, DelegationRequired: true},
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeHuman, PrincipalID: delegatorID, Scopes: []string{"inference:invoke"}},
			},
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
	// v1 delegation tokens carry no scope claim; the empty scope string is
	// treated as "no scopes" and the token-scope leg defers to live evaluation.
	delegationToken := signDelegationJWT(t, delegatorID, nil)

	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization":            "Bearer " + mainJWT,
		"x-orbit-delegation-token": delegationToken,
	})
	if !allow {
		t.Errorf("expected allow for unscoped delegation with both other legs satisfied, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "delegation:") {
		t.Errorf("expected delegation reason, got %q", reason)
	}
}

func TestGateway_DelegationRequired_UnmarkedAgentUnaffected(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/plain-agent"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}},
			},
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization": "Bearer " + mainJWT,
	})
	if !allow {
		t.Errorf("expected allow for unmarked agent at the gateway, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "grant:") {
		t.Errorf("expected grant reason, got %q", reason)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Delegation run binding (Decision 4): a delegation token is bound to one run;
// where the request independently carries run identity (the /run/{run_id}
// path prefix, or the platform token's run_id claim in per-run sandbox pods),
// a token bound to a different run is refused at Layer 1. The Go engine
// applies the identical checks for Layer 2 (see platform/policy tests).
// ═══════════════════════════════════════════════════════════════════════════

// signDelegationJWTWithRunID signs a delegation token bound to a specific run.
func signDelegationJWTWithRunID(t *testing.T, delegatorID, runID string) string {
	t.Helper()
	return signDelegationClaims(t, jwt.MapClaims{
		"sub":                      delegatorID,
		"scope":                    "",
		"run_id":                   runID,
		"delegator_principal_type": "HUMAN",
	})
}

// delegationRunBindingBundle compiles a bundle where the full delegated chain
// (marked agent grant + delegator grant) is satisfied, so any deny in these
// tests is attributable to the run binding alone.
func delegationRunBindingBundle(t *testing.T, jwksURL string) rego.PreparedEvalQuery {
	t.Helper()
	return compileAndPrepare(t, bundleOpts{
		jwksURL: jwksURL,
		routes: []routes.RouteScope{
			{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: []*grants.Grant{
			delegatedAgentGrant(testAgentID, "inference:invoke"),
			humanGrant(testDelegateID, "inference:invoke"),
		},
	})
}

func TestAuthz_Layer1_DelegationRunBinding_PathMode(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	pq := delegationRunBindingBundle(t, jwksSrv.URL+"/jwks")
	mainJWT := signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)
	delegationToken := signDelegationJWTWithRunID(t, testDelegateID, "run-a")

	tests := []struct {
		name       string
		path       string
		wantAllow  bool
		wantReason string
	}{
		{"matching run prefix allowed", "/run/run-a/v1/messages", true, "delegation:inference:invoke"},
		{"foreign run prefix denied", "/run/run-b/v1/messages", false, "deny:delegation_run_mismatch"},
		{"no run prefix — binding not checkable at Layer 1", "/v1/messages", true, "delegation:inference:invoke"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			allow, reason := evalLayer1(t, pq, "POST", tc.path, map[string]string{
				"authorization":            "Bearer " + mainJWT,
				"x-orbit-delegation-token": delegationToken,
			})
			if allow != tc.wantAllow {
				t.Errorf("allow = %v, want %v (reason: %s)", allow, tc.wantAllow, reason)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestAuthz_Layer1_DelegationRunBinding_SandboxMode(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	pq := delegationRunBindingBundle(t, jwksSrv.URL+"/jwks")
	delegationToken := signDelegationJWTWithRunID(t, testDelegateID, "run-a")

	tests := []struct {
		name       string
		tokenRunID string
		wantAllow  bool
		wantReason string
	}{
		{"matching sandbox run allowed", "run-a", true, "delegation:inference:invoke"},
		{"foreign sandbox run denied", "run-b", false, "deny:delegation_run_mismatch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The platform token carries the per-run sandbox SVID's run_id claim.
			mainJWT := signTestJWT(t, "AGENT", testTenantID, testAgentID, map[string]any{
				"claims": map[string]any{"run_id": tc.tokenRunID},
			})
			allow, reason := evalLayer1(t, pq, "POST", "/v1/messages", map[string]string{
				"authorization":            "Bearer " + mainJWT,
				"x-orbit-delegation-token": delegationToken,
			})
			if allow != tc.wantAllow {
				t.Errorf("allow = %v, want %v (reason: %s)", allow, tc.wantAllow, reason)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

// A delegation token carrying NO run_id claim is bound to no run, and is
// refused everywhere — including on requests where run identity is not
// independently knowable, the one case a well-formed token is honoured on.
//
// Before the fix this was the inverse: an absent run_id left
// _delegation_run_id undefined, `undefined != ""` is undefined rather than
// true, so both mismatch rules went undefined, the aggregate went undefined
// (falsy), and every `not _delegation_binding_failed` guard passed. A token
// with no run binding was accepted on every run.
func TestAuthz_Layer1_DelegationRunBinding_UnboundTokenDenied(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	pq := delegationRunBindingBundle(t, jwksSrv.URL+"/jwks")
	unbound := signDelegationJWTWithoutRunID(t, testDelegateID)

	tests := []struct {
		name         string
		path         string
		tokenRunID   string // platform-token run_id claim (sandbox mode)
		delegation   string
		wantAllow    bool
		wantedReason string
	}{
		{"unbound token, no run identity anywhere", "/v1/messages", "", unbound, false, "deny:delegation_run_unbound"},
		{"unbound token, path asserts a run", "/run/run-a/v1/messages", "", unbound, false, "deny:delegation_run_unbound"},
		{"unbound token, sandbox token attests a run", "/v1/messages", "run-a", unbound, false, "deny:delegation_run_unbound"},
		// Contrast: a well-formed token on a request with no independently
		// known run identity is still honoured — the binding is simply not
		// checkable there, which is a different condition entirely.
		{"bound token, no run identity anywhere", "/v1/messages", "", signDelegationJWTWithRunID(t, testDelegateID, "run-a"), true, "delegation:inference:invoke"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orbitExtra := map[string]any{}
			if tc.tokenRunID != "" {
				orbitExtra["claims"] = map[string]any{"run_id": tc.tokenRunID}
			}
			mainJWT := signTestJWT(t, "AGENT", testTenantID, testAgentID, orbitExtra)
			allow, reason := evalLayer1(t, pq, "POST", tc.path, map[string]string{
				"authorization":            "Bearer " + mainJWT,
				"x-orbit-delegation-token": tc.delegation,
			})
			if allow != tc.wantAllow {
				t.Errorf("allow = %v, want %v (reason: %s)", allow, tc.wantAllow, reason)
			}
			if reason != tc.wantedReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantedReason)
			}
		})
	}
}

// The unbound token must not reach the delegated EGRESS rule either: that rule
// carries the same guard, and CONNECT never carries a run prefix, so it was
// the widest of the bypassed paths.
func TestAuthz_Layer1_DelegationRunBinding_UnboundTokenDeniedOnEgress(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	pq := compileAndPrepare(t, bundleOpts{
		jwksURL: jwksSrv.URL + "/jwks",
		grants:  []*grants.Grant{delegatedAgentGrant(testAgentID, "egress:api.github.com:connect")},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)
	allow, reason := evalLayer1Connect(t, pq, "api.github.com:443", map[string]string{
		"authorization":            "Bearer " + mainJWT,
		"x-orbit-delegation-token": signDelegationJWTWithoutRunID(t, testDelegateID),
	})
	if allow {
		t.Error("expected deny: an unbound delegation must not satisfy the delegated egress rule")
	}
	if reason != "deny:delegation_run_unbound" {
		t.Errorf("reason = %q, want deny:delegation_run_unbound", reason)
	}
}

func TestGateway_DelegationRunBinding_UnboundTokenDenied(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/unbound-agent"
	delegatorID := "user-delegator-unbound"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
			{PathPrefix: "/run/", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}, DelegationRequired: true},
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeHuman, PrincipalID: delegatorID, Scopes: []string{"inference:invoke"}},
			},
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
	unbound := signDelegationJWTWithoutRunID(t, delegatorID)

	for _, path := range []string{"/v1/chat/completions", "/run/run-a/v1/chat/completions"} {
		t.Run(path, func(t *testing.T) {
			allow, reason := evalGateway(t, pq, "POST", path, map[string]string{
				"authorization":            "Bearer " + mainJWT,
				"x-orbit-delegation-token": unbound,
			})
			if allow {
				t.Error("expected deny at the gateway for a delegation bound to no run")
			}
			if reason != "deny:delegation_run_unbound" {
				t.Errorf("reason = %q, want deny:delegation_run_unbound", reason)
			}
		})
	}
}

func TestGateway_DelegationRunBinding_PathMismatchDenied(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/bound-agent"
	delegatorID := "user-delegator-bound"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/run/", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}, DelegationRequired: true},
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeHuman, PrincipalID: delegatorID, Scopes: []string{"inference:invoke"}},
			},
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
	delegationToken := signDelegationJWTWithRunID(t, delegatorID, "run-a")

	allow, reason := evalGateway(t, pq, "POST", "/run/run-b/v1/chat/completions", map[string]string{
		"authorization":            "Bearer " + mainJWT,
		"x-orbit-delegation-token": delegationToken,
	})
	if allow {
		t.Error("expected deny at the gateway for a delegation bound to a different run")
	}
	if reason != "deny:delegation_run_mismatch" {
		t.Errorf("reason = %q, want deny:delegation_run_mismatch", reason)
	}
}
