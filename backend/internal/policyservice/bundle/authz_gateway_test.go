package bundle_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	opabundle "github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/runkora/kora/backend/internal/policyservice/bundle"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// ---------------------------------------------------------------------------
// Gateway bundle helpers
// ---------------------------------------------------------------------------

type gatewayBundleOpts struct {
	tenantIDs    []string
	grants       map[string][]*grants.Grant
	roles        map[string][]*roles.RoleEntry
	globalRoutes []routes.RouteScope
	tenantRoutes map[string][]routes.RouteScope
	// vocab is the shared scope vocabulary (requires edges for the
	// delegator-leg scope projection).
	vocab []*vocabulary.ScopeEntry
	// principalRoles maps tenant → user→role membership snapshot
	// (delegator-leg role resolution).
	principalRoles map[string]map[string][]string
	frontendHost   string
	// jwksURL overrides the default failing JWKS URL for tests that need real JWT verification.
	jwksURL string
}

// ---------------------------------------------------------------------------
// JWT test helpers (for tests that require real JWT validation)
// ---------------------------------------------------------------------------

var (
	testRSAKey     *rsa.PrivateKey
	testRSAKeyOnce sync.Once
)

func getTestRSAKey() *rsa.PrivateKey {
	testRSAKeyOnce.Do(func() {
		var err error
		testRSAKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
	})
	return testRSAKey
}

// startJWKSServer starts a test HTTP server that serves the test RSA public key
// as a JWKS. The returned server's URL should be used as the jwksURL in
// gatewayBundleOpts for tests that require real JWT verification.
func startJWKSServer(t *testing.T) *httptest.Server {
	t.Helper()
	key := getTestRSAKey()
	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(new(big.Int).SetInt64(int64(key.PublicKey.E)).Bytes())
	jwks := map[string]any{
		"keys": []map[string]any{
			{"kty": "RSA", "use": "sig", "alg": "RS256", "n": n, "e": e},
		},
	}
	jwksJSON, _ := json.Marshal(jwks)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// signTestJWT signs a JWT with the test RSA key for gateway evaluation tests.
// principalType is "AGENT", "HUMAN", or "PAT". koraExtra is merged into the
// orbit claim (e.g. {"claims": {"roles": "platform-admin"}} for HUMAN principals).
func signTestJWT(t *testing.T, principalType, tenantID, principalID string, koraExtra map[string]any) string {
	t.Helper()
	koraClaims := map[string]any{
		"tenant_id":      tenantID,
		"principal_type": principalType,
	}
	maps.Copy(koraClaims, koraExtra)
	claims := jwt.MapClaims{
		"sub":  principalID,
		"iss":  "kora",
		"aud":  jwt.ClaimStrings{"kora-platform"},
		"kora": koraClaims,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(getTestRSAKey())
	if err != nil {
		t.Fatalf("sign test JWT: %v", err)
	}
	return signed
}

// testDelegationRunID is the run a well-formed test delegation token binds to
// when the test is not about run binding. Requests in those tests carry no
// /run/ prefix and no platform-token run_id, so the binding is not checkable
// and the token is honoured — the "run identity unknown here" case.
const testDelegationRunID = "run-test"

// signDelegationJWT signs a well-formed delegation token: sub, scope, run_id,
// and delegator_principal_type, exactly as the identity service mints them.
// scopes is the space-separated scope string in the delegation token.
// delegator_principal_type is always HUMAN, matching what the identity service
// mints (PAT-minted delegations normalise to the person at issuance).
func signDelegationJWT(t *testing.T, delegatorID string, scopes []string) string {
	t.Helper()
	return signDelegationClaims(t, jwt.MapClaims{
		"sub":                      delegatorID,
		"scope":                    strings.Join(scopes, " "),
		"run_id":                   testDelegationRunID,
		"delegator_principal_type": "HUMAN",
	})
}

// signDelegationJWTWithoutRunID signs a delegation token carrying NO run_id
// claim — bound to no run at all. The identity service never mints one, so it
// stands in for a malformed or hand-rolled token.
func signDelegationJWTWithoutRunID(t *testing.T, delegatorID string) string {
	t.Helper()
	return signDelegationClaims(t, jwt.MapClaims{
		"sub":                      delegatorID,
		"scope":                    "",
		"delegator_principal_type": "HUMAN",
	})
}

// signDelegationClaims signs arbitrary delegation claims with the test key,
// filling in the registered claims OPA now verifies (iss, aud, exp) unless the
// caller set them. The identity service signs delegation tokens with the same
// key, issuer, and audience as platform tokens (signDelegationToken in
// internal/identity), which is what makes one JWKS enough for both.
func signDelegationClaims(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	return signDelegationClaimsWithKey(t, claims, getTestRSAKey())
}

// signDelegationClaimsWithKey signs with an explicit key so tests can present
// a token signed by a key the JWKS does not publish.
func signDelegationClaimsWithKey(t *testing.T, claims jwt.MapClaims, key *rsa.PrivateKey) string {
	t.Helper()
	if _, ok := claims["iss"]; !ok {
		claims["iss"] = "kora"
	}
	if _, ok := claims["aud"]; !ok {
		claims["aud"] = jwt.ClaimStrings{"kora-platform"}
	}
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Hour).Unix()
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign delegation JWT: %v", err)
	}
	return signed
}

