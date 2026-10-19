package egressgw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// candidateConns serves a fixed candidate list for every host.
type candidateConns []*credentials.Connection

func (c candidateConns) ListByHost(context.Context, string, string) ([]*credentials.Connection, error) {
	if len(c) == 0 {
		return nil, credentials.ErrConnectionNotFound
	}
	return c, nil
}

// selectionPolicy holds the listed scopes, answers the egress posture check with
// ownEgress, denies every connection scope and records which connections it checked.
type selectionPolicy struct {
	scopes     []string
	scopesErr  error
	ownEgress  bool
	scopeReads int
	checked    []string
}

func (p *selectionPolicy) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	if in.ResourceType == "egress" {
		return policy.Result{Allow: p.ownEgress}, nil
	}
	p.checked = append(p.checked, in.PathSegments[0])
	return policy.Result{Reason: noMatchingGrant}, nil
}

func (p *selectionPolicy) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	p.scopeReads++
	return p.scopes, p.scopesErr
}

func githubConn(t *testing.T, name, owner string) *credentials.Connection {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{"app_id": "1", "installation_id": 1, "owner": owner})
	if err != nil {
		t.Fatal(err)
	}
	return &credentials.Connection{TenantID: "acme", Name: name, Adapter: credentials.AdapterGitHubApp, BaseHost: "github.com", Config: cfg}
}

func selectionBroker(pol *selectionPolicy, conns candidateConns) *Broker {
	logger := logging.NewLoggerWithOutput(io.Discard)
	adapters := NewAdapterRegistry()
	adapters.Register(githubapp.New(nil, nil, logger))
	return NewBroker(pol, conns, adapters, passthrough.New(), logger)
}

func egressRequest(method, url string) EgressRequest {
	r := httptest.NewRequest(method, url, nil)
	return EgressRequest{Host: r.Host, Method: method, URL: r.URL, HTTPRequest: r}
}

var acmeClaims = &token.PlatformClaims{Orbit: token.OrbitClaims{TenantID: "acme"}}

func TestResolveConnection_SameHostSelection(t *testing.T) {
	contoso := githubConn(t, "contoso-github", "contoso")
	fabrikam := githubConn(t, "fabrikam-github", "Fabrikam")
	both := candidateConns{contoso, fabrikam}

	tests := []struct {
		name          string
		conns         candidateConns
		scopes        []string
		url           string
		want          string
		wantAmbiguous bool
		wantScopeRead bool
	}{
		{name: "owner selects fabrikam", conns: both, url: "https://github.com/fabrikam/widgets.git/info/refs?service=git-upload-pack", want: "fabrikam-github"},
		{name: "owner selects contoso", conns: both, url: "https://api.github.com/repos/Contoso/gadgets/pulls", want: "contoso-github"},
		{name: "owner matches no candidate", conns: both, url: "https://github.com/octocat/hello-world.git/info/refs?service=git-upload-pack", wantScopeRead: true},
		{name: "owner-less with one held", conns: both, scopes: []string{"connection:fabrikam-github:contents:read", "inference:invoke"}, url: "https://api.github.com/graphql", want: "fabrikam-github", wantScopeRead: true},
		{name: "owner-less with two held", conns: both, scopes: []string{"connection:fabrikam-github:contents:read", "connection:contoso-github:issues:write"}, url: "https://api.github.com/graphql", wantAmbiguous: true, wantScopeRead: true},
		{name: "owner-less with none held", conns: both, scopes: []string{"connection:fabrikam-github-old:contents:read"}, url: "https://api.github.com/search/issues", wantScopeRead: true},
		{name: "owner-less with a scope covering every connection", conns: both, scopes: []string{"connection:read"}, url: "https://api.github.com/graphql", wantAmbiguous: true, wantScopeRead: true},
		{name: "fully-qualified host selects by owner", conns: both, url: "https://api.github.com./repos/fabrikam/widgets", want: "fabrikam-github"},
		{name: "single connection unchanged", conns: candidateConns{contoso}, url: "https://api.github.com/graphql", want: "contoso-github"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pol := &selectionPolicy{scopes: tc.scopes}
			b := selectionBroker(pol, tc.conns)
			conn, registered, err := b.resolveConnection(context.Background(), acmeClaims, egressRequest(http.MethodPost, tc.url))
			if (pol.scopeReads > 0) != tc.wantScopeRead {
				t.Errorf("scope reads = %d, want read %v", pol.scopeReads, tc.wantScopeRead)
			}
			if tc.want != "" {
				if err != nil || !registered || conn.Name != tc.want {
					t.Fatalf("resolved %v (registered %v, err %v), want %s", conn, registered, err, tc.want)
				}
				return
			}
			var d *Denial
			if !errors.As(err, &d) || d.Reason != DenialConnectionUnresolved {
				t.Fatalf("err = %v, want a connection_unresolved denial", err)
			}
			if d.ambiguous != tc.wantAmbiguous {
				t.Errorf("ambiguous = %v, want %v", d.ambiguous, tc.wantAmbiguous)
			}
			if !slices.Equal(d.Candidates, []string{"contoso-github", "fabrikam-github"}) {
				t.Errorf("candidates = %v, want both", d.Candidates)
			}
		})
	}
}

