package egressgw

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestAllScopes_NoConnections_PublishesStaticScopes(t *testing.T) {
	registry := NewAdapterRegistry()
	got := AllScopes(nil, registry)
	// With no connections, AllScopes still publishes the static platform
	// connection scopes (read guards GET /api/v1/connections; write is the
	// reserved-role management scope) so the policy-service vocabulary contains
	// them even when no connections are mounted.
	if len(got) != 2 {
		t.Fatalf("scopes = %d, want 2 (platform:connections:read + :write, no connections), got %v", len(got), scopeStrings(got))
	}
	if got[0].Scope != "platform:connections:read" {
		t.Errorf("scope[0] = %q, want platform:connections:read", got[0].Scope)
	}
	if got[1].Scope != "platform:connections:write" {
		t.Errorf("scope[1] = %q, want platform:connections:write", got[1].Scope)
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

	// AllScopes prepends the static platform connection scopes (read + write),
	// then per connection: the two covering parents (connection:<n>:read/write)
	// plus the adapter's 15 category/catch-all actions = 17 each, 36 total.
	// No wildcard entry: connection:<n>:* matched nothing under scope_covers
	// and is no longer published.
	const perConn = 17
	if want := 2 + 2*perConn; len(got) != want {
		t.Fatalf("scopes = %d, want %d (got: %v)", len(got), want, scopeStrings(got))
	}
	byScope := map[string]int{}
	for _, d := range got {
		byScope[d.Scope]++
		if strings.HasSuffix(d.Scope, ":*") {
			t.Errorf("wildcard entry %q must no longer be published", d.Scope)
		}
	}
	for _, want := range []string{
		"connection:github-prod:read",
		"connection:github-prod:write",
		"connection:github-prod:issues:write",
		"connection:github-prod:metadata:read",
		"connection:github-prod:repo:write",
		"connection:github-staging:contents:read",
	} {
		if byScope[want] != 1 {
			t.Errorf("scope %q published %d times, want exactly once", want, byScope[want])
		}
	}

	// The connection scopes carry their tenant-friendly metadata and requires
	// edges: the legacy repo:write umbrella confers every category write, and
	// category actions confer the metadata read.
	for _, d := range got {
		switch d.Scope {
		case "connection:github-prod:repo:write":
			// Metadata matches the scope grammar: path = [name, category],
			// action = the final verb, not path=[name], action="repo:write".
			if d.ResourceType != "connection" || len(d.PathSegments) != 2 ||
				d.PathSegments[0] != "github-prod" || d.PathSegments[1] != "repo" || d.Action != "write" {
				t.Errorf("structure = %+v, want resource=connection path=[github-prod repo] action=write", d)
			}
			if !slices.Contains(d.Requires, "connection:github-prod:issues:write") ||
				!slices.Contains(d.Requires, "connection:github-prod:contents:write") {
				t.Errorf("repo:write requires = %v, want category writes conferred", d.Requires)
			}
		case "connection:github-prod:issues:write":
			if !slices.Contains(d.Requires, "connection:github-prod:metadata:read") {
				t.Errorf("issues:write requires = %v, want metadata:read conferred", d.Requires)
			}
		}
	}
}

func TestConnectionScopes_SkipsUnregisteredAdapter(t *testing.T) {
	registry := NewAdapterRegistry() // empty: no adapters registered
	conns := []*credentials.Connection{
		{ID: "1", TenantID: "acme", Name: "ghost", Adapter: credentials.AdapterGitHubApp, Config: json.RawMessage(`{}`)},
	}
	got := ConnectionScopes(conns, registry)
	if len(got) != 0 {
		t.Errorf("got %d scopes, want 0: connections with unregistered adapters must be skipped", len(got))
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

	// Expect one set of unique entries: 2 covering parents + the adapter's 15
	// actions: duplicates from the repeated connection name are dropped.
	wantCount := 17
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