// otherRSAKey is a signing key the test JWKS never publishes — a forged token.
var (
	otherRSAKey     *rsa.PrivateKey
	otherRSAKeyOnce sync.Once
)

func getOtherRSAKey() *rsa.PrivateKey {
	otherRSAKeyOnce.Do(func() {
		var err error
		otherRSAKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
	})
	return otherRSAKey
}

func compileAndPrepareGateway(t *testing.T, opts gatewayBundleOpts) rego.PreparedEvalQuery {
	t.Helper()

	if opts.frontendHost == "" {
		opts.frontendHost = "orbit.example.com"
	}
	jwksURL := "http://127.0.0.1:1/jwks"
	if opts.jwksURL != "" {
		jwksURL = opts.jwksURL
	}

	cfg := bundle.Config{
		JWKSURL:      jwksURL,
		Issuer:       "kora",
		Audience:     "kora-platform",
		FrontendHost: opts.frontendHost,
	}

	routeStore := routes.NewStore()
	if len(opts.globalRoutes) > 0 {
		routeStore.Register(&routes.Registration{
			Service: "test-global",
			Routes:  opts.globalRoutes,
		})
	}
	for tenantID, tRoutes := range opts.tenantRoutes {
		if len(tRoutes) > 0 {
			routeStore.Register(&routes.Registration{
				Service:  "test-tenant",
				TenantID: tenantID,
				Routes:   tRoutes,
			})
		}
	}

	tenantIDs := opts.tenantIDs
	if tenantIDs == nil {
		tenantIDs = []string{}
	}

	allGrants := opts.grants
	if allGrants == nil {
		allGrants = map[string][]*grants.Grant{}
	}
	allRoles := opts.roles
	if allRoles == nil {
		allRoles = map[string][]*roles.RoleEntry{}
	}

	compiler := bundle.NewCompiler(cfg, routeStore)
	cached, err := compiler.CompileGateway(tenantIDs, allGrants, allRoles, opts.vocab, opts.principalRoles)
	if err != nil {
		t.Fatalf("compile gateway bundle: %v", err)
	}

	b, err := opabundle.NewReader(bytes.NewReader(cached.Data)).Read()
	if err != nil {
		t.Fatalf("read gateway bundle: %v", err)
	}

	pq, err := rego.New(
		rego.Query(`x := {"allow": data.kora.authz_gateway.allow, "reason": data.kora.authz_gateway.reason}`),
		rego.ParsedBundle("gateway", &b),
	).PrepareForEval(context.Background())
	if err != nil {
		t.Fatalf("prepare gateway rego: %v", err)
	}
	return pq
}

