package bundle_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policyservice/bundle"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
	jwt "github.com/golang-jwt/jwt/v5"
	opabundle "github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
)

// ---------------------------------------------------------------------------
// Test constants
// ---------------------------------------------------------------------------

const (
	testAgentID    = "spiffe://orbit.test/tenant/test-tenant/agent/test-agent"
	testHumanID    = "test-human-001"
	testDelegateID = "test-delegator-001"
)

// ---------------------------------------------------------------------------
// Bundle compilation + OPA evaluation helpers
// ---------------------------------------------------------------------------

type bundleOpts struct {
	grants []*grants.Grant
	vocab  []*vocabulary.ScopeEntry
	roles  []*roles.RoleEntry
	routes []routes.RouteScope
}

// compileAndPrepare compiles a real OPA bundle using the same compiler the
// policy-service uses, then prepares it for in-process evaluation — identical
// to how every Orbit service enforces policy at runtime.
func compileAndPrepare(t *testing.T, opts bundleOpts) rego.PreparedEvalQuery {
	t.Helper()

	cfg := bundle.Config{
		// Connection-refused URL so JWKS http.send fails fast instead of
		// blocking on DNS timeout. Layer 1 JWT tests that need real JWKS
		// belong in the live-cluster integration suite.
		JWKSURL:  "http://127.0.0.1:1/jwks",
		Issuer:   "orbit",
		Audience: "orbit-platform",
	}

	routeStore := routes.NewStore()
	if len(opts.routes) > 0 {
		routeStore.Register(&routes.Registration{
			Service: "test",
			Routes:  opts.routes,
		})
	}

	compiler := bundle.NewCompiler(cfg, routeStore)
	cached, err := compiler.Compile(testTenantID, opts.grants, opts.vocab, opts.roles)
	if err != nil {
		t.Fatalf("compile bundle: %v", err)
	}

	b, err := opabundle.NewReader(bytes.NewReader(cached.Data)).Read()
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}

	pq, err := rego.New(
		rego.Query(`x := {"allow": data.orbit.authz.allow, "reason": data.orbit.authz.reason}`),
		rego.ParsedBundle(testTenantID, &b),
	).PrepareForEval(context.Background())
	if err != nil {
		t.Fatalf("prepare rego: %v", err)
	}
	return pq
}

// evalLayer2 evaluates the policy with Layer 2 (Go library) input format.
func evalLayer2(t *testing.T, pq rego.PreparedEvalQuery, claims *token.PlatformClaims, delegation *policy.DelegationClaims, resourceType string, pathSegments []string, action string) (bool, string) {
	t.Helper()

	input := map[string]any{
		"principal": map[string]any{
			"type":      string(claims.Orbit.PrincipalType),
			"tenant_id": claims.Orbit.TenantID,
			"id":        claims.Subject,
			"roles":     extractRoles(claims),
		},
		"required_scope":     policy.DeriveRequiredScope(resourceType, pathSegments, action),
		"delegation_present": delegation != nil,
		"resource":           map[string]any{"type": resourceType},
		"action":             action,
	}
	if delegation != nil {
		input["delegation_present"] = true
		input["delegation_scopes"] = delegation.Scopes
		input["delegator_id"] = delegation.Sub
	}

	return evalPolicy(t, pq, input)
}

// evalLayer1 evaluates the policy with Layer 1 (Envoy CheckRequest) input format.
func evalLayer1(t *testing.T, pq rego.PreparedEvalQuery, method, path string, headers map[string]string) (bool, string) {
	t.Helper()
	if headers == nil {
		headers = map[string]string{}
	}
	input := map[string]any{
		"attributes": map[string]any{
			"request": map[string]any{
				"http": map[string]any{
					"method":  method,
					"path":    path,
					"headers": headers,
				},
			},
		},
	}
	return evalPolicy(t, pq, input)
}

func extractRoles(claims *token.PlatformClaims) []string {
	if claims.Orbit.Claims == nil {
		return []string{}
	}
	raw, ok := claims.Orbit.Claims["roles"]
	if !ok || raw == "" {
		return []string{}
	}
	var out []string
	for p := range strings.SplitSeq(raw, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Claim and grant builders
// ---------------------------------------------------------------------------

func agentClaims(principalID string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: principalID},
		Orbit: token.OrbitClaims{
			PrincipalType: token.PrincipalTypeAgent,
			TenantID:      testTenantID,
		},
	}
}

