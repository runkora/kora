package egressgw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/githubapp"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// lookupAdapter opens real github_app connectors whose node id lookup is served
// from visible: what each connection's installation can see, keyed by
// connection name, then node id, to owner login.
type lookupAdapter struct {
	credentials.Adapter
	visible    map[string]map[string]string
	err        error
	lookups    int
	categories [][]string
}

type lookupConnector struct {
	credentials.Connector
	ownerScoped
	name    string
	adapter *lookupAdapter
}

func (a *lookupAdapter) Open(conn credentials.Connection) (credentials.Connector, error) {
	c, err := a.Adapter.Open(conn)
	if err != nil {
		return nil, err
	}
	return &lookupConnector{Connector: c, ownerScoped: c.(ownerScoped), name: conn.Name, adapter: a}, nil
}

func (c *lookupConnector) NodeOwners(_ context.Context, ids, categories []string) (map[string]string, error) {
	c.adapter.lookups++
	c.adapter.categories = append(c.adapter.categories, categories)
	if c.adapter.err != nil {
		return nil, c.adapter.err
	}
	out := map[string]string{}
	for _, id := range ids {
		if owner, ok := c.adapter.visible[c.name][id]; ok {
			out[id] = owner
		}
	}
	return out, nil
}

func lookupBroker(pol *selectionPolicy, conns candidateConns, la *lookupAdapter) *Broker {
	logger := logging.NewLoggerWithOutput(io.Discard)
	la.Adapter = githubapp.New(nil, nil, logger)
	adapters := NewAdapterRegistry()
	adapters.Register(la)
	return NewBroker(pol, conns, adapters, passthrough.New(), logger, nil)
}

func graphqlRequest(body string) EgressRequest {
	r := httptest.NewRequest(http.MethodPost, "https://api.github.com/graphql", strings.NewReader(body))
	return EgressRequest{Host: r.Host, Method: http.MethodPost, URL: r.URL, HTTPRequest: r}
}

func searchRequest(q string) EgressRequest {
	r := httptest.NewRequest(http.MethodGet, "https://api.github.com/search/issues?"+url.Values{"q": {q}}.Encode(), nil)
	return EgressRequest{Host: r.Host, Method: http.MethodGet, URL: r.URL, HTTPRequest: r}
}