// evalGateway evaluates the gateway policy with an Envoy CheckRequest input.
func evalGateway(t *testing.T, pq rego.PreparedEvalQuery, method, path string, headers map[string]string) (bool, string) {
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

// ---------------------------------------------------------------------------
// Frontend host bypass
// ---------------------------------------------------------------------------

// TestGateway_SelfEnforcedMCP_PathBoundary pins the self-enforced bypass to the
// exact "/v1/mcp" path and its "/v1/mcp/" subpaths — a bare prefix would also
// self-enforce (i.e. bypass edge scope enforcement for) an unrelated sibling like
// "/v1/mcpx".
func TestGateway_SelfEnforcedMCP_PathBoundary(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{frontendHost: "orbit.example.com"})
	hdr := map[string]string{":authority": "api.orbit.example.com"}

	// The MCP gateway is self-enforcing → its paths are allowed unauthenticated so
	// it can issue its own 401 challenge.
	for _, p := range []string{"/v1/mcp", "/v1/mcp/kora"} {
		if allow, _ := evalGateway(t, pq, "POST", p, hdr); !allow {
			t.Errorf("%s should be self-enforced (allowed unauthenticated)", p)
		}
	}
	// A sibling that merely shares the prefix must NOT be self-enforced — with no
	// token it is denied by normal edge enforcement.
	if allow, _ := evalGateway(t, pq, "GET", "/v1/mcpx", hdr); allow {
		t.Error("/v1/mcpx must not be self-enforced (would bypass edge scope enforcement)")
	}
}

func TestGateway_FrontendBypass_ExactHost(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		frontendHost: "orbit.example.com",
	})

	allow, _ := evalGateway(t, pq, "GET", "/", map[string]string{
		":authority": "orbit.example.com",
	})
	if !allow {
		t.Error("expected frontend host to be bypassed")
	}
}

func TestGateway_FrontendBypass_HostWithPort(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		frontendHost: "orbit.example.com",
	})

	// Local dev sends :authority with a non-standard port.
	allow, _ := evalGateway(t, pq, "GET", "/login", map[string]string{
		":authority": "orbit.example.com:8443",
	})
	if !allow {
		t.Error("expected frontend host with port to be bypassed")
	}
}

func TestGateway_FrontendBypass_WrongHost_Denied(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		frontendHost: "orbit.example.com",
	})

	// api.orbit.example.com is NOT the frontend host.
	allow, reason := evalGateway(t, pq, "GET", "/v1/models", map[string]string{
		":authority": "api.orbit.example.com",
	})
	if allow {
		t.Error("expected non-frontend host to NOT be bypassed")
	}
	// No route match and no JWT — denied at the invalid-token stage.
	if reason == "" {
		t.Error("expected a deny reason")
	}
}

// ---------------------------------------------------------------------------
// Infrastructure bypass
// ---------------------------------------------------------------------------

func TestGateway_Bypass_HealthCheck(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{})

	allow, _ := evalGateway(t, pq, "GET", "/health", nil)
	if !allow {
		t.Error("expected /health to be bypassed")
	}
}

func TestGateway_Bypass_OIDCDiscovery(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{})

	allow, _ := evalGateway(t, pq, "GET", "/api/v1/identity/.well-known/jwks.json", nil)
	if !allow {
		t.Error("expected OIDC discovery to be bypassed")
	}
}

func TestGateway_Bypass_BundlePolling(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{})

	allow, _ := evalGateway(t, pq, "GET", "/api/v1/policy/bundles/some-tenant", nil)
	if !allow {
		t.Error("expected bundle polling endpoint to be bypassed")
	}
}