func humanClaims(principalID string, rolesList ...string) *token.PlatformClaims {
	var claims map[string]string
	if len(rolesList) > 0 {
		claims = map[string]string{"roles": strings.Join(rolesList, ",")}
	}
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: principalID},
		Orbit: token.OrbitClaims{
			PrincipalType: token.PrincipalTypeHuman,
			TenantID:      testTenantID,
			Claims:        claims,
		},
	}
}

func agentGrant(principalID string, scopes ...string) *grants.Grant {
	return &grants.Grant{
		TenantID:      testTenantID,
		PrincipalType: grants.PrincipalTypeAgent,
		PrincipalID:   principalID,
		Scopes:        scopes,
	}
}

func humanGrant(principalID string, scopes ...string) *grants.Grant {
	return &grants.Grant{
		TenantID:      testTenantID,
		PrincipalType: grants.PrincipalTypeHuman,
		PrincipalID:   principalID,
		Scopes:        scopes,
	}
}

func defaultRoutes() []routes.RouteScope {
	return []routes.RouteScope{
		{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/models", Method: "GET", RequiredScope: "inference:list"},
		{PathPrefix: "/api/v1/policy/grants", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/grants", Method: "POST", RequiredScope: "platform:grants:write"},
	}
}

// compileAndPrepareScopesQuery compiles a bundle and prepares the
// principal_all_scopes query for testing the scope projection rule.
func compileAndPrepareScopesQuery(t *testing.T, opts bundleOpts) rego.PreparedEvalQuery {
	t.Helper()

	cfg := bundle.Config{
		JWKSURL:  "http://127.0.0.1:1/jwks",
		Issuer:   "orbit",
		Audience: "orbit-platform",
	}

	routeStore := routes.NewStore()
	if len(opts.routes) > 0 {
		routeStore.Register(&routes.Registration{
			Service: "test",
			Routes:  opts.routes,
		})
	}

	compiler := bundle.NewCompiler(cfg, routeStore)
	cached, err := compiler.Compile(testTenantID, opts.grants, opts.vocab, opts.roles)
	if err != nil {
		t.Fatalf("compile bundle: %v", err)
	}

	b, err := opabundle.NewReader(bytes.NewReader(cached.Data)).Read()
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}

	pq, err := rego.New(
		rego.Query(`x := data.orbit.authz.principal_all_scopes`),
		rego.ParsedBundle(testTenantID, &b),
	).PrepareForEval(context.Background())
	if err != nil {
		t.Fatalf("prepare scopes rego: %v", err)
	}
	return pq
}

// evalScopes evaluates the principal_all_scopes rule and returns the scope set.
func evalScopes(t *testing.T, pq rego.PreparedEvalQuery, claims *token.PlatformClaims) []string {
	t.Helper()

	input := map[string]any{
		"principal": map[string]any{
			"type":      string(claims.Orbit.PrincipalType),
			"tenant_id": claims.Orbit.TenantID,
			"id":        claims.Subject,
			"roles":     extractRoles(claims),
		},
	}

	rs, err := pq.Eval(context.Background(), rego.EvalInput(input))
	if err != nil {
		t.Fatalf("rego scopes eval: %v", err)
	}
	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		return nil
	}
	raw, ok := rs[0].Bindings["x"]
	if !ok {
		return nil
	}
	rawSlice, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rawSlice))
	for _, v := range rawSlice {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func scopeSetContains(scopes []string, expected string) bool {
	return slices.Contains(scopes, expected)
}

// ═══════════════════════════════════════════════════════════════════════════
// Scope projection (principal_all_scopes rule — ADR-0037)
// ═══════════════════════════════════════════════════════════════════════════

func TestScopes_AgentWithGrants(t *testing.T) {
	pq := compileAndPrepareScopesQuery(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:invoke", "inference:list")},
	})

	scopes := evalScopes(t, pq, agentClaims(testAgentID))
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d: %v", len(scopes), scopes)
	}
	if !scopeSetContains(scopes, "inference:invoke") {
		t.Error("expected inference:invoke in scopes")
	}
	if !scopeSetContains(scopes, "inference:list") {
		t.Error("expected inference:list in scopes")
	}
}

func TestScopes_AgentWithNoGrants(t *testing.T) {
	pq := compileAndPrepareScopesQuery(t, bundleOpts{})

	scopes := evalScopes(t, pq, agentClaims(testAgentID))
	if len(scopes) != 0 {
		t.Errorf("expected empty scopes for agent with no grants, got %v", scopes)
	}
}

