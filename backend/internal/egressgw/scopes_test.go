package egressgw

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestAllScopes_NoConnections_PublishesReadScope(t *testing.T) {
	registry := NewAdapterRegistry()
	got := AllScopes(nil, registry)
	// With no connections, AllScopes still publishes the read scope guarding
	// the read-only connections catalog (GET /api/v1/connections) so the
	// policy-service registration has a non-empty vocabulary bound to its route.
	if len(got) != 1 {
		t.Fatalf("scopes = %d, want 1 (platform:connections:read, no connections) — got %v", len(got), scopeStrings(got))
	}
	if got[0].Scope != "platform:connections:read" {
		t.Errorf("scope[0] = %q, want platform:connections:read", got[0].Scope)
	}
}

func TestReadRoutes_BindsConnectionsRoute(t *testing.T) {
	routes := ReadRoutes()
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	r := routes[0]
	if r.PathPrefix != "/api/v1/connections" || r.Method != "GET" || r.RequiredScope != "platform:connections:read" {
		t.Errorf("route = %+v, want {/api/v1/connections GET platform:connections:read}", r)
	}
}

func TestAllScopes_PerConnectionScopes(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	gh := githubapp.New(ghclient.NewClient("https://example", logger), nil, logger)
	registry := NewAdapterRegistry()
	registry.Register(gh)

	conns := []*credentials.Connection{
		{ID: "1", TenantID: "acme", Name: "github-prod", Adapter: credentials.AdapterGitHubApp, Config: json.RawMessage(`{}`)},
		{ID: "2", TenantID: "acme", Name: "github-staging", Adapter: credentials.AdapterGitHubApp, Config: json.RawMessage(`{}`)},
	}
	got := AllScopes(conns, registry)

	// AllScopes prepends the read scope (platform:connections:read), then the
	// per-connection scopes sorted lexicographically by Scope: wildcard +
	// repo:read + repo:write, for each of two connections = 6, plus the read
	// scope = 7 total.
	wantScopes := []string{
		"platform:connections:read",
		"connection:github-prod:*",
		"connection:github-prod:repo:read",
		"connection:github-prod:repo:write",
		"connection:github-staging:*",
		"connection:github-staging:repo:read",
		"connection:github-staging:repo:write",
	}
	if len(got) != len(wantScopes) {
		t.Fatalf("scopes = %d, want %d (got: %v)", len(got), len(wantScopes), scopeStrings(got))
	}
	for i, want := range wantScopes {
		if got[i].Scope != want {
			t.Errorf("scope[%d] = %q, want %q", i, got[i].Scope, want)
		}
	}

	// The connection scopes carry their tenant-friendly metadata.
	for _, d := range got {
		if d.Scope == "connection:github-prod:repo:write" {
			if d.ResourceType != "connection" || len(d.PathSegments) != 1 || d.PathSegments[0] != "github-prod" || d.Action != "repo:write" {
				t.Errorf("structure = %+v, want resource=connection path=[github-prod] action=repo:write", d)
			}
		}
	}
}

func TestConnectionScopes_SkipsUnregisteredAdapter(t *testing.T) {
	registry := NewAdapterRegistry() // empty — no adapters registered
	conns := []*credentials.Connection{
		{ID: "1", TenantID: "acme", Name: "ghost", Adapter: credentials.AdapterGitHubApp, Config: json.RawMessage(`{}`)},
	}
	got := ConnectionScopes(conns, registry)
	if len(got) != 0 {
		t.Errorf("got %d scopes, want 0 — connections with unregistered adapters must be skipped", len(got))
	}
}

func TestConnectionScopes_NilRegistry(t *testing.T) {
	if got := ConnectionScopes([]*credentials.Connection{{Name: "x"}}, nil); got != nil {
		t.Errorf("nil registry: got %v, want nil", got)
	}
}

func TestConnectionScopes_DedupeOnDuplicateConnectionName(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	gh := githubapp.New(ghclient.NewClient("https://example", logger), nil, logger)
	registry := NewAdapterRegistry()
	registry.Register(gh)

	// Two connection rows with the same name. Defensive against duplicates;
	// in production (tenant_id, name) is unique but the helper must still
	// produce a deterministic, deduped output if fed a list.
	conns := []*credentials.Connection{
		{ID: "1", TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp},
		{ID: "2", TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp},
	}
	got := ConnectionScopes(conns, registry)

	// Expect 3 unique entries: wildcard + repo:read + repo:write.
	wantCount := 3
	if len(got) != wantCount {
		t.Errorf("scopes = %d, want %d (got: %v)", len(got), wantCount, scopeStrings(got))
	}
}

func scopeStrings(decls []routescopes.ScopeDeclaration) []string {
	out := make([]string, 0, len(decls))
	for _, d := range decls {
		out = append(out, d.Scope)
	}
	return out
}