func TestResolveConnection_OwnerResolution(t *testing.T) {
	contoso := githubConn(t, "contoso-github", "contoso")
	fabrikam := githubConn(t, "fabrikam-github", "fabrikam")
	both := candidateConns{contoso, fabrikam}
	holdsBoth := []string{"connection:contoso-github:repo:read", "connection:fabrikam-github:repo:read"}
	visible := map[string]map[string]string{
		"contoso-github":  {"PR_contoso": "contoso", "R_public": "fabrikam"},
		"fabrikam-github": {"PR_fabrikam": "fabrikam", "R_public": "fabrikam"},
	}
	nodeDoc := func(ids ...string) string {
		return `{"query":"{ nodes(ids: [\"` + strings.Join(ids, `\", \"`) + `\"]) { id } }"}`
	}

	tests := []struct {
		name        string
		conns       candidateConns
		scopes      []string
		req         EgressRequest
		lookupErr   error
		want        string
		wantInMsg   string
		wantLookups int
	}{
		{name: "graphql owner selects fabrikam", conns: both, scopes: holdsBoth, req: graphqlRequest(`{"query":"{ repository(owner: \"fabrikam\", name: \"widgets\") { id } }"}`), want: "fabrikam-github"},
		{name: "graphql owner selects contoso", conns: both, scopes: holdsBoth, req: graphqlRequest(`{"query":"{ organization(login: \"Contoso\") { id } }"}`), want: "contoso-github"},
		{name: "graphql owners spanning candidates denied", conns: both, scopes: holdsBoth, req: graphqlRequest(`{"query":"{ a: repository(owner: \"fabrikam\", name: \"widgets\") { id } b: organization(login: \"contoso\") { id } }"}`), wantInMsg: "served by different connections"},
		{name: "node id lookup selects fabrikam", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_fabrikam")), want: "fabrikam-github", wantLookups: 2},
		{name: "node id lookup selects contoso", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_contoso")), want: "contoso-github", wantLookups: 1},
		{name: "public node selected by its owner", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("R_public")), want: "fabrikam-github", wantLookups: 1},
		{name: "unresolvable id denied", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_unknown")), wantInMsg: "none of its node ids resolved", wantLookups: 2},
		{name: "resolvable id beside an unresolvable string", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("garbage", "PR_fabrikam")), want: "fabrikam-github", wantLookups: 2},
		{name: "ids spanning candidates denied", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_contoso", "PR_fabrikam")), wantInMsg: "served by different connections", wantLookups: 2},
		{name: "failed lookup denied", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_fabrikam")), lookupErr: errors.New("github down"), wantInMsg: "node id lookup failed", wantLookups: 1},
		{name: "viewer only stays unresolved", conns: both, scopes: holdsBoth, req: graphqlRequest(`{"query":"{ viewer { login } }"}`), wantInMsg: "GraphQL document names no owner"},
		{name: "one held candidate needs no lookup", conns: both, scopes: holdsBoth[1:], req: graphqlRequest(nodeDoc("PR_contoso")), want: "fabrikam-github"},
		{name: "single-connection tenant needs no lookup", conns: candidateConns{fabrikam}, req: graphqlRequest(nodeDoc("PR_contoso")), want: "fabrikam-github"},
		{name: "search repo qualifier", conns: both, scopes: holdsBoth, req: searchRequest("repo:fabrikam/widgets is:pr"), want: "fabrikam-github"},
		{name: "search org qualifier", conns: both, scopes: holdsBoth, req: searchRequest("org:contoso bug"), want: "contoso-github"},
		{name: "search qualifiers spanning candidates denied", conns: both, scopes: holdsBoth, req: searchRequest("org:contoso repo:fabrikam/widgets"), wantInMsg: "served by different connections"},
		{name: "search without qualifier stays unresolved", conns: both, scopes: holdsBoth, req: searchRequest("bug is:open"), wantInMsg: "search query names no owner"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			la := &lookupAdapter{visible: visible, err: tc.lookupErr}
			b := lookupBroker(&selectionPolicy{scopes: tc.scopes}, tc.conns, la)
			conn, _, err := b.resolveConnection(context.Background(), acmeClaims, tc.req)
			if la.lookups != tc.wantLookups {
				t.Errorf("lookups = %d, want %d", la.lookups, tc.wantLookups)
			}
			if tc.want != "" {
				if err != nil || conn.Name != tc.want {
					t.Fatalf("resolved %v (err %v), want %s", conn, err, tc.want)
				}
				return
			}
			var d *Denial
			if !errors.As(err, &d) || d.Reason != DenialConnectionUnresolved {
				t.Fatalf("err = %v, want a connection_unresolved denial", err)
			}
			if !strings.Contains(d.Message("kora"), tc.wantInMsg) {
				t.Errorf("message %q does not contain %q", d.Message("kora"), tc.wantInMsg)
			}
		})
	}
}

// A resolved node id is cached, so the next request needs no lookup.
func TestResolveConnection_NodeOwnerCacheHit(t *testing.T) {
	both := candidateConns{githubConn(t, "contoso-github", "contoso"), githubConn(t, "fabrikam-github", "fabrikam")}
	la := &lookupAdapter{visible: map[string]map[string]string{"contoso-github": {"PR_contoso": "contoso"}}}
	b := lookupBroker(&selectionPolicy{scopes: []string{"connection:contoso-github:repo:read", "connection:fabrikam-github:repo:read"}}, both, la)
	for range 2 {
		conn, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"PR_contoso\") { id } }"}`))
		if err != nil || conn.Name != "contoso-github" {
			t.Fatalf("resolved %v, err %v", conn, err)
		}
	}
	if la.lookups != 1 {
		t.Errorf("lookups = %d, want 1", la.lookups)
	}
}

// Owners resolved from node ids select only among the caller's held connections
// and never appear in a denial, so a caller learns nothing its own connections
// could not tell it.
func TestResolveConnection_NodeOwnersDoNotLeak(t *testing.T) {
	three := candidateConns{
		githubConn(t, "contoso-github", "contoso"),
		githubConn(t, "fabrikam-github", "fabrikam"),
		githubConn(t, "acme-github", "acme"),
	}
	visible := map[string]map[string]string{
		"contoso-github":  {"PR_contoso": "contoso"},
		"fabrikam-github": {"PR_fabrikam": "fabrikam", "R_contosopublic": "contoso"},
		"acme-github":     {"PR_acme": "acme"},
	}
	holdsAll := []string{"connection:contoso-github:repo:read", "connection:fabrikam-github:repo:read", "connection:acme-github:repo:read"}
	holdsFabrikamAcme := []string{"connection:fabrikam-github:repo:read", "connection:acme-github:repo:read"}
	la := &lookupAdapter{visible: visible}
	logger := logging.NewLoggerWithOutput(io.Discard)
	la.Adapter = githubapp.New(nil, nil, logger)
	adapters := NewAdapterRegistry()
	adapters.Register(la)
	pol := &selectionPolicy{scopes: holdsAll}
	b := NewBroker(pol, three, adapters, passthrough.New(), logger, nil)

	denial := func(t *testing.T, err error) *Denial {
		t.Helper()
		var d *Denial
		if !errors.As(err, &d) || d.Reason != DenialConnectionUnresolved {
			t.Fatalf("err = %v, want a connection_unresolved denial", err)
		}
		for _, owner := range []string{"contoso ", "fabrikam ", "acme "} {
			if strings.Contains(d.Unresolved+" ", owner) {
				t.Errorf("denial names a resolved owner: %q", d.Unresolved)
			}
		}
		return d
	}

	// A caller holding every connection resolves PR_contoso through contoso-github.
	if conn, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"PR_contoso\") { id } }"}`)); err != nil || conn.Name != "contoso-github" {
		t.Fatalf("resolved %v, err %v", conn, err)
	}

	t.Run("cache entry from a connection the caller does not hold is unused", func(t *testing.T) {
		pol.scopes, la.lookups = holdsFabrikamAcme, 0
		_, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"PR_contoso\") { id } }"}`))
		denial(t, err)
		if la.lookups != 2 {
			t.Errorf("lookups = %d, want the caller's own 2", la.lookups)
		}
	})
	t.Run("owner resolved to a connection the caller does not hold", func(t *testing.T) {
		pol.scopes = holdsFabrikamAcme
		_, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"R_contosopublic\") { id } }"}`))
		if d := denial(t, err); !strings.Contains(d.Unresolved, "none of the agent's connections") {
			t.Errorf("unresolved = %q", d.Unresolved)
		}
	})
	t.Run("node ids spanning held connections name no owner", func(t *testing.T) {
		pol.scopes = holdsFabrikamAcme
		_, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ nodes(ids: [\"PR_fabrikam\", \"PR_acme\"]) { id } }"}`))
		d := denial(t, err)
		if body := d.Message("kora"); strings.Contains(body, "fabrikam,") || strings.Contains(body, "acme from") || !strings.Contains(d.Unresolved, "different connections") {
			t.Errorf("denial body names owners: %q", body)
		}
	})
}

func TestResolveConnection_OwnerResolutionEdges(t *testing.T) {
	both := candidateConns{githubConn(t, "contoso-github", "contoso"), githubConn(t, "fabrikam-github", "fabrikam")}
	holdsBoth := []string{"connection:contoso-github:repo:read", "connection:fabrikam-github:repo:read"}
	many := make([]string, MaxNodeIDs+1)
	for i := range many {
		many[i] = fmt.Sprintf("PR_%d", i)
	}
	tests := []struct {
		name          string
		scopes        []string
		req           EgressRequest
		want          string
		wantAmbiguous bool
		wantInMsg     string
	}{
		{name: "unserved owner with one held candidate uses it", scopes: holdsBoth[1:], req: searchRequest("org:facebook bug"), want: "fabrikam-github"},
		{name: "unserved owner with two held candidates is unresolved", scopes: holdsBoth, req: searchRequest("org:facebook bug"), wantInMsg: "owner facebook from the search query is served by no connection"},
		{name: "parenthesised qualifiers spanning candidates denied", scopes: holdsBoth, req: searchRequest("(org:fabrikam OR org:contoso) bug"), wantAmbiguous: true, wantInMsg: "different connections"},
		{name: "too many node ids denied", scopes: holdsBoth, req: graphqlRequest(`{"query":"{ nodes(ids: [\"` + strings.Join(many, `\", \"`) + `\"]) { id } }"}`), wantAmbiguous: true, wantInMsg: "more than 100 node ids"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			la := &lookupAdapter{}
			b := lookupBroker(&selectionPolicy{scopes: tc.scopes}, both, la)
			conn, _, err := b.resolveConnection(context.Background(), acmeClaims, tc.req)
			if tc.want != "" {
				if err != nil || conn.Name != tc.want {
					t.Fatalf("resolved %v (err %v), want %s", conn, err, tc.want)
				}
				return
			}
			var d *Denial
			if !errors.As(err, &d) || d.Reason != DenialConnectionUnresolved || d.ambiguous != tc.wantAmbiguous {
				t.Fatalf("err = %v, want a connection_unresolved denial (ambiguous %v)", err, tc.wantAmbiguous)
			}
			if !strings.Contains(d.Unresolved, tc.wantInMsg) || la.lookups != 0 {
				t.Errorf("unresolved %q, lookups %d", d.Unresolved, la.lookups)
			}
		})
	}
}

// An id no held connection can see is remembered, so a repeat request does not
// look it up again.
func TestResolveConnection_UnseenNodeIDCached(t *testing.T) {
	both := candidateConns{githubConn(t, "contoso-github", "contoso"), githubConn(t, "fabrikam-github", "fabrikam")}
	la := &lookupAdapter{}
	b := lookupBroker(&selectionPolicy{scopes: []string{"connection:contoso-github:repo:read", "connection:fabrikam-github:repo:read"}}, both, la)
	for range 3 {
		if _, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"nope\") { id } }"}`)); err == nil {
			t.Fatal("want an unresolved denial")
		}
	}
	if la.lookups != 2 {
		t.Errorf("lookups = %d, want one per held connection", la.lookups)
	}
}