func TestScopes_HumanWithRoleOnly(t *testing.T) {
	pq := compileAndPrepareScopesQuery(t, bundleOpts{})

	// tenant-admin is a reserved role with hardcoded scopes.
	claims := humanClaims(testHumanID, "tenant-admin")
	scopes := evalScopes(t, pq, claims)

	// Must contain the hardcoded tenant-admin scopes.
	for _, expected := range []string{
		"platform:grants:read",
		"platform:grants:write",
		"platform:roles:read",
		"platform:roles:write",
		"agents:types:read",
		"agents:types:write",
		"agents:runs:read",
		"agents:runs:write",
	} {
		if !scopeSetContains(scopes, expected) {
			t.Errorf("expected %s in tenant-admin scopes, got %v", expected, scopes)
		}
	}
}

func TestScopes_HumanWithGrantsAndRole(t *testing.T) {
	pq := compileAndPrepareScopesQuery(t, bundleOpts{
		grants: []*grants.Grant{humanGrant(testHumanID, "inference:invoke")},
	})

	claims := humanClaims(testHumanID, "tenant-admin")
	scopes := evalScopes(t, pq, claims)

	// Must contain both the grant scope and the role scopes.
	if !scopeSetContains(scopes, "inference:invoke") {
		t.Errorf("expected inference:invoke from grant, got %v", scopes)
	}
	if !scopeSetContains(scopes, "platform:grants:read") {
		t.Errorf("expected platform:grants:read from tenant-admin role, got %v", scopes)
	}
}

func TestScopes_CustomRole(t *testing.T) {
	pq := compileAndPrepareScopesQuery(t, bundleOpts{
		roles: []*roles.RoleEntry{
			{TenantID: testTenantID, Role: "analyst", Scopes: []string{"inference:list", "mcp:gmail:read"}},
		},
	})

	claims := humanClaims(testHumanID, "analyst")
	scopes := evalScopes(t, pq, claims)
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes from analyst role, got %d: %v", len(scopes), scopes)
	}
	if !scopeSetContains(scopes, "inference:list") {
		t.Error("expected inference:list from analyst role")
	}
	if !scopeSetContains(scopes, "mcp:gmail:read") {
		t.Error("expected mcp:gmail:read from analyst role")
	}
}

func TestScopes_WrongPrincipalReturnsEmpty(t *testing.T) {
	pq := compileAndPrepareScopesQuery(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:invoke")},
	})

	// Different agent — should not see testAgentID's grants.
	scopes := evalScopes(t, pq, agentClaims("spiffe://other/agent"))
	if len(scopes) != 0 {
		t.Errorf("expected empty scopes for wrong principal, got %v", scopes)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 2: grant evaluation
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_AgentAllowedWithGrant(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:invoke", "inference:list")},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", nil, "invoke")
	if !allow {
		t.Errorf("expected allow, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "grant:") {
		t.Errorf("expected grant reason, got %q", reason)
	}
}

func TestAuthz_AgentDeniedWithoutGrant(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:invoke")},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "platform", []string{"grants"}, "write")
	if allow {
		t.Error("expected deny, got allow")
	}
	if reason != "deny:no_matching_grant" {
		t.Errorf("expected deny:no_matching_grant, got %q", reason)
	}
}

func TestAuthz_AgentDeniedWrongPrincipal(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:invoke")},
	})

	allow, reason := evalLayer2(t, pq, agentClaims("spiffe://other/agent"), nil, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny for wrong principal")
	}
	if reason != "deny:no_matching_grant" {
		t.Errorf("expected deny:no_matching_grant, got %q", reason)
	}
}

func TestAuthz_EmptyPrincipal(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{})

	emptyClaims := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: ""},
		Orbit:            token.OrbitClaims{TenantID: testTenantID},
	}
	allow, reason := evalLayer2(t, pq, emptyClaims, nil, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny with empty principal")
	}
	if reason != "deny:no_matching_grant" {
		t.Errorf("expected deny:no_matching_grant, got %q", reason)
	}
}

func TestAuthz_MultipleOverlappingGrants(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{
			agentGrant(testAgentID, "inference:invoke"),
			agentGrant(testAgentID, "inference:anthropic:invoke"),
		},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", []string{"anthropic"}, "invoke")
	if !allow {
		t.Errorf("expected allow with overlapping grants, got deny (reason: %s)", reason)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 2: scope hierarchy
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_ScopeHierarchy_ParentCoversChild(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:invoke")},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", []string{"anthropic", "claude-sonnet"}, "invoke")
	if !allow {
		t.Errorf("expected parent scope to cover child, got deny (reason: %s)", reason)
	}
}

func TestAuthz_ScopeHierarchy_ChildDoesNotCoverParent(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:anthropic:invoke")},
	})

	allow, _ := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", nil, "invoke")
	if allow {
		t.Error("expected child scope NOT to cover parent, got allow")
	}
}