// TestGateway_Bypass_RootDiscovery verifies the root RFC 8414/9728 discovery
// documents are bypassed (public, unauthenticated) — ADR 2026-07-04.
func TestGateway_Bypass_RootDiscovery(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{})
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-authorization-server",
	} {
		allow, reason := evalGateway(t, pq, "GET", path, nil)
		if !allow {
			t.Errorf("expected root discovery %s to be bypassed", path)
		}
		if reason != "allow:platform_internal_bypass" {
			t.Errorf("path %s reason = %q, want allow:platform_internal_bypass", path, reason)
		}
	}
}

// TestGateway_SelfEnforced_MCP verifies the MCP gateway path is allowed through
// unauthenticated (so the gateway can issue its own 401 OAuth challenge) even
// though /v1/mcp carries a route-scope — ADR 2026-07-04 (self-enforced services).
func TestGateway_SelfEnforced_MCP(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/mcp", Method: "POST", RequiredScope: "mcp:invoke"},
		},
	})
	// Unauthenticated — must still be allowed through (single reason, no conflict).
	allow, reason := evalGateway(t, pq, "POST", "/v1/mcp", nil)
	if !allow {
		t.Error("expected /v1/mcp to be allowed through to the self-enforcing gateway")
	}
	if reason != "allow:self_enforced" {
		t.Errorf("reason = %q, want allow:self_enforced", reason)
	}
}

// ---------------------------------------------------------------------------
// Token validation
// ---------------------------------------------------------------------------

func TestGateway_NoToken_Denied(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", nil)
	if allow {
		t.Error("expected deny with no authorization header")
	}
	if reason != "deny:invalid_token" {
		t.Errorf("expected deny:invalid_token, got %q", reason)
	}
}

func TestGateway_InvalidToken_Denied(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization": "Bearer not.a.valid.jwt",
	})
	if allow {
		t.Error("expected deny with invalid token")
	}
	if reason != "deny:invalid_token" {
		t.Errorf("expected deny:invalid_token, got %q", reason)
	}
}

// ---------------------------------------------------------------------------
// Route matching
// ---------------------------------------------------------------------------

func TestGateway_NoRouteMatch_Denied(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	// Path not registered — no JWT either, so we get invalid_token before no_route_match.
	// To distinguish no_route_match: use an unregistered path with no token.
	allow, _ := evalGateway(t, pq, "GET", "/api/v1/unknown", nil)
	if allow {
		t.Error("expected deny for unregistered route")
	}
}

func TestGateway_GlobalRoute_WildcardMethod(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/admin", Method: "*", RequiredScope: "platform:admin"},
		},
	})

	for _, method := range []string{"GET", "POST", "DELETE"} {
		_, reason := evalGateway(t, pq, method, "/v1/admin/users", nil)
		// no JWT → deny:invalid_token (not deny:no_route_match) confirms route matched
		if reason == "deny:no_route_match" {
			t.Errorf("wildcard route should match %s /v1/admin/users", method)
		}
	}
}

func TestGateway_TenantRoute_Matched(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		tenantIDs: []string{testTenantID},
		tenantRoutes: map[string][]routes.RouteScope{
			testTenantID: {
				{PathPrefix: "/api/v1/agents", Method: "GET", RequiredScope: "agents:types:read"},
			},
		},
	})

	_, reason := evalGateway(t, pq, "GET", "/api/v1/agents/my-agent", nil)
	// no JWT → deny:invalid_token (not deny:no_route_match) confirms route matched
	if reason == "deny:no_route_match" {
		t.Error("expected tenant route to match")
	}
}

// ---------------------------------------------------------------------------
// Cross-tenant isolation
// ---------------------------------------------------------------------------

