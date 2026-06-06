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

func TestAdminRoutes_StableSet(t *testing.T) {
	got := AdminRoutes()
	if len(got) == 0 {
		t.Fatal("AdminRoutes returned empty")
	}
	want := map[string]string{
		"GET":    ScopePlatformConnectionsRead,
		"POST":   ScopePlatformConnectionsWrite,
		"PUT":    ScopePlatformConnectionsWrite,
		"DELETE": ScopePlatformConnectionsWrite,
	}
	for _, r := range got {
		if r.PathPrefix != "/api/v1/connections" {
			t.Errorf("path = %q, want /api/v1/connections", r.PathPrefix)
		}
		if w, ok := want[r.Method]; !ok || w != r.RequiredScope {
			t.Errorf("%s scope = %q, want %q", r.Method, r.RequiredScope, w)
		}
	}
}

func TestAllScopes_NoConnections_ReturnsAdminOnly(t *testing.T) {
	registry := NewAdapterRegistry()
	got := AllScopes(nil, registry)
	if len(got) != 2 {
		t.Fatalf("scopes = %d, want 2 admin scopes", len(got))
	}
	wantSet := map[string]bool{
		ScopePlatformConnectionsRead:  true,
		ScopePlatformConnectionsWrite: true,
	}
	for _, d := range got {
		if !wantSet[d.Scope] {
			t.Errorf("unexpected scope %q", d.Scope)
		}
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

	// Expect admin (2) + per-connection: wildcard + repo:read + repo:write,
	// for each of two connections = 6 + 2 admin = 8. AllScopes returns the full
	// set sorted lexicographically by Scope; "connection:" sorts before "platform:".
	wantScopes := []string{
		"connection:github-prod:*",
		"connection:github-prod:repo:read",
		"connection:github-prod:repo:write",
		"connection:github-staging:*",
		"connection:github-staging:repo:read",
		"connection:github-staging:repo:write",
		ScopePlatformConnectionsRead,
		ScopePlatformConnectionsWrite,
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
