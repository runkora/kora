package egressgw

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// lookupAdapter opens real github_app connectors whose node id lookup is served
// from visible: what each connection's installation can see, keyed by
// connection name, then node id, to owner login.
type lookupAdapter struct {
	credentials.Adapter
	visible map[string]map[string]string
	err     error
	lookups int
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

func (c *lookupConnector) NodeOwners(_ context.Context, ids []string) (map[string]string, error) {
	c.adapter.lookups++
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
	return NewBroker(pol, conns, adapters, passthrough.New(), logger)
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
	clearRoute := githubConn(t, "clear-route-github", "clear-route")
	runkora := githubConn(t, "runkora-github", "runkora")
	both := candidateConns{clearRoute, runkora}
	holdsBoth := []string{"connection:clear-route-github:repo:read", "connection:runkora-github:repo:read"}
	visible := map[string]map[string]string{
		"clear-route-github": {"PR_cr": "clear-route", "R_public": "runkora"},
		"runkora-github":     {"PR_rk": "runkora", "R_public": "runkora"},
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
		{name: "graphql owner selects runkora", conns: both, scopes: holdsBoth, req: graphqlRequest(`{"query":"{ repository(owner: \"runkora\", name: \"kora\") { id } }"}`), want: "runkora-github"},
		{name: "graphql owner selects clear-route", conns: both, scopes: holdsBoth, req: graphqlRequest(`{"query":"{ organization(login: \"Clear-Route\") { id } }"}`), want: "clear-route-github"},
		{name: "graphql owners spanning candidates denied", conns: both, scopes: holdsBoth, req: graphqlRequest(`{"query":"{ a: repository(owner: \"runkora\", name: \"kora\") { id } b: organization(login: \"clear-route\") { id } }"}`), wantInMsg: "served by different connections"},
		{name: "node id lookup selects runkora", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_rk")), want: "runkora-github", wantLookups: 2},
		{name: "node id lookup selects clear-route", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_cr")), want: "clear-route-github", wantLookups: 1},
		{name: "public node selected by its owner", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("R_public")), want: "runkora-github", wantLookups: 1},
		{name: "unresolvable id denied", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_unknown")), wantInMsg: "none of its node ids resolved", wantLookups: 2},
		{name: "resolvable id beside an unresolvable string", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("garbage", "PR_rk")), want: "runkora-github", wantLookups: 2},
		{name: "ids spanning candidates denied", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_cr", "PR_rk")), wantInMsg: "served by different connections", wantLookups: 2},
		{name: "failed lookup denied", conns: both, scopes: holdsBoth, req: graphqlRequest(nodeDoc("PR_rk")), lookupErr: errors.New("github down"), wantInMsg: "node id lookup failed", wantLookups: 1},
		{name: "viewer only stays unresolved", conns: both, scopes: holdsBoth, req: graphqlRequest(`{"query":"{ viewer { login } }"}`), wantInMsg: "GraphQL document names no owner"},
		{name: "one held candidate needs no lookup", conns: both, scopes: holdsBoth[1:], req: graphqlRequest(nodeDoc("PR_cr")), want: "runkora-github"},
		{name: "single-connection tenant needs no lookup", conns: candidateConns{runkora}, req: graphqlRequest(nodeDoc("PR_cr")), want: "runkora-github"},
		{name: "search repo qualifier", conns: both, scopes: holdsBoth, req: searchRequest("repo:runkora/kora is:pr"), want: "runkora-github"},
		{name: "search org qualifier", conns: both, scopes: holdsBoth, req: searchRequest("org:clear-route bug"), want: "clear-route-github"},
		{name: "search qualifiers spanning candidates denied", conns: both, scopes: holdsBoth, req: searchRequest("org:clear-route repo:runkora/kora"), wantInMsg: "served by different connections"},
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
			if !strings.Contains(d.Message(), tc.wantInMsg) {
				t.Errorf("message %q does not contain %q", d.Message(), tc.wantInMsg)
			}
		})
	}
}

// A resolved node id is cached, so the next request needs no lookup.
func TestResolveConnection_NodeOwnerCacheHit(t *testing.T) {
	both := candidateConns{githubConn(t, "clear-route-github", "clear-route"), githubConn(t, "runkora-github", "runkora")}
	la := &lookupAdapter{visible: map[string]map[string]string{"clear-route-github": {"PR_cr": "clear-route"}}}
	b := lookupBroker(&selectionPolicy{scopes: []string{"connection:clear-route-github:repo:read", "connection:runkora-github:repo:read"}}, both, la)
	for range 2 {
		conn, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"PR_cr\") { id } }"}`))
		if err != nil || conn.Name != "clear-route-github" {
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
		githubConn(t, "clear-route-github", "clear-route"),
		githubConn(t, "runkora-github", "runkora"),
		githubConn(t, "acme-github", "acme"),
	}
	visible := map[string]map[string]string{
		"clear-route-github": {"PR_cr": "clear-route"},
		"runkora-github":     {"PR_rk": "runkora", "R_crpublic": "clear-route"},
		"acme-github":        {"PR_acme": "acme"},
	}
	holdsAll := []string{"connection:clear-route-github:repo:read", "connection:runkora-github:repo:read", "connection:acme-github:repo:read"}
	holdsRunkoraAcme := []string{"connection:runkora-github:repo:read", "connection:acme-github:repo:read"}
	la := &lookupAdapter{visible: visible}
	logger := logging.NewLoggerWithOutput(io.Discard)
	la.Adapter = githubapp.New(nil, nil, logger)
	adapters := NewAdapterRegistry()
	adapters.Register(la)
	pol := &selectionPolicy{scopes: holdsAll}
	b := NewBroker(pol, three, adapters, passthrough.New(), logger)

	denial := func(t *testing.T, err error) *Denial {
		t.Helper()
		var d *Denial
		if !errors.As(err, &d) || d.Reason != DenialConnectionUnresolved {
			t.Fatalf("err = %v, want a connection_unresolved denial", err)
		}
		for _, owner := range []string{"clear-route ", "runkora ", "acme "} {
			if strings.Contains(d.Unresolved+" ", owner) {
				t.Errorf("denial names a resolved owner: %q", d.Unresolved)
			}
		}
		return d
	}

	// A caller holding every connection resolves PR_cr through clear-route-github.
	if conn, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"PR_cr\") { id } }"}`)); err != nil || conn.Name != "clear-route-github" {
		t.Fatalf("resolved %v, err %v", conn, err)
	}

	t.Run("cache entry from a connection the caller does not hold is unused", func(t *testing.T) {
		pol.scopes, la.lookups = holdsRunkoraAcme, 0
		_, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"PR_cr\") { id } }"}`))
		denial(t, err)
		if la.lookups != 2 {
			t.Errorf("lookups = %d, want the caller's own 2", la.lookups)
		}
	})
	t.Run("owner resolved to a connection the caller does not hold", func(t *testing.T) {
		pol.scopes = holdsRunkoraAcme
		_, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ node(id: \"R_crpublic\") { id } }"}`))
		if d := denial(t, err); !strings.Contains(d.Unresolved, "none of the agent's connections") {
			t.Errorf("unresolved = %q", d.Unresolved)
		}
	})
	t.Run("node ids spanning held connections name no owner", func(t *testing.T) {
		pol.scopes = holdsRunkoraAcme
		_, _, err := b.resolveConnection(context.Background(), acmeClaims, graphqlRequest(`{"query":"{ nodes(ids: [\"PR_rk\", \"PR_acme\"]) { id } }"}`))
		d := denial(t, err)
		if body := d.Message(); strings.Contains(body, "owner runkora") || strings.Contains(body, "owners") {
			t.Errorf("denial body names owners: %q", body)
		}
	})
}