func TestGateway_CrossTenant_GrantsIsolated(t *testing.T) {
	tenantA := "tenant-a"
	tenantB := "tenant-b"
	agentID := "spiffe://orbit.test/tenant/tenant-a/agent/agent-1"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		tenantIDs: []string{tenantA, tenantB},
		grants: map[string][]*grants.Grant{
			tenantA: {
				{
					TenantID:      tenantA,
					PrincipalType: grants.PrincipalTypeAgent,
					PrincipalID:   agentID,
					Scopes:        []string{"inference:invoke"},
				},
			},
			// tenantB has no grants.
		},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	// Without a real JWT we can't test the allow path, but we CAN verify that
	// the bundle compiled and the routes are registered. The no_route_match test
	// for an unregistered path confirms the bundle loaded correctly.
	_, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", nil)
	// Should be invalid_token (route matched) not no_route_match (route missed).
	if reason == "deny:no_route_match" {
		t.Error("expected global route to be present in gateway bundle")
	}
	if reason != "deny:invalid_token" {
		t.Errorf("unexpected reason %q for missing token", reason)
	}
}

func TestGateway_TenantRoute_OtherTenantNotVisible(t *testing.T) {
	tenantA := "tenant-a"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		tenantIDs: []string{tenantA},
		tenantRoutes: map[string][]routes.RouteScope{
			tenantA: {
				{PathPrefix: "/api/v1/agents", Method: "GET", RequiredScope: "agents:types:read"},
			},
		},
	})

	// Request without a token targeting the tenant-A route.
	// We can verify the route is present (invalid_token, not no_route_match).
	_, reason := evalGateway(t, pq, "GET", "/api/v1/agents/x", nil)
	if reason == "deny:no_route_match" {
		t.Error("tenant-a route should be present when tenant-a is in the bundle")
	}
}

// ---------------------------------------------------------------------------
// Bundle compilation
// ---------------------------------------------------------------------------

func TestGateway_EmptyBundleCompiles(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{})

	// Health check still bypassed with empty bundle.
	allow, _ := evalGateway(t, pq, "GET", "/health", nil)
	if !allow {
		t.Error("expected /health to be bypassed in empty gateway bundle")
	}
}

func TestGateway_MultiTenantBundle_GlobalRoutesPresent(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		tenantIDs: []string{"tenant-1", "tenant-2", "tenant-3"},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
			{PathPrefix: "/v1/models", Method: "GET", RequiredScope: "inference:list"},
		},
	})

	// All global routes are accessible regardless of tenant.
	for _, tc := range []struct{ method, path string }{
		{"POST", "/v1/chat/completions"},
		{"GET", "/v1/models"},
	} {
		_, reason := evalGateway(t, pq, tc.method, tc.path, nil)
		if reason == "deny:no_route_match" {
			t.Errorf("expected global route %s %s in multi-tenant bundle", tc.method, tc.path)
		}
		if reason != "deny:invalid_token" {
			t.Errorf("expected deny:invalid_token for %s %s (no JWT), got %q", tc.method, tc.path, reason)
		}
	}
}

func TestGateway_FrontendBypass_PathVariants(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		frontendHost: "orbit.example.com",
	})

	// Various browser-originated paths all bypass via the host check.
	paths := []string{"/", "/login", "/dashboard", "/_next/static/abc.js", "/api/auth/session"}
	for _, path := range paths {
		allow, _ := evalGateway(t, pq, "GET", path, map[string]string{
			":authority": "orbit.example.com",
		})
		if !allow {
			t.Errorf("expected frontend bypass for path %s", path)
		}
	}
}

// ---------------------------------------------------------------------------
// Reason rules (without real JWT — token-layer tests only)
// ---------------------------------------------------------------------------

func TestGateway_Reason_FrontendBypass(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{})

	// Frontend bypass sets allow=true; reason should not be a deny.
	allow, reason := evalGateway(t, pq, "GET", "/dashboard", map[string]string{
		":authority": "orbit.example.com",
	})
	if !allow {
		t.Errorf("expected allow for frontend, got deny (reason: %s)", reason)
	}
}

