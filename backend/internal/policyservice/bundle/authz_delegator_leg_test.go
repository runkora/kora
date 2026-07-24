package bundle_test

import (
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// ═══════════════════════════════════════════════════════════════════════════
// Delegator leg (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 5):
// the delegator's side of the intersection is the person's FULL held access —
// direct grants ∪ role-derived scopes (membership from the bundled
// user_profiles snapshot) — plus the vocabulary `requires` projection for
// scopes people are never granted (MCP tool scopes).
// ═══════════════════════════════════════════════════════════════════════════

// humanDelegation returns the delegation claims a v1 token carries: no scopes,
// delegator_principal_type always HUMAN.
func humanDelegation(delegatorID string) *policy.DelegationClaims {
	return &policy.DelegationClaims{Sub: delegatorID, DelegatorPrincipalType: "HUMAN"}
}

// (2) Role-derived access: a delegator whose ONLY access comes through a role
// (no direct grant) satisfies the delegator leg — membership resolved from the
// bundled principal_roles snapshot, scopes from the role→scope map.
func TestAuthz_DelegatorLeg_RoleDerivedAllow(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{delegatedAgentGrant(testAgentID, "inference:invoke")},
		roles: []*roles.RoleEntry{
			{TenantID: testTenantID, Role: "analyst", Scopes: []string{"inference:invoke"}},
		},
		principalRoles: map[string][]string{testDelegateID: {"analyst"}},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), humanDelegation(testDelegateID), "inference", nil, "invoke")
	if !allow {
		t.Errorf("expected allow via the delegator's role-derived access, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "delegation:") {
		t.Errorf("expected delegation reason, got %q", reason)
	}
}

// A reserved role held by the delegator (e.g. tenant-admin) also counts:
// role-provisioned admins are not bounded to nothing under delegation.
func TestAuthz_DelegatorLeg_ReservedRoleDerivedAllow(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants:         []*grants.Grant{delegatedAgentGrant(testAgentID, "agents:runs:read")},
		principalRoles: map[string][]string{testDelegateID: {"tenant-admin"}},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), humanDelegation(testDelegateID), "agents", []string{"runs"}, "read")
	if !allow {
		t.Errorf("expected allow via the delegator's tenant-admin role, got deny (reason: %s)", reason)
	}
}

// A delegator with neither grants nor role membership stays denied — the
// principal_roles snapshot of OTHER users must not leak access.
func TestAuthz_DelegatorLeg_RoleMembershipIsPerUser(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{delegatedAgentGrant(testAgentID, "inference:invoke")},
		roles: []*roles.RoleEntry{
			{TenantID: testTenantID, Role: "analyst", Scopes: []string{"inference:invoke"}},
		},
		principalRoles: map[string][]string{"some-other-user": {"analyst"}},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), humanDelegation(testDelegateID), "inference", nil, "invoke")
	if allow {
		t.Error("expected deny: another user's role membership must not satisfy the delegator leg")
	}
	if reason != "deny:no_delegator_grant" {
		t.Errorf("expected deny:no_delegator_grant, got %q", reason)
	}
}

