package agents

import (
	"context"
	"slices"
	"testing"
)

// TestConnectorScopes_ResourceQualifiers locks the grant compilation of
// repository/resource-qualified connectors
// (docs/adr/2026-07-22-connection-scope-qualifiers.md): an unrestricted
// connector compiles unqualified scopes (the covering form), a restricted one
// compiles the qualifier into each category action, and single-word actions
// take no qualifier.
func TestConnectorScopes_ResourceQualifiers(t *testing.T) {
	at := AgentType{Connectors: []Connector{
		{Connection: "github", Actions: []string{"issues:write", "contents:read"}, Resources: []string{"Kora", " webapp "}},
		{Connection: "public-api", Actions: []string{"use"}, Resources: []string{"ignored"}},
		{Connection: "gh-all", Actions: []string{"issues:read"}},
	}}

	got := at.ConnectorScopes()
	want := []string{
		"connection:github:issues:kora:write",
		"connection:github:issues:webapp:write",
		"connection:github:contents:kora:read",
		"connection:github:contents:webapp:read",
		// single-word action: qualifier not applicable, stays unqualified
		"connection:public-api:use",
		// no resources: unqualified — covers every qualified form by prefix
		"connection:gh-all:issues:read",
	}
	if !slices.Equal(got, want) {
		t.Errorf("ConnectorScopes() = %v, want %v", got, want)
	}
}

// TestMergeConnectors_ResourceUnion locks the merge semantics: resource lists
// union, and any all-resources contribution (empty Resources) dominates.
func TestMergeConnectors_ResourceUnion(t *testing.T) {
	merged := mergeConnectors([]Connector{
		{Connection: "github", Actions: []string{"issues:write"}, Resources: []string{"kora"}},
		{Connection: "github", Actions: []string{"contents:read"}, Resources: []string{"webapp", "kora"}},
		{Connection: "cluster", Actions: []string{"workloads:read"}, Resources: []string{"staging"}},
		{Connection: "cluster", Actions: []string{"workloads:read"}}, // all resources — dominates
	})

	if len(merged) != 2 {
		t.Fatalf("merged = %v, want 2 connectors", merged)
	}
	gh := merged[0]
	if gh.Connection != "github" || !slices.Equal(gh.Resources, []string{"kora", "webapp"}) {
		t.Errorf("github merged = %+v, want resources [kora webapp]", gh)
	}
	cl := merged[1]
	if cl.Connection != "cluster" || cl.Resources != nil {
		t.Errorf("cluster merged = %+v, want nil resources (all-resources dominates)", cl)
	}
}

// stubConnResolver implements connectionResolver for baseline tests.
type stubConnResolver struct {
	adapter  string
	baseHost string
}

func (s stubConnResolver) ResolveConnection(context.Context, string, string) (string, string, bool, error) {
	return s.adapter, s.baseHost, true, nil
}

// TestEffectiveScopes_BaselineActions locks baseline injection: a github_app
// connector attachment always confers the unqualified metadata:read alongside
// its (possibly qualified) selections, because vocabulary requires edges cannot
// reach qualified grant scopes.
func TestEffectiveScopes_BaselineActions(t *testing.T) {
	r := NewScopeResolver(nil, stubConnResolver{adapter: "github_app", baseHost: "github.com"})
	at := AgentType{
		TenantID: "acme",
		Connectors: []Connector{
			{Connection: "github", Actions: []string{"issues:write"}, Resources: []string{"kora"}},
		},
	}

	scopes, err := r.EffectiveScopes(context.Background(), at)
	if err != nil {
		t.Fatalf("EffectiveScopes: %v", err)
	}

	for _, want := range []string{
		"connection:github:issues:kora:write",
		"connection:github:metadata:read", // baseline, unqualified
		"egress:github.com:connector",     // connector auto-egress
	} {
		if !slices.Contains(scopes, want) {
			t.Errorf("EffectiveScopes missing %q (got %v)", want, scopes)
		}
	}
	// Connector egress must never read as the agent's own posture.
	if slices.Contains(scopes, "egress:github.com:connect") {
		t.Errorf("connector auto-egress compiled to an own-posture scope: %v", scopes)
	}

	// A connector granting nothing gets no baseline.
	empty := AgentType{TenantID: "acme", Connectors: []Connector{{Connection: "github"}}}
	scopes, err = r.EffectiveScopes(context.Background(), empty)
	if err != nil {
		t.Fatalf("EffectiveScopes(empty): %v", err)
	}
	if slices.Contains(scopes, "connection:github:metadata:read") {
		t.Errorf("empty connector must not confer the baseline, got %v", scopes)
	}
}

// TestValidateConnectors_Resources locks the resource rule against the
// qualifier grammar the policy service accepts: one segment, no separators
// (docs/adr/2026-07-22-connection-scope-qualifiers.md). The rule applies to
// any adapter with a resource dimension, not just GitHub repositories — a
// GitHub "owner/repo" value used to compile to
// connection:<name>:<category>:<owner>/<repo>:<verb>, which the grant API
// rejects, so every run start for the agent failed with a 500 long after the
// attachment was accepted.
func TestValidateConnectors_Resources(t *testing.T) {
	cases := []struct {
		name      string
		resources []string
		wantErr   bool
	}{
		{name: "bare repository name", resources: []string{"constellation-iac"}},
		{name: "mixed case is lowercased at compile time", resources: []string{"Constellation-IAC"}},
		{name: "several resources", resources: []string{"kora", "webapp"}},
		{name: "no resources", resources: nil},
		{name: "kubernetes namespace, a different adapter's resource shape", resources: []string{"staging"}},
		{name: "owner/repo", resources: []string{"clear-route/constellation-iac"}, wantErr: true},
		{name: "scope separator", resources: []string{"repo:read"}, wantErr: true},
		{name: "wildcard", resources: []string{"*"}, wantErr: true},
		{name: "whitespace inside", resources: []string{"two words"}, wantErr: true},
		{name: "empty entry", resources: []string{""}, wantErr: true},
		{name: "blank entry", resources: []string{"   "}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConnectors([]Connector{
				{Connection: "github", Actions: []string{"contents:read"}, Resources: tc.resources},
			})
			if tc.wantErr && err == nil {
				t.Fatalf("resources %v were accepted, want a validation error", tc.resources)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("resources %v rejected: %v", tc.resources, err)
			}
		})
	}
}