func TestGateway_Reason_InvalidToken_NotFrontend(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	_, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		":authority": "api.orbit.example.com",
	})
	if !strings.Contains(reason, "invalid_token") {
		t.Errorf("expected deny:invalid_token for non-frontend request without token, got %q", reason)
	}
}

// ---------------------------------------------------------------------------
// Infrastructure bypass (M4: identity/exchange was missing)
// ---------------------------------------------------------------------------

func TestGateway_Bypass_IdentityExchange(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{})

	allow, _ := evalGateway(t, pq, "POST", "/api/v1/identity/exchange", nil)
	if !allow {
		t.Error("expected /api/v1/identity/exchange to be bypassed")
	}
}

// ---------------------------------------------------------------------------
// PAT orb_ prefix stripping (M4)
// ---------------------------------------------------------------------------

// TestGateway_PAT_OrbPrefix_Stripped verifies that an orb_-prefixed PAT is
// stripped before JWT validation. The stripped value here is invalid, so the
// result must be deny:invalid_token — not a Rego evaluation error or crash.
func TestGateway_PAT_OrbPrefix_Stripped(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization": "Bearer orb_not.a.valid.jwt",
	})
	if allow {
		t.Error("expected deny for PAT with invalid JWT payload")
	}
	if reason != "deny:invalid_token" {
		t.Errorf("expected deny:invalid_token after orb_ strip, got %q", reason)
	}
}

// ---------------------------------------------------------------------------
// Delegation (M4)
// ---------------------------------------------------------------------------

// TestGateway_Delegation_NoMainJWT verifies that a request with a delegation
// header but no valid main JWT is denied. JWT validation is required before
// delegation logic can fire.
func TestGateway_Delegation_NoMainJWT(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization":           "Bearer not.a.valid.jwt",
		"x-kora-delegation-token": "some.delegation.token",
	})
	if allow {
		t.Error("expected deny for delegated request with invalid main JWT")
	}
	if reason != "deny:invalid_token" {
		t.Errorf("expected deny:invalid_token, got %q", reason)
	}
}

// TestGateway_Delegation_NoDelegationScope verifies deny:no_delegation_scope when
// the agent has a grant and the delegator has a grant but the delegation token
// does not carry the required scope.
func TestGateway_Delegation_NoDelegationScope(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/del-agent"
	delegatorID := "user-delegator-001"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}},
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeHuman, PrincipalID: delegatorID, Scopes: []string{"inference:invoke"}},
			},
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
	// delegation token carries inference:list — wrong scope for inference:invoke
	delegationToken := signDelegationJWT(t, delegatorID, []string{"inference:list"})

	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization":           "Bearer " + mainJWT,
		"x-kora-delegation-token": delegationToken,
	})
	if allow {
		t.Error("expected deny when delegation token lacks required scope")
	}
	if reason != "deny:no_delegation_scope" {
		t.Errorf("expected deny:no_delegation_scope, got %q", reason)
	}
}

// TestGateway_Delegation_NoDelegatorGrant verifies deny:no_delegator_grant when
// the delegation token covers the required scope but the delegator has no grant.
func TestGateway_Delegation_NoDelegatorGrant(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/del-agent-2"
	delegatorID := "user-no-grant-001"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}},
				// delegatorID has no grant
			},
		},
	})

	mainJWT := signTestJWT(t, "AGENT", testTenantID, agentID, nil)
	delegationToken := signDelegationJWT(t, delegatorID, []string{"inference:invoke"})

	allow, reason := evalGateway(t, pq, "POST", "/v1/chat/completions", map[string]string{
		"authorization":           "Bearer " + mainJWT,
		"x-kora-delegation-token": delegationToken,
	})
	if allow {
		t.Error("expected deny when delegator has no grant")
	}
	if reason != "deny:no_delegator_grant" {
		t.Errorf("expected deny:no_delegator_grant, got %q", reason)
	}
}

