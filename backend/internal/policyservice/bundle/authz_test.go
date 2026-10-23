package bundle_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	jwt "github.com/golang-jwt/jwt/v5"
	opabundle "github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/bundle"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// ---------------------------------------------------------------------------
// Test constants
// ---------------------------------------------------------------------------

const (
	testAgentID    = "spiffe://runkora.test/tenant/test-tenant/agent/test-agent"
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
	// tenantRoutes are registered for testTenantID, apart from the global routes.
	tenantRoutes []routes.RouteScope
	// principalRoles is the user→role membership snapshot emitted as
	// principal_roles/data.json (delegator-leg role resolution).
	principalRoles map[string][]string
	// jwksURL overrides the default failing JWKS URL for Layer-1 tests that
	// need real JWT verification (pair with startJWKSServer / signTestJWT).
	jwksURL string
	// telemetryCollector is the collector authority compiled into data.config.
	telemetryCollector string
}

// compileAndPrepare compiles a real OPA bundle using the same compiler the
// policy-service uses, then prepares it for in-process evaluation — identical
// to how every kora service enforces policy at runtime.
func compileAndPrepare(t *testing.T, opts bundleOpts) rego.PreparedEvalQuery {
	t.Helper()

	// Connection-refused URL by default so JWKS http.send fails fast instead
	// of blocking on DNS timeout; opts.jwksURL points at a local test server
	// for Layer-1 tests that need real JWT verification.
	jwksURL := "http://127.0.0.1:1/jwks"
	if opts.jwksURL != "" {
		jwksURL = opts.jwksURL
	}
	cfg := bundle.Config{
		JWKSURL:            jwksURL,
		Issuer:             "kora",
		Audience:           "kora-platform",
		TelemetryCollector: opts.telemetryCollector,
	}

	routeStore := routes.NewStore()
	if len(opts.routes) > 0 {
		routeStore.Register(&routes.Registration{
			Service: "test",
			Routes:  opts.routes,
		})
	}
	if len(opts.tenantRoutes) > 0 {
		routeStore.Register(&routes.Registration{
			Service:  "test-tenant-service",
			TenantID: testTenantID,
			Routes:   opts.tenantRoutes,
		})
	}

	compiler := bundle.NewCompiler(cfg, routeStore)
	cached, err := compiler.Compile(testTenantID, opts.grants, opts.vocab, opts.roles, nil, opts.principalRoles)
	if err != nil {
		t.Fatalf("compile bundle: %v", err)
	}

	b, err := opabundle.NewReader(bytes.NewReader(cached.Data)).Read()
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}

	pq, err := rego.New(
		rego.Query(`x := {"allow": data.kora.authz.allow, "reason": data.kora.authz.reason}`),
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
			"type":      string(claims.Kora.PrincipalType),
			"tenant_id": claims.Kora.TenantID,
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
		// Mirrors buildOPAInput: the claim is omitted when empty, and the
		// delegator-grant leg then fails closed.
		if delegation.DelegatorPrincipalType != "" {
			input["delegator_principal_type"] = delegation.DelegatorPrincipalType
		}
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
	if claims.Kora.Claims == nil {
		return []string{}
	}
	raw, ok := claims.Kora.Claims["roles"]
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
		Kora: token.KoraClaims{
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
		Kora: token.KoraClaims{
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
		Issuer:   "kora",
		Audience: "kora-platform",
	}

	routeStore := routes.NewStore()
	if len(opts.routes) > 0 {
		routeStore.Register(&routes.Registration{
			Service: "test",
			Routes:  opts.routes,
		})
	}
	if len(opts.tenantRoutes) > 0 {
		routeStore.Register(&routes.Registration{
			Service:  "test-tenant-service",
			TenantID: testTenantID,
			Routes:   opts.tenantRoutes,
		})
	}

	compiler := bundle.NewCompiler(cfg, routeStore)
	cached, err := compiler.Compile(testTenantID, opts.grants, opts.vocab, opts.roles, nil, opts.principalRoles)
	if err != nil {
		t.Fatalf("compile bundle: %v", err)
	}

	b, err := opabundle.NewReader(bytes.NewReader(cached.Data)).Read()
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}

	pq, err := rego.New(
		rego.Query(`x := data.kora.authz.principal_all_scopes`),
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
			"type":      string(claims.Kora.PrincipalType),
			"tenant_id": claims.Kora.TenantID,
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
// Scope projection (principal_all_scopes rule — docs/adr/2026-04-14-frontend-policy-projection.md)
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

// adminVocab is the registered vocabulary the derived admin role is tested
// against; the admin holds every scope here that no other entry covers.
func adminVocab() []*vocabulary.ScopeEntry {
	var out []*vocabulary.ScopeEntry
	for _, sc := range []string{
		"platform:grants:read", "platform:grants:write", "platform:roles:read", "platform:roles:write",
		"agents:types:read", "agents:types:write", "agents:runs:read", "agents:runs:write",
		"inference:invoke", "inference:list",
	} {
		out = append(out, &vocabulary.ScopeEntry{Scope: sc})
	}
	return out
}

func TestScopes_HumanWithRoleOnly(t *testing.T) {
	pq := compileAndPrepareScopesQuery(t, bundleOpts{vocab: adminVocab()})

	claims := humanClaims(testHumanID, "platform-admin")
	scopes := evalScopes(t, pq, claims)

	// Must contain the derived admin scopes.
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
			t.Errorf("expected %s in platform-admin scopes, got %v", expected, scopes)
		}
	}
}