// (3) Requires projection: an MCP tool scope — which people are never granted —
// is satisfied on the delegator leg when the person covers every scope in the
// tool's vocabulary requires expansion (the upstream authority the tool
// actually exercises).
func TestAuthz_DelegatorLeg_RequiresProjection(t *testing.T) {
	vocab := []*vocabulary.ScopeEntry{
		{
			Scope:        "mcp:github:create_issue:use",
			ResourceType: "mcp",
			PathSegments: []string{"github", "create_issue"},
			Action:       "use",
			Requires:     []string{"connection:github:issues:write"},
		},
	}

	tests := []struct {
		name            string
		delegatorGrants []string
		delegatorRoles  map[string][]string
		roleEntries     []*roles.RoleEntry
		wantAllow       bool
		wantReason      string
	}{
		{
			name:            "person holds the connection scope directly",
			delegatorGrants: []string{"connection:github:issues:write"},
			wantAllow:       true,
			wantReason:      "delegation:mcp:github:create_issue:use",
		},
		{
			name:            "person holds a parent connection scope",
			delegatorGrants: []string{"connection:github:write"},
			wantAllow:       true,
			wantReason:      "delegation:mcp:github:create_issue:use",
		},
		{
			name: "person holds the connection scope via a role",
			roleEntries: []*roles.RoleEntry{
				{TenantID: testTenantID, Role: "gh-writer", Scopes: []string{"connection:github:issues:write"}},
			},
			delegatorRoles: map[string][]string{testDelegateID: {"gh-writer"}},
			wantAllow:      true,
			wantReason:     "delegation:mcp:github:create_issue:use",
		},
		{
			name:            "person lacks the connection scope — delegator leg is the limiting factor",
			delegatorGrants: []string{"connection:github:issues:read"}, // wrong action
			wantAllow:       false,
			wantReason:      "deny:no_delegator_grant",
		},
		{
			name:       "person holds nothing",
			wantAllow:  false,
			wantReason: "deny:no_delegator_grant",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			grantList := []*grants.Grant{delegatedAgentGrant(testAgentID, "mcp:github:create_issue:use")}
			if len(tc.delegatorGrants) > 0 {
				grantList = append(grantList, humanGrant(testDelegateID, tc.delegatorGrants...))
			}
			pq := compileAndPrepare(t, bundleOpts{
				grants:         grantList,
				vocab:          vocab,
				roles:          tc.roleEntries,
				principalRoles: tc.delegatorRoles,
			})

			allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), humanDelegation(testDelegateID),
				"mcp", []string{"github", "create_issue"}, "use")
			if allow != tc.wantAllow {
				t.Errorf("allow = %v, want %v (reason: %s)", allow, tc.wantAllow, reason)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

// A multi-requires tool projects through ALL of its upstream scopes: covering
// only some of the expansion is not enough.
func TestAuthz_DelegatorLeg_RequiresProjection_AllRequiredScopes(t *testing.T) {
	vocab := []*vocabulary.ScopeEntry{
		{
			Scope:        "mcp:github:transfer_issue:use",
			ResourceType: "mcp",
			PathSegments: []string{"github", "transfer_issue"},
			Action:       "use",
			Requires:     []string{"connection:github:issues:write", "connection:github:contents:read"},
		},
	}

	tests := []struct {
		name            string
		delegatorScopes []string
		wantAllow       bool
	}{
		{"covers the full expansion", []string{"connection:github:issues:write", "connection:github:contents:read"}, true},
		{"covers only part of the expansion", []string{"connection:github:issues:write"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pq := compileAndPrepare(t, bundleOpts{
				grants: []*grants.Grant{
					delegatedAgentGrant(testAgentID, "mcp:github:transfer_issue:use"),
					humanGrant(testDelegateID, tc.delegatorScopes...),
				},
				vocab: vocab,
			})

			allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), humanDelegation(testDelegateID),
				"mcp", []string{"github", "transfer_issue"}, "use")
			if allow != tc.wantAllow {
				t.Errorf("allow = %v, want %v (reason: %s)", allow, tc.wantAllow, reason)
			}
		})
	}
}