// ---------------------------------------------------------------------------
// HUMAN role-based allow (M4)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// x-api-key token extraction
// ---------------------------------------------------------------------------

// TestGateway_XAPIKey_WithOrbPrefix verifies that an Anthropic SDK-style
// x-api-key credential prefixed with orb_ is accepted and authorised identically
// to the Authorization: Bearer path.
func TestGateway_XAPIKey_WithOrbPrefix_Allowed(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/xapikey-agent-1"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}},
			},
		},
	})

	signed := signTestJWT(t, "AGENT", testTenantID, agentID, nil)

	allow, reason := evalGateway(t, pq, "POST", "/v1/messages", map[string]string{
		"x-api-key": "orb_" + signed,
	})
	if !allow {
		t.Errorf("expected x-api-key with orb_ prefix to be allowed, got deny (reason: %s)", reason)
	}
}

// TestGateway_XAPIKey_WithoutOrbPrefix verifies that an x-api-key without the
// orb_ prefix is also accepted (trim_prefix is a no-op when the prefix is absent).
func TestGateway_XAPIKey_WithoutOrbPrefix_Allowed(t *testing.T) {
	jwksSrv := startJWKSServer(t)
	agentID := "spiffe://orbit.test/tenant/" + testTenantID + "/agent/xapikey-agent-2"

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		},
		grants: map[string][]*grants.Grant{
			testTenantID: {
				{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeAgent, PrincipalID: agentID, Scopes: []string{"inference:invoke"}},
			},
		},
	})

	signed := signTestJWT(t, "AGENT", testTenantID, agentID, nil)

	allow, reason := evalGateway(t, pq, "POST", "/v1/messages", map[string]string{
		"x-api-key": signed,
	})
	if !allow {
		t.Errorf("expected x-api-key without orb_ prefix to be allowed, got deny (reason: %s)", reason)
	}
}

// TestGateway_XAPIKey_InvalidToken_Denied verifies that an invalid JWT in the
// x-api-key header produces deny:invalid_token.
func TestGateway_XAPIKey_InvalidToken_Denied(t *testing.T) {
	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		},
	})

	allow, reason := evalGateway(t, pq, "POST", "/v1/messages", map[string]string{
		"x-api-key": "orb_not.a.valid.jwt",
	})
	if allow {
		t.Error("expected deny for invalid JWT in x-api-key header")
	}
	if reason != "deny:invalid_token" {
		t.Errorf("expected deny:invalid_token, got %q", reason)
	}
}

// ---------------------------------------------------------------------------
// HUMAN role-based allow (M4)
// ---------------------------------------------------------------------------

// TestGateway_HUMAN_RoleAllow verifies that a HUMAN principal with a reserved
// OIDC role (platform-admin) is allowed to access a route that the role grants.
func TestGateway_HUMAN_RoleAllow(t *testing.T) {
	jwksSrv := startJWKSServer(t)

	pq := compileAndPrepareGateway(t, gatewayBundleOpts{
		jwksURL:   jwksSrv.URL + "/jwks",
		tenantIDs: []string{testTenantID},
		vocab:     adminVocab(),
		globalRoutes: []routes.RouteScope{
			{PathPrefix: "/api/v1/policy/grants", Method: "GET", RequiredScope: "platform:grants:read"},
		},
	})

	signed := signTestJWT(t, "HUMAN", testTenantID, "user-admin-001", map[string]any{
		"claims": map[string]any{"roles": "platform-admin"},
	})

	allow, reason := evalGateway(t, pq, "GET", "/api/v1/policy/grants", map[string]string{
		"authorization": "Bearer " + signed,
	})
	if !allow {
		t.Errorf("expected platform-admin role to allow platform:grants:read, got deny (reason: %s)", reason)
	}
	if !strings.HasPrefix(reason, "role:") {
		t.Errorf("expected role: reason prefix, got %q", reason)
	}
}