func TestAuthorize_UnresolvedConnection(t *testing.T) {
	both := candidateConns{githubConn(t, "contoso-github", "contoso"), githubConn(t, "fabrikam-github", "fabrikam")}
	holdsBoth := []string{"connection:contoso-github:repo:read", "connection:fabrikam-github:repo:read"}

	tests := []struct {
		name       string
		method     string
		url        string
		scopes     []string
		wantAction string
	}{
		{name: "third-owner read is anonymous", method: http.MethodGet, url: "https://github.com/octocat/hello-world.git/info/refs?service=git-upload-pack", wantAction: anonymousReadAction},
		{name: "third-owner write is denied", method: http.MethodPost, url: "https://github.com/octocat/hello-world.git/git-receive-pack"},
		{name: "owner-less read holding none is anonymous", method: http.MethodGet, url: "https://api.github.com/search/repositories?q=kora", wantAction: anonymousReadAction},
		{name: "owner-less read holding both is denied", method: http.MethodGet, url: "https://api.github.com/search/repositories?q=kora", scopes: holdsBoth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := selectionBroker(&selectionPolicy{scopes: tc.scopes, ownEgress: true}, both)
			got, err := b.Authorize(context.Background(), acmeClaims, egressRequest(tc.method, tc.url))
			if tc.wantAction != "" {
				if err != nil || got.Action != tc.wantAction || got.Credential.Value != "" {
					t.Fatalf("decision = %+v, err %v, want an uncredentialed %s", got, err, tc.wantAction)
				}
				return
			}
			var d *Denial
			if !errors.As(err, &d) || d.Reason != DenialConnectionUnresolved || len(d.Candidates) != 2 {
				t.Fatalf("err = %v, want a connection_unresolved denial listing both candidates", err)
			}
		})
	}
}

func TestDenial_ConnectionUnresolvedNamesCandidates(t *testing.T) {
	d := &Denial{Reason: DenialConnectionUnresolved, Host: "api.github.com", Candidates: []string{"contoso-github", "fabrikam-github"}}
	if msg := d.Message("kora"); !strings.Contains(msg, "contoso-github, fabrikam-github") || strings.Contains(msg, "—") {
		t.Errorf("message = %q, want both candidates and no em dash", msg)
	}
	if got := d.Details()["candidates"]; !slices.Equal(got.([]string), d.Candidates) {
		t.Errorf("details candidates = %v, want %v", got, d.Candidates)
	}
}

// A failed scope read denies as policy_unavailable and never falls back.
func TestAuthorize_ScopeReadFailureDenies(t *testing.T) {
	both := candidateConns{githubConn(t, "contoso-github", "contoso"), githubConn(t, "fabrikam-github", "fabrikam")}
	b := selectionBroker(&selectionPolicy{scopesErr: errors.New("bundle down"), ownEgress: true}, both)
	_, err := b.Authorize(context.Background(), acmeClaims, egressRequest(http.MethodGet, "https://api.github.com/user"))
	var d *Denial
	if !errors.As(err, &d) || d.Reason != DenialPolicyUnavailable {
		t.Fatalf("err = %v, want a policy_unavailable denial", err)
	}
}

// Policy is evaluated against the connection the owner selects.
func TestAuthorize_PolicyCheckedOnSelectedConnection(t *testing.T) {
	both := candidateConns{githubConn(t, "contoso-github", "contoso"), githubConn(t, "fabrikam-github", "fabrikam")}
	pol := &selectionPolicy{scopes: []string{"connection:contoso-github:repo:read"}, ownEgress: true}
	got, err := selectionBroker(pol, both).Authorize(context.Background(), acmeClaims, egressRequest(http.MethodGet, "https://api.github.com/repos/fabrikam/widgets"))
	if err != nil || got.Action != anonymousReadAction {
		t.Fatalf("decision = %+v, err %v, want the anonymous read after a policy deny", got, err)
	}
	if len(pol.checked) == 0 || slices.ContainsFunc(pol.checked, func(c string) bool { return c != "fabrikam-github" }) {
		t.Errorf("policy checked %v, want only fabrikam-github", pol.checked)
	}
}