// The delegator-grant lookup matches on principal type as well as id: a
// delegation whose delegator_principal_type is missing, or names a different
// type than the grant's, fails the delegator leg closed. (Closes the former
// TODO(M2) — two principal types sharing an id string.)
func TestAuthz_DelegatorLeg_PrincipalTypeMustMatch(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{
			delegatedAgentGrant(testAgentID, "inference:invoke"),
			humanGrant(testDelegateID, "inference:invoke"),
		},
	})

	tests := []struct {
		name          string
		delegatorType string
	}{
		{"missing type claim", ""},
		{"non-matching type", "PAT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			delegation := &policy.DelegationClaims{Sub: testDelegateID, DelegatorPrincipalType: tc.delegatorType}
			allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
			if allow {
				t.Error("expected deny: the delegator grant leg must match principal type")
			}
			if reason != "deny:no_delegator_grant" {
				t.Errorf("expected deny:no_delegator_grant, got %q", reason)
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// (4) Delegated egress (authz.rego only): egress is the agent's connectivity
// posture, so the delegator is NOT intersected — the agent's own egress grant
// plus a valid delegation suffices. Without delegation the marked agent stays
// fully denied (Phase 1 tightening, re-asserted in authz_delegation_test.go).
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_Egress_Delegated_AllowedWithoutDelegatorEgressGrant(t *testing.T) {
	jwksSrv := startJWKSServer(t)

	pq := compileAndPrepare(t, bundleOpts{
		jwksURL: jwksSrv.URL + "/jwks",
		// The delegator holds NO egress grant — deliberately: egress scopes
		// are evaluated agent-side only under delegation.
		grants: []*grants.Grant{delegatedAgentGrant(testAgentID, "egress:api.github.com:connect")},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)
	delegationToken := signDelegationJWT(t, testDelegateID, nil)

	allow, reason := evalLayer1Connect(t, pq, "api.github.com:443", map[string]string{
		"authorization":            "Bearer " + mainJWT,
		"x-orbit-delegation-token": delegationToken,
	})
	if !allow {
		t.Errorf("expected delegated egress allow, got deny (reason: %s)", reason)
	}
	if reason != "egress_delegated:api.github.com" {
		t.Errorf("expected egress_delegated:api.github.com reason, got %q", reason)
	}
}

// Delegation never widens the agent's posture: a delegated CONNECT to a host
// outside the agent's egress grants stays denied.
func TestAuthz_Egress_Delegated_AgentPostureStillBounds(t *testing.T) {
	jwksSrv := startJWKSServer(t)

	pq := compileAndPrepare(t, bundleOpts{
		jwksURL: jwksSrv.URL + "/jwks",
		grants: []*grants.Grant{
			delegatedAgentGrant(testAgentID, "egress:api.github.com:connect"),
			humanGrant(testDelegateID, "egress:evil.example.com:connect"),
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, testAgentID, nil)
	delegationToken := signDelegationJWT(t, testDelegateID, nil)

	allow, _ := evalLayer1Connect(t, pq, "evil.example.com:443", map[string]string{
		"authorization":            "Bearer " + mainJWT,
		"x-orbit-delegation-token": delegationToken,
	})
	if allow {
		t.Error("expected deny: the delegator's egress grants must never widen the agent's posture")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Gateway mirrors (authz_gateway.rego)
// ═══════════════════════════════════════════════════════════════════════════

func TestGateway_DelegatorLeg_RoleDerivedAllow(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/role-leg-agent"
	delegatorID := "user-role-only"

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
		roles: map[string][]*roles.RoleEntry{
			testTenantID: {
				{TenantID: testTenantID, Role: "analyst", Scopes: []string{"inference:invoke"}},
			},
		},
		principalRoles: map[string]map[string][]string{
			testTenantID: {delegatorID: {"analyst"}},
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
	delegationToken := signDelegationJWT(t, delegatorID, nil)

	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization":            "Bearer " + mainJWT,
		"x-orbit-delegation-token": delegationToken,
	})
	if !allow {
		t.Errorf("expected allow via the delegator's role at the gateway, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "delegation:") {
		t.Errorf("expected delegation reason, got %q", reason)
	}
}

func TestGateway_DelegatorLeg_RequiresProjection(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/projection-agent"

	vocab := []*vocabulary.ScopeEntry{
		{
			Scope:        "mcp:github:create_issue:use",
			ResourceType: "mcp",
			PathSegments: []string{"github", "create_issue"},
			Action:       "use",
			Requires:     []string{"connection:github:issues:write"},
		},
	}

	tests := []struct {
		name           string
		delegatorScope string
		wantAllow      bool
		wantReason     string
	}{
		{"person holds the projected connection scope", "connection:github:issues:write", true, "delegation:mcp:github:create_issue:use"},
		{"person lacks it — delegator leg limits", "connection:github:issues:read", false, "deny:no_delegator_grant"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			delegatorID := "user-projection"
			pq := compileAndPrepareGateway(t, gatewayBundleOpts{
				jwksURL:   jwksSrv.URL + "/jwks",
				tenantIDs: []string{testTenantID},
				vocab:     vocab,
				globalRoutes: []routes.RouteScope{
					{PathPrefix: "/v1/mcp-proxy/github/create_issue", Method: "POST", RequiredScope: "mcp:github:create_issue:use"},
				},
				grants: map[string][]*grants.Grant{
					testTenantID: {
						{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"mcp:github:create_issue:use"}, DelegationRequired: true},
						{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeHuman, PrincipalID: delegatorID, Scopes: []string{tc.delegatorScope}},
					},
				},
			})

			mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
			delegationToken := signDelegationJWT(t, delegatorID, nil)

			allow, reason := evalGateway(t, pq, "POST", "/v1/mcp-proxy/github/create_issue", map[string]string{
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
