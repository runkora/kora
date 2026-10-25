package toolgateway

import (
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
)

func TestNormalizeSegment(t *testing.T) {
	cases := map[string]string{
		"agents":      "agents",
		"Agents":      "agents",
		"create_run":  "create_run",
		"foo.bar":     "foo-bar", // "." is split BEFORE normalize; a stray "." here folds
		"weird name!": "weird-name-",
		"":            "_",
	}
	for in, want := range cases {
		if got := normalizeSegment(in); got != want {
			t.Errorf("normalizeSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolScopePath(t *testing.T) {
	got := toolScopePath("kora", "agents.create")
	want := []string{"kora", "agents", "create"}
	if len(got) != len(want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// scopeSet collects the scope strings from declarations for assertions.
func scopeSet(decls []routescopes.ScopeDeclaration) map[string]routescopes.ScopeDeclaration {
	m := make(map[string]routescopes.ScopeDeclaration, len(decls))
	for _, d := range decls {
		m[d.Scope] = d
	}
	return m
}

func TestBuildScopes_MultiSegmentRollups(t *testing.T) {
	snaps := []ServerSnapshot{{
		Server: Server{TenantID: "", Name: "kora"},
		State:  StateOK,
		Tools: []Tool{
			{Name: "agents.list"},
			{Name: "agents.create"},
			{Name: "models.list"},
		},
	}}

	decls, collisions := BuildScopes(snaps)
	if len(collisions) != 0 {
		t.Fatalf("unexpected collisions: %+v", collisions)
	}
	got := scopeSet(decls)

	want := []string{
		"mcp:invoke",                 // coarse gate
		"mcp:kora:use",               // server rollup
		"mcp:kora:agents:use",        // domain rollup
		"mcp:kora:agents:list:use",   // leaf
		"mcp:kora:agents:create:use", // leaf
		"mcp:kora:models:use",        // domain rollup
		"mcp:kora:models:list:use",   // leaf
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("missing scope %q; got %v", w, keys(got))
		}
	}

	// The agents rollup must carry the right resource_type + path for coverage.
	rollup := got["mcp:kora:agents:use"]
	if rollup.ResourceType != "mcp" || rollup.Action != "use" || len(rollup.PathSegments) != 2 ||
		rollup.PathSegments[0] != "kora" || rollup.PathSegments[1] != "agents" {
		t.Errorf("agents rollup decl wrong: %+v", rollup)
	}

	// agents.* rollup must be deduped (generated once though two tools share it).
	count := 0
	for _, d := range decls {
		if d.Scope == "mcp:kora:agents:use" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("agents rollup appeared %d times, want 1 (dedup)", count)
	}
}

func TestBuildScopes_LeafPrefixCollision(t *testing.T) {
	snaps := []ServerSnapshot{{
		Server: Server{TenantID: "acme", Name: "search-svc"},
		State:  StateOK,
		Tools: []Tool{
			{Name: "search"},          // collides: its scope == rollup of search.advanced
			{Name: "search.advanced"}, // the longer path
		},
	}}

	decls, collisions := BuildScopes(snaps)

	if len(collisions) != 1 {
		t.Fatalf("want 1 collision, got %d: %+v", len(collisions), collisions)
	}
	c := collisions[0]
	if c.Tool != "search" || c.ShadowedBy != "search.advanced" {
		t.Errorf("collision = %+v, want search shadowed by search.advanced", c)
	}

	got := scopeSet(decls)
	// The longer tool's leaf + its rollup (== the collider's string) still exist;
	// the collider's OWN leaf is just not separately registered.
	if _, ok := got["mcp:search-svc:search:advanced:use"]; !ok {
		t.Error("expected leaf for search.advanced")
	}
	if _, ok := got["mcp:search-svc:search:use"]; !ok {
		t.Error("expected the search rollup (from search.advanced) to still exist")
	}
}

func TestGroupByTenant(t *testing.T) {
	snaps := []ServerSnapshot{
		{Server: Server{TenantID: "", Name: "kora"}},
		{Server: Server{TenantID: "acme", Name: "slack"}},
		{Server: Server{TenantID: "globex", Name: "slack"}},
	}

	groups := groupByTenant(snaps)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3 (global, acme, globex)", len(groups))
	}
	for tenant, wantServer := range map[string]string{"": "kora", "acme": "slack", "globex": "slack"} {
		g := groups[tenant]
		if len(g) != 1 || g[0].Server.Name != wantServer {
			t.Errorf("group %q = %+v, want one server named %q", tenant, g, wantServer)
		}
	}
}

func TestGroupByTenant_GlobalGroupAlwaysPresent(t *testing.T) {
	groups := groupByTenant([]ServerSnapshot{{Server: Server{TenantID: "acme", Name: "slack"}}})

	g, ok := groups[""]
	if !ok {
		t.Fatal("global group missing; it must exist to carry mcp:invoke")
	}
	if len(g) != 0 {
		t.Errorf("global group has %d servers, want 0", len(g))
	}
}

// Publish builds scopes per tenant group, so same-named servers in different
// tenants each keep their own declarations instead of collapsing.
func TestBuildScopes_PerTenantGroupsKeepOwnDescriptions(t *testing.T) {
	snaps := []ServerSnapshot{
		{
			Server: Server{TenantID: "acme", Name: "slack"},
			State:  StateOK,
			Tools:  []Tool{{Name: "messages.post", Description: "from Acme"}},
		},
		{
			Server: Server{TenantID: "globex", Name: "slack"},
			State:  StateOK,
			Tools:  []Tool{{Name: "messages.post", Description: "from Globex"}},
		},
	}

	groups := groupByTenant(snaps)
	for tenant, want := range map[string]string{"acme": "from Acme", "globex": "from Globex"} {
		decls, _ := BuildScopes(groups[tenant])
		leaf := scopeSet(decls)["mcp:slack:messages:post:use"]
		if !strings.Contains(leaf.Description, want) {
			t.Errorf("tenant %q leaf description = %q, want it to contain %q", tenant, leaf.Description, want)
		}
	}
}

func keys(m map[string]routescopes.ScopeDeclaration) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The leaf requires tri-state decides whether a publish sets, clears or keeps
// the stored edges. nil keeps them; a non-nil empty slice clears them.
func TestBuildScopes_LeafRequires(t *testing.T) {
	tool := Tool{Name: "agents.list", RequiresScopes: []string{"agents:types:read"}}
	bare := Tool{Name: "agents.list"}
	tests := []struct {
		name     string
		tenantID string
		state    ServerState
		tool     Tool
		want     []string
	}{
		{name: "global ok publishes declared requires", state: StateOK, tool: tool, want: []string{"agents:types:read"}},
		{name: "global ok with none declares empty", state: StateOK, tool: bare, want: []string{}},
		{name: "global degraded is unknown", state: StateDegraded, tool: tool, want: nil},
		{name: "tenant server never sets or clears", tenantID: "acme", state: StateOK, tool: tool, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decls, _ := BuildScopes([]ServerSnapshot{{
				Server: Server{TenantID: tt.tenantID, Name: "kora"},
				State:  tt.state,
				Tools:  []Tool{tt.tool},
			}})
			got := scopeSet(decls)["mcp:kora:agents:list:use"].Requires
			if (got == nil) != (tt.want == nil) || !slices.Equal(got, tt.want) {
				t.Errorf("leaf requires = %#v, want %#v", got, tt.want)
			}
			if rollup := scopeSet(decls)["mcp:kora:agents:use"].Requires; rollup != nil {
				t.Errorf("rollup requires = %#v, want nil", rollup)
			}
		})
	}
}
