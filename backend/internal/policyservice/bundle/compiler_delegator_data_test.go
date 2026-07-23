package bundle

import (
	"encoding/json"
	"testing"

	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// Delegator-leg bundle data (delegated-agent-identity ADR, Decision 5): the
// per-tenant and gateway bundles must carry the user→role membership snapshot
// (principal_roles) and the vocabulary requires edges, so both rego layers can
// resolve the delegator's role-derived access and the requires scope
// projection offline.

func delegatorDataVocab() []*vocabulary.ScopeEntry {
	return []*vocabulary.ScopeEntry{
		{
			Scope:        "mcp:github:create_issue:use",
			ResourceType: "mcp",
			PathSegments: []string{"github", "create_issue"},
			Action:       "use",
			Requires:     []string{"connection:github:issues:write"},
		},
		{
			Scope:        "connection:github:issues:write",
			ResourceType: "connection",
			PathSegments: []string{"github", "issues"},
			Action:       "write",
		},
	}
}

func assertPrincipalRolesDoc(t *testing.T, raw []byte) {
	t.Helper()
	var doc struct {
		PrincipalRoles map[string][]string `json:"principal_roles"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal principal_roles doc: %v", err)
	}
	if got := doc.PrincipalRoles["user-1"]; len(got) != 2 || got[0] != "analyst" || got[1] != "engineer" {
		t.Errorf("principal_roles[user-1] = %v, want [analyst engineer]", got)
	}
}

func assertVocabRequires(t *testing.T, raw []byte) {
	t.Helper()
	var doc struct {
		ScopeVocabulary map[string]struct {
			Requires []string `json:"requires"`
		} `json:"scope_vocabulary"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal vocabulary doc: %v", err)
	}
	mcp, ok := doc.ScopeVocabulary["mcp:github:create_issue:use"]
	if !ok {
		t.Fatal("vocabulary doc missing the mcp tool scope")
	}
	if len(mcp.Requires) != 1 || mcp.Requires[0] != "connection:github:issues:write" {
		t.Errorf("requires = %v, want [connection:github:issues:write]", mcp.Requires)
	}
	if conn := doc.ScopeVocabulary["connection:github:issues:write"]; len(conn.Requires) != 0 {
		t.Errorf("plain scope must carry no requires, got %v", conn.Requires)
	}
}

func TestCompile_EmitsPrincipalRolesAndVocabRequires(t *testing.T) {
	cfg := Config{JWKSURL: "http://127.0.0.1:1/jwks", Issuer: "orbit", Audience: "orbit-platform"}
	compiler := NewCompiler(cfg, routes.NewStore())

	cached, err := compiler.Compile("acme", nil, delegatorDataVocab(), nil, nil,
		map[string][]string{"user-1": {"analyst", "engineer"}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	assertPrincipalRolesDoc(t, extractBundleFile(t, cached.Data, "principal_roles/data.json"))
	assertVocabRequires(t, extractBundleFile(t, cached.Data, "vocabulary/data.json"))
}

func TestCompileGateway_EmitsPrincipalRolesAndVocabulary(t *testing.T) {
	cfg := Config{JWKSURL: "http://127.0.0.1:1/jwks", Issuer: "orbit", Audience: "orbit-platform", FrontendHost: "orbit.example.com"}
	compiler := NewCompiler(cfg, routes.NewStore())

	cached, err := compiler.CompileGateway(
		[]string{"acme"},
		nil,
		nil,
		delegatorDataVocab(),
		map[string]map[string][]string{"acme": {"user-1": {"analyst", "engineer"}}},
	)
	if err != nil {
		t.Fatalf("compile gateway: %v", err)
	}

	assertPrincipalRolesDoc(t, extractBundleFile(t, cached.Data, "tenants/acme/principal_roles/data.json"))
	assertVocabRequires(t, extractBundleFile(t, cached.Data, "vocabulary/data.json"))
}

// A tenant with no user carrying roles still gets a well-formed (empty)
// principal_roles document, keeping rego lookups undefined-safe.
func TestCompile_EmptyPrincipalRoles(t *testing.T) {
	cfg := Config{JWKSURL: "http://127.0.0.1:1/jwks", Issuer: "orbit", Audience: "orbit-platform"}
	compiler := NewCompiler(cfg, routes.NewStore())

	cached, err := compiler.Compile("acme", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var doc struct {
		PrincipalRoles map[string][]string `json:"principal_roles"`
	}
	raw := extractBundleFile(t, cached.Data, "principal_roles/data.json")
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal principal_roles doc: %v", err)
	}
	if doc.PrincipalRoles == nil || len(doc.PrincipalRoles) != 0 {
		t.Errorf("expected an empty (non-null) principal_roles map, got %v", doc.PrincipalRoles)
	}
}