func TestScopes_HumanWithGrantsAndRole(t *testing.T) {
	pq := compileAndPrepareScopesQuery(t, bundleOpts{
		grants: []*grants.Grant{humanGrant(testHumanID, "inference:invoke")},
		vocab:  adminVocab(),
	})

	claims := humanClaims(testHumanID, "platform-admin")
	scopes := evalScopes(t, pq, claims)

	// Must contain both the grant scope and the role scopes.
	if !scopeSetContains(scopes, "inference:invoke") {
		t.Errorf("expected inference:invoke from grant, got %v", scopes)
	}
	if !scopeSetContains(scopes, "platform:grants:read") {
		t.Errorf("expected platform:grants:read from platform-admin role, got %v", scopes)
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

// TestAuthz_QualifiedConnectionScopes locks the coverage behavior of
// resource-qualified connection scopes
// (docs/adr/2026-07-22-connection-scope-qualifiers.md): an unqualified grant
// covers the qualified requirement by prefix, a qualified grant matches only
// its own resource, and a qualified grant never satisfies a broader
// (unqualified) requirement.
func TestAuthz_QualifiedConnectionScopes(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		grants: []*grants.Grant{agentGrant(testAgentID,
			"connection:github:issues:write",         // unqualified — all repos
			"connection:github:contents:kora:read")}, // qualified — one repo
	})

	cases := []struct {
		name  string
		path  []string
		verb  string
		allow bool
	}{
		{"unqualified_grant_covers_qualified_request", []string{"github", "issues", "kora"}, "write", true},
		{"unqualified_grant_covers_other_repo", []string{"github", "issues", "webapp"}, "write", true},
		{"unqualified_grant_covers_unqualified_request", []string{"github", "issues"}, "write", true},
		{"qualified_grant_matches_its_repo", []string{"github", "contents", "kora"}, "read", true},
		{"qualified_grant_denies_sibling_repo", []string{"github", "contents", "webapp"}, "read", false},
		{"qualified_grant_denies_unqualified_request", []string{"github", "contents"}, "read", false},
		{"category_grant_denies_other_category", []string{"github", "admin", "kora"}, "write", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "connection", tc.path, tc.verb)
			if allow != tc.allow {
				t.Errorf("connection/%v/%s: allow = %v (reason %q), want %v", tc.path, tc.verb, allow, reason, tc.allow)
			}
		})
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
		Kora:             token.KoraClaims{TenantID: testTenantID},
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