func TestOwnerCache_ExpiryAndBound(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	c := newOwnerCache(func() time.Time { return now })
	c.put("acme", "api.github.com", "gh", "PR_1", "fabrikam")
	if owner, ok := c.get("acme", "api.github.com", "gh", "PR_1"); !ok || owner != "fabrikam" {
		t.Fatalf("get = %q %v", owner, ok)
	}
	if _, ok := c.get("acme", "api.github.com", "other", "PR_1"); ok {
		t.Error("entry served for another connection")
	}
	now = now.Add(nodeOwnerTTL + time.Second)
	if _, ok := c.get("acme", "api.github.com", "gh", "PR_1"); ok {
		t.Error("expired entry served")
	}
	for i := range nodeOwnerEntries + 5 {
		c.put("acme", "api.github.com", "gh", fmt.Sprint(i), "fabrikam")
	}
	if len(c.entries) > nodeOwnerEntries {
		t.Errorf("entries = %d, want at most %d", len(c.entries), nodeOwnerEntries)
	}
}

// A lookup reads only the categories the caller holds without a repository
// qualifier, and a caller holding only qualified grants triggers none.
func TestResolveConnection_LookupLimitedToUnqualifiedCategories(t *testing.T) {
	both := candidateConns{githubConn(t, "contoso-github", "contoso"), githubConn(t, "fabrikam-github", "fabrikam")}
	visible := map[string]map[string]string{"fabrikam-github": {"PR_fabrikam": "fabrikam"}}
	doc := graphqlRequest(`{"query":"{ node(id: \"PR_fabrikam\") { id } }"}`)

	t.Run("qualified grants only", func(t *testing.T) {
		la := &lookupAdapter{visible: visible}
		b := lookupBroker(&selectionPolicy{scopes: []string{"connection:contoso-github:pulls:gadgets:read", "connection:fabrikam-github:pulls:widgets:read"}}, both, la)
		_, _, err := b.resolveConnection(context.Background(), acmeClaims, doc)
		var d *Denial
		if !errors.As(err, &d) || la.lookups != 0 {
			t.Fatalf("err %v, lookups %d, want an unresolved denial and no lookup", err, la.lookups)
		}
	})
	t.Run("categories passed to the lookup", func(t *testing.T) {
		la := &lookupAdapter{visible: visible}
		b := lookupBroker(&selectionPolicy{scopes: []string{"connection:contoso-github:metadata:read", "connection:fabrikam-github:metadata:read", "connection:fabrikam-github:pulls:read", "connection:fabrikam-github:issues:widgets:read"}}, both, la)
		conn, _, err := b.resolveConnection(context.Background(), acmeClaims, doc)
		if err != nil || conn.Name != "fabrikam-github" {
			t.Fatalf("resolved %v, err %v", conn, err)
		}
		if len(la.categories) != 2 || strings.Join(la.categories[0], ",") != "metadata" || strings.Join(la.categories[1], ",") != "metadata,pulls" {
			t.Errorf("lookup categories = %v", la.categories)
		}
	})
}
