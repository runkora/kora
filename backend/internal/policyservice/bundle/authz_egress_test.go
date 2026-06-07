package bundle_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/clear-route/orbit/internal/policyservice/bundle"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	opabundle "github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
)

// ═══════════════════════════════════════════════════════════════════════════
// Egress connectivity posture (ADR 2026-06-06, Amendment 2026-06-07)
//
// The sandbox OPA authorizes the egress CONNECT against the egress:<host>:connect
// vocabulary. The full allow path is Layer-1 (Envoy CheckRequest) and needs a
// real JWKS to derive the principal, so it is exercised in the live-cluster
// suite — same limitation the other Layer-1 allow tests note. Here we unit-test
// the two pieces that carry the security weight and are pure functions of their
// inputs: the host-matching algebra (_host_matches) and the scope-shape
// extraction (_egress_host_pattern).
// ═══════════════════════════════════════════════════════════════════════════

// prepareAuthzFuncQuery compiles a bundle (no grants needed — these are pure
// helper functions) and prepares a query that invokes a single authz rule/func.
func prepareAuthzFuncQuery(t *testing.T, query string) rego.PreparedEvalQuery {
	t.Helper()

	cfg := bundle.Config{
		JWKSURL:  "http://127.0.0.1:1/jwks",
		Issuer:   "orbit",
		Audience: "orbit-platform",
	}
	compiler := bundle.NewCompiler(cfg, routes.NewStore())
	cached, err := compiler.Compile(testTenantID, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("compile bundle: %v", err)
	}
	b, err := opabundle.NewReader(bytes.NewReader(cached.Data)).Read()
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	pq, err := rego.New(
		rego.Query(query),
		rego.ParsedBundle(testTenantID, &b),
	).PrepareForEval(context.Background())
	if err != nil {
		t.Fatalf("prepare rego (%s): %v", query, err)
	}
	return pq
}

// evalHostMatch invokes _host_matches(pattern, host) and reports whether it held.
func evalHostMatch(t *testing.T, pq rego.PreparedEvalQuery, pattern, host string) bool {
	t.Helper()
	rs, err := pq.Eval(context.Background(), rego.EvalInput(map[string]any{
		"pattern": pattern,
		"host":    host,
	}))
	if err != nil {
		t.Fatalf("host-match eval (%q,%q): %v", pattern, host, err)
	}
	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		return false
	}
	v, ok := rs[0].Bindings["x"].(bool)
	return ok && v
}

func TestEgress_HostMatches(t *testing.T) {
	pq := prepareAuthzFuncQuery(t, `x := data.orbit.authz._host_matches(input.pattern, input.host)`)

	cases := []struct {
		name    string
		pattern string
		host    string
		want    bool
	}{
		// exact
		{"exact match", "api.github.com", "api.github.com", true},
		{"exact mismatch", "api.github.com", "github.com", false},
		{"exact not a subdomain", "github.com", "api.github.com", false},

		// open
		{"star matches anything", "*", "api.github.com", true},
		{"star matches apex", "*", "github.com", true},
		{"star matches single label", "*", "localhost", true},

		// *.suffix — apex and any subdomain depth
		{"wildcard apex", "*.github.com", "github.com", true},
		{"wildcard one level", "*.github.com", "api.github.com", true},
		{"wildcard deep", "*.github.com", "a.b.c.github.com", true},

		// *.suffix — must NOT over-match (the leading dot guards suffix confusion)
		{"wildcard rejects suffix-confusion", "*.github.com", "evilgithub.com", false},
		{"wildcard rejects different domain", "*.github.com", "github.com.evil.com", false},
		{"wildcard rejects unrelated", "*.github.com", "example.com", false},
		{"wildcard rejects empty-ish", "*.github.com", "com", false},

		// exact wildcard pattern is not treated as glob beyond the defined cases
		{"exact rejects different host", "api.github.com", "api.gitlab.com", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalHostMatch(t, pq, tc.pattern, tc.host); got != tc.want {
				t.Errorf("_host_matches(%q, %q) = %v, want %v", tc.pattern, tc.host, got, tc.want)
			}
		})
	}
}

// evalEgressPattern invokes _egress_host_pattern(scope); ok is false when the
// scope is not an egress scope (the function is undefined for it).
func evalEgressPattern(t *testing.T, pq rego.PreparedEvalQuery, scope string) (string, bool) {
	t.Helper()
	rs, err := pq.Eval(context.Background(), rego.EvalInput(map[string]any{"scope": scope}))
	if err != nil {
		t.Fatalf("egress-pattern eval (%q): %v", scope, err)
	}
	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		return "", false
	}
	v, ok := rs[0].Bindings["x"].(string)
	return v, ok
}

func TestEgress_HostPatternExtraction(t *testing.T) {
	pq := prepareAuthzFuncQuery(t, `x := data.orbit.authz._egress_host_pattern(input.scope)`)

	cases := []struct {
		name    string
		scope   string
		want    string
		defined bool
	}{
		{"two-part open", "egress:connect", "*", true},
		{"exact host", "egress:api.github.com:connect", "api.github.com", true},
		{"wildcard host", "egress:*.github.com:connect", "*.github.com", true},
		{"star host", "egress:*:connect", "*", true},

		// non-egress scopes must not yield a pattern
		{"connection scope ignored", "connection:github:repo:write", "", false},
		{"inference scope ignored", "inference:invoke", "", false},
		{"wrong action ignored", "egress:api.github.com:read", "", false},
		{"wrong resource ignored", "ingress:api.github.com:connect", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := evalEgressPattern(t, pq, tc.scope)
			if ok != tc.defined {
				t.Fatalf("_egress_host_pattern(%q) defined=%v, want %v (got %q)", tc.scope, ok, tc.defined, got)
			}
			if tc.defined && got != tc.want {
				t.Errorf("_egress_host_pattern(%q) = %q, want %q", tc.scope, got, tc.want)
			}
		})
	}
}