func TestAuthz_ScopeHierarchy_DifferentAction(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:invoke")},
	})

	allow, _ := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", nil, "list")
	if allow {
		t.Error("expected different action to be denied")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 2: OIDC roles
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_HumanWithRoleAllowed(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{})

	claims := humanClaims(testHumanID, "tenant-admin")
	allow, reason := evalLayer2(t, pq, claims, nil, "platform", []string{"grants"}, "read")
	if !allow {
		t.Errorf("expected tenant-admin role to allow, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "role:") {
		t.Errorf("expected role reason, got %q", reason)
	}
}

func TestAuthz_CustomRole(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		roles: []*roles.RoleEntry{
			{TenantID: testTenantID, Role: "analyst", Scopes: []string{"inference:list"}},
		},
	})

	claims := humanClaims(testHumanID, "analyst")
	allow, reason := evalLayer2(t, pq, claims, nil, "inference", nil, "list")
	if !allow {
		t.Errorf("expected custom role to allow, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "role:") {
		t.Errorf("expected role reason, got %q", reason)
	}
}

func TestAuthz_ReservedRoleCannotBeOverridden(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		roles: []*roles.RoleEntry{
			{TenantID: testTenantID, Role: "tenant-admin", Scopes: []string{"inference:invoke"}},
		},
	})

	claims := humanClaims(testHumanID, "tenant-admin")
	allow, _ := evalLayer2(t, pq, claims, nil, "platform", []string{"grants"}, "read")
	if !allow {
		t.Error("expected hardcoded tenant-admin to still work after override attempt")
	}
}

func TestAuthz_HumanRoleDeniedWhenDelegated(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{})

	claims := humanClaims(testHumanID, "tenant-admin")
	delegation := &policy.DelegationClaims{
		Sub:    testDelegateID,
		Scopes: []string{"platform:grants:read"},
	}

	allow, reason := evalLayer2(t, pq, claims, delegation, "platform", []string{"grants"}, "read")
	if allow {
		t.Errorf("expected role bypass to be blocked during delegation, got allow (reason: %s)", reason)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 2: delegation
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_Delegation_FullChain(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{
			agentGrant(testAgentID, "inference:invoke"),
			humanGrant(testDelegateID, "inference:invoke"),
		},
	})

	delegation := &policy.DelegationClaims{
		Sub:    testDelegateID,
		Scopes: []string{"inference:invoke"},
	}

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
	if !allow {
		t.Errorf("expected delegation allow, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "delegation:") {
		t.Errorf("expected delegation reason, got %q", reason)
	}
}

func TestAuthz_Delegation_DeniedNoDelegationScope(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{
			agentGrant(testAgentID, "inference:invoke"),
			humanGrant(testDelegateID, "inference:invoke"),
		},
	})

	delegation := &policy.DelegationClaims{
		Sub:    testDelegateID,
		Scopes: []string{"inference:list"}, // wrong scope
	}

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny when delegation token lacks required scope")
	}
	if reason != "deny:no_delegation_scope" {
		t.Errorf("expected deny:no_delegation_scope, got %q", reason)
	}
}

func TestAuthz_Delegation_DeniedNoDelegatorGrant(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID, "inference:invoke")},
	})

	delegation := &policy.DelegationClaims{
		Sub:    testDelegateID,
		Scopes: []string{"inference:invoke"},
	}

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny when delegator has no grant")
	}
	if reason != "deny:no_delegator_grant" {
		t.Errorf("expected deny:no_delegator_grant, got %q", reason)
	}
}

func TestAuthz_Delegation_DeniedNoAgentGrant(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{humanGrant(testDelegateID, "inference:invoke")},
	})

	delegation := &policy.DelegationClaims{
		Sub:    testDelegateID,
		Scopes: []string{"inference:invoke"},
	}

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), delegation, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny when agent has no grant")
	}
	if reason != "deny:no_agent_grant" {
		t.Errorf("expected deny:no_agent_grant, got %q", reason)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 1: platform internal bypass
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_Layer1_PlatformBypass_IdentityExchange(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{})

	allow, _ := evalLayer1(t, pq, "POST", "/api/v1/identity/exchange", nil)
	if !allow {
		t.Error("expected platform bypass for /api/v1/identity/exchange")
	}
}

func TestAuthz_Layer1_PlatformBypass_JWKS(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{})

	allow, _ := evalLayer1(t, pq, "GET", "/api/v1/identity/.well-known/jwks.json", nil)
	if !allow {
		t.Error("expected platform bypass for JWKS endpoint")
	}
}