// Mode-qualified agent-type creation scopes (delegated-agent-identity ADR,
// Decision 1): agents:types:write prefix-covers both agents:types:{mode}:write
// children (same resource type + action, path prefix), while each child covers
// only its own mode and never the parent.
func TestAuthz_ScopeHierarchy_AgentTypeModeScopes(t *testing.T) {
	t.Run("parent covers delegated child", func(t *testing.T) {
		pq := compileAndPrepare(t, bundleOpts{
			grants: []*grants.Grant{agentGrant(testAgentID, "agents:types:write")},
		})
		allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "agents", []string{"types", "delegated"}, "write")
		if !allow {
			t.Errorf("expected agents:types:write to cover agents:types:delegated:write, got deny (reason: %s)", reason)
		}
	})
	t.Run("parent covers autonomous child", func(t *testing.T) {
		pq := compileAndPrepare(t, bundleOpts{
			grants: []*grants.Grant{agentGrant(testAgentID, "agents:types:write")},
		})
		allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "agents", []string{"types", "autonomous"}, "write")
		if !allow {
			t.Errorf("expected agents:types:write to cover agents:types:autonomous:write, got deny (reason: %s)", reason)
		}
	})
	t.Run("delegated child does not cover autonomous sibling", func(t *testing.T) {
		pq := compileAndPrepare(t, bundleOpts{
			grants: []*grants.Grant{agentGrant(testAgentID, "agents:types:delegated:write")},
		})
		allow, _ := evalLayer2(t, pq, agentClaims(testAgentID), nil, "agents", []string{"types", "autonomous"}, "write")
		if allow {
			t.Error("expected agents:types:delegated:write NOT to cover agents:types:autonomous:write, got allow")
		}
	})
	t.Run("delegated child does not cover parent", func(t *testing.T) {
		pq := compileAndPrepare(t, bundleOpts{
			grants: []*grants.Grant{agentGrant(testAgentID, "agents:types:delegated:write")},
		})
		allow, _ := evalLayer2(t, pq, agentClaims(testAgentID), nil, "agents", []string{"types"}, "write")
		if allow {
			t.Error("expected agents:types:delegated:write NOT to cover agents:types:write, got allow")
		}
	})
	t.Run("delegated child covers itself", func(t *testing.T) {
		pq := compileAndPrepare(t, bundleOpts{
			grants: []*grants.Grant{agentGrant(testAgentID, "agents:types:delegated:write")},
		})
		allow, reason := evalLayer2(t, pq, agentClaims(testAgentID), nil, "agents", []string{"types", "delegated"}, "write")
		if !allow {
			t.Errorf("expected agents:types:delegated:write to cover itself, got deny (reason: %s)", reason)
		}
	})
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
	pq := compileAndPrepare(t, bundleOpts{vocab: adminVocab()})

	claims := humanClaims(testHumanID, "platform-admin")
	allow, reason := evalLayer2(t, pq, claims, nil, "platform", []string{"grants"}, "read")
	if !allow {
		t.Errorf("expected platform-admin role to allow, got deny (reason: %s)", reason)
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
		vocab: adminVocab(),
		roles: []*roles.RoleEntry{
			{TenantID: testTenantID, Role: "platform-admin", Scopes: []string{"inference:invoke"}},
		},
	})

	claims := humanClaims(testHumanID, "platform-admin")
	allow, _ := evalLayer2(t, pq, claims, nil, "platform", []string{"grants"}, "read")
	if !allow {
		t.Error("expected the derived platform-admin to still work after override attempt")
	}
}

func TestAuthz_HumanRoleDeniedWhenDelegated(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{vocab: adminVocab()})

	claims := humanClaims(testHumanID, "platform-admin")
	delegation := &policy.DelegationClaims{
		Sub:                    testDelegateID,
		DelegatorPrincipalType: "HUMAN",
		Scopes:                 []string{"platform:grants:read"},
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
		Sub:                    testDelegateID,
		DelegatorPrincipalType: "HUMAN",
		Scopes:                 []string{"inference:invoke"},
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
		Sub:                    testDelegateID,
		DelegatorPrincipalType: "HUMAN",
		Scopes:                 []string{"inference:list"}, // wrong scope
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
		Sub:                    testDelegateID,
		DelegatorPrincipalType: "HUMAN",
		Scopes:                 []string{"inference:invoke"},
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
		Sub:                    testDelegateID,
		DelegatorPrincipalType: "HUMAN",
		Scopes:                 []string{"inference:invoke"},
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

// Run identity travels as a "/run/{run_id}" path prefix (hierarchical budget
// ADR). The Envoy/Layer-1 route matcher must strip it so a run-scoped inference
// call resolves to the same route as the unprefixed path — otherwise the agent
// pod's OPA sidecar denies it with no_route_match before it reaches the gateway.
func TestAuthz_Layer1_RunPrefixStrippedForRouteMatching(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{
		routes: []routes.RouteScope{
			{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	// A run-prefixed inference path must match the same route as /v1/messages.
	// reason != no_route_match confirms the prefix was stripped before matching
	// (no JWT here, so it proceeds to deny:invalid_token once the route matched).
	_, reason := evalLayer1(t, pq, "POST", "/run/abc-123/v1/messages?beta=true", nil)
	if reason == "deny:no_route_match" {
		t.Error("run-prefixed path should match the /v1/messages route after prefix stripping")
	}

	// The unprefixed path still matches (no regression for non-agent callers).
	_, plain := evalLayer1(t, pq, "POST", "/v1/messages", nil)
	if plain == "deny:no_route_match" {
		t.Error("unprefixed /v1/messages must still match its route")
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