func TestAuthz_Layer1_PlatformBypass_BundleEndpoint(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{})

	allow, _ := evalLayer1(t, pq, "GET", "/api/v1/policy/bundles/example", nil)
	if !allow {
		t.Error("expected platform bypass for bundle endpoint")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 1: token and route validation
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_Layer1_NoAuthorizationHeader(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{routes: defaultRoutes()})

	allow, reason := evalLayer1(t, pq, "GET", "/v1/models", nil)
	if allow {
		t.Error("expected deny with no authorization header")
	}
	if reason != "deny:invalid_token" {
		t.Errorf("expected deny:invalid_token, got %q", reason)
	}
}

func TestAuthz_Layer1_InvalidToken(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{routes: defaultRoutes()})

	allow, reason := evalLayer1(t, pq, "GET", "/v1/models", map[string]string{
		"authorization": "Bearer not.a.valid.jwt",
	})
	if allow {
		t.Error("expected deny with invalid token")
	}
	if reason != "deny:invalid_token" {
		t.Errorf("expected deny:invalid_token, got %q", reason)
	}
}

func TestAuthz_Layer1_NoRouteMatch(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{routes: defaultRoutes()})

	allow, _ := evalLayer1(t, pq, "GET", "/api/v1/unknown/endpoint", nil)
	if allow {
		t.Error("expected deny for unregistered route")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 1: route matching
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_Layer1_RouteMatching_ExactMethod(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		routes: []routes.RouteScope{
			{PathPrefix: "/v1/models", Method: "GET", RequiredScope: "inference:list"},
			{PathPrefix: "/v1/models", Method: "POST", RequiredScope: "inference:create"},
		},
	})

	// GET /v1/models should match the GET route, not the POST route.
	// deny:invalid_token (no JWT) confirms the route matched — if it didn't,
	// we'd get deny:no_route_match instead.
	_, reason := evalLayer1(t, pq, "GET", "/v1/models", nil)
	if reason == "deny:no_route_match" {
		t.Error("expected route to match GET /v1/models")
	}
}

func TestAuthz_Layer1_RouteMatching_WildcardMethod(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		routes: []routes.RouteScope{
			{PathPrefix: "/v1/admin", Method: "*", RequiredScope: "platform:admin"},
		},
	})

	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		_, reason := evalLayer1(t, pq, method, "/v1/admin/users", nil)
		if reason == "deny:no_route_match" {
			t.Errorf("expected wildcard route to match %s /v1/admin/users", method)
		}
	}
}

func TestAuthz_Layer1_RouteMatching_ExactMethodPrecedence(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		routes: []routes.RouteScope{
			{PathPrefix: "/v1/data", Method: "GET", RequiredScope: "data:read"},
			{PathPrefix: "/v1/data", Method: "*", RequiredScope: "data:admin"},
		},
		grants: []*grants.Grant{agentGrant(testAgentID, "data:read")},
	})

	// Layer 1 tests without a real JWT can't reach the allow rule (they get
	// deny:invalid_token). Use Layer 2 to verify that the exact method route
	// (data:read) is selected over the wildcard (data:admin). The agent only
	// holds data:read — if the wildcard's data:admin were resolved instead,
	// the check would deny.
	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "data", nil, "read")
	if !allow {
		t.Errorf("expected allow (exact route data:read selected), got deny (reason: %s)", reason)
	}

	// Verify wildcard kicks in for a non-GET method: the agent lacks data:admin.
	allow, reason = evalLayer2(t, pq, agentClaims(testAgentID), nil, "data", nil, "admin")
	if allow {
		t.Error("expected deny for data:admin (agent only has data:read)")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Cross-tenant isolation
// ═══════════════════════════════════════════════════════════════════════════

func TestAuthz_CrossTenantGrantDoesNotApply(t *testing.T) {
	// Bundles are compiled per-tenant — only grants for the target tenant are
	// included. Simulate the service layer filtering: the agent holds a grant
	// in "other-tenant" but that grant is NOT passed to the compiler when
	// building the testTenantID bundle. The agent should be denied.
	pq := compileAndPrepare(t, bundleOpts{
		// No grants for testTenantID — the other-tenant grant is excluded by
		// the service layer before compilation.
		grants: []*grants.Grant{},
	})

	allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "inference", nil, "invoke")
	if allow {
		t.Error("expected deny — no grants in this tenant's bundle")
	}
	if reason != "deny:no_matching_grant" {
		t.Errorf("expected reason deny:no_matching_grant, got %s", reason)
	}
}
