package egressgw

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
)

// qualifyingConnector adds the real github_app connector's ClassifyResolved to a
// lookupConnector, whose id lookup is served from the test's visible map.
type qualifyingConnector struct{ *lookupConnector }

func (c qualifyingConnector) ClassifyResolved(req *http.Request, repos map[string]string) (credentials.Classification, error) {
	return c.Connector.(idQualifier).ClassifyResolved(req, repos)
}

func TestClassify_QualifiesByResolvedIDs(t *testing.T) {
	conn := githubConn(t, "fabrikam-github", "fabrikam")
	visible := map[string]map[string]string{"fabrikam-github": {"PR_w": "fabrikam/widgets", "PR_g": "fabrikam/gadgets", "522": "fabrikam/widgets"}}
	pullsWrite := `{"query":"mutation($in: UpdatePullRequestInput!) { updatePullRequest(input: $in) { clientMutationId } }","variables":{"in":{"pullRequestId":"PR_ID"}}}`
	update := func(id string) EgressRequest {
		return graphqlRequest(strings.ReplaceAll(pullsWrite, "PR_ID", id))
	}
	repoPath := func(path string) EgressRequest {
		r := httptest.NewRequest(http.MethodGet, "https://api.github.com"+path, nil)
		return EgressRequest{Host: r.Host, Method: http.MethodGet, URL: r.URL, HTTPRequest: r}
	}

	tests := []struct {
		name           string
		scopes         []string
		req            EgressRequest
		lookupErr      error
		scopesErr      error
		wantQualifier  string
		wantAction     string
		wantErr        bool
		wantCategories []string
	}{
		{name: "per-repository grant qualifies by the resolved repository", scopes: []string{"connection:fabrikam-github:pulls:widgets:write"}, req: update("PR_w"), wantQualifier: "widgets", wantAction: "pulls:write", wantCategories: []string{"pulls"}},
		{name: "a repository the caller holds no grant on stays unresolved", scopes: []string{"connection:fabrikam-github:pulls:widgets:write"}, req: update("PR_g"), wantAction: "pulls:write", wantCategories: []string{"pulls"}},
		{name: "an unqualified grant qualifies by any resolved repository", scopes: []string{"connection:fabrikam-github:pulls:write", "connection:fabrikam-github:issues:widgets:write"}, req: update("PR_g"), wantQualifier: "gadgets", wantAction: "pulls:write", wantCategories: []string{"issues", "pulls"}},
		{name: "no grant on the connection needs no lookup", scopes: []string{"connection:other-github:pulls:write"}, req: update("PR_w"), wantAction: "pulls:write"},
		{name: "a failed lookup fails closed", scopes: []string{"connection:fabrikam-github:pulls:widgets:write"}, req: update("PR_w"), lookupErr: errors.New("github down"), wantErr: true, wantCategories: []string{"pulls"}},
		{name: "a failed scope read fails closed", req: update("PR_w"), scopesErr: errors.New("opa down"), wantErr: true},
		{name: "repository id path classifies as its repos path", scopes: []string{"connection:fabrikam-github:pulls:widgets:read"}, req: repoPath("/repositories/522/pulls/7/files?page=2"), wantQualifier: "widgets", wantAction: "pulls:read", wantCategories: []string{"pulls"}},
		{name: "unresolved repository id keeps the catch-all", scopes: []string{"connection:fabrikam-github:pulls:widgets:read"}, req: repoPath("/repositories/9/pulls"), wantAction: "repo:read", wantCategories: []string{"pulls"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			la := &lookupAdapter{visible: visible, err: tc.lookupErr}
			b := lookupBroker(&selectionPolicy{scopes: tc.scopes, scopesErr: tc.scopesErr}, candidateConns{conn}, la)
			connector := openQualifying(t, la, conn)
			got, err := b.classify(context.Background(), acmeClaims, conn, connector, tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("classify = %+v, want an error", got)
				}
			} else if err != nil || got.Action != tc.wantAction || got.Qualifier != tc.wantQualifier {
				t.Fatalf("classify = %+v (err %v), want %s @%q", got, err, tc.wantAction, tc.wantQualifier)
			}
			if len(tc.wantCategories) == 0 && la.lookups != 0 {
				t.Errorf("lookups = %d, want none", la.lookups)
			}
			if len(tc.wantCategories) > 0 && (la.lookups != 1 || !slices.Equal(la.categories[0], tc.wantCategories)) {
				t.Errorf("lookups = %d with categories %v, want one with %v", la.lookups, la.categories, tc.wantCategories)
			}
		})
	}
}

// A resolved id is cached for the connection and categories, so a repeat
// request needs no second lookup.
func TestClassify_ResolvedIDCacheHit(t *testing.T) {
	conn := githubConn(t, "fabrikam-github", "fabrikam")
	la := &lookupAdapter{visible: map[string]map[string]string{"fabrikam-github": {"PR_w": "fabrikam/widgets"}}}
	b := lookupBroker(&selectionPolicy{scopes: []string{"connection:fabrikam-github:pulls:widgets:read"}}, candidateConns{conn}, la)
	connector := openQualifying(t, la, conn)
	for range 2 {
		got, err := b.classify(context.Background(), acmeClaims, conn, connector, graphqlRequest(`{"query":"{ node(id: \"PR_w\") { ... on PullRequest { title } } }"}`))
		if err != nil || got.Qualifier != "widgets" {
			t.Fatalf("classify = %+v, err %v", got, err)
		}
	}
	if la.lookups != 1 {
		t.Errorf("lookups = %d, want 1", la.lookups)
	}
}

// A lookup answered with errors caches what resolved but not the misses, which
// may be failures rather than ids the token cannot see.
func TestClassify_PartialLookupDoesNotCacheMisses(t *testing.T) {
	conn := githubConn(t, "fabrikam-github", "fabrikam")
	la := &lookupAdapter{visible: map[string]map[string]string{"fabrikam-github": {"PR_w": "fabrikam/widgets"}}, partial: true}
	b := lookupBroker(&selectionPolicy{scopes: []string{"connection:fabrikam-github:pulls:widgets:read"}}, candidateConns{conn}, la)
	connector := openQualifying(t, la, conn)
	doc := func(id string) EgressRequest {
		return graphqlRequest(`{"query":"{ node(id: \"` + id + `\") { ... on PullRequest { title } } }"}`)
	}
	for _, id := range []string{"PR_w", "PR_w", "PR_missing", "PR_missing"} {
		if _, err := b.classify(context.Background(), acmeClaims, conn, connector, doc(id)); err != nil {
			t.Fatalf("classify %s: %v", id, err)
		}
	}
	if la.lookups != 3 {
		t.Errorf("lookups = %d, want 3: one for PR_w, then PR_missing twice", la.lookups)
	}
}

func openQualifying(t *testing.T, la *lookupAdapter, conn *credentials.Connection) credentials.Connector {
	t.Helper()
	c, err := la.Open(*conn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return qualifyingConnector{c.(*lookupConnector)}
}

// A failed scope read or lookup denies even a read the anonymous fallback would
// otherwise serve, since the grant never got to decide it.
func TestAuthorize_FailedIDResolutionIsPolicyUnavailable(t *testing.T) {
	conn := githubConn(t, "fabrikam-github", "fabrikam")
	r := httptest.NewRequest(http.MethodGet, "https://api.github.com/repositories/522/pulls", nil)
	req := EgressRequest{Host: r.Host, Method: http.MethodGet, URL: r.URL, HTTPRequest: r}
	tests := []struct {
		name      string
		scopesErr error
		lookupErr error
	}{
		{name: "scope read fails", scopesErr: errors.New("opa down")},
		{name: "lookup fails", lookupErr: errors.New("github down")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			la := &lookupAdapter{err: tc.lookupErr, qualifying: true}
			pol := &selectionPolicy{scopes: []string{"connection:fabrikam-github:pulls:widgets:read"}, scopesErr: tc.scopesErr, ownEgress: true}
			dec, err := lookupBroker(pol, candidateConns{conn}, la).Authorize(context.Background(), acmeClaims, req)
			var d *Denial
			if !errors.As(err, &d) || d.Reason != DenialPolicyUnavailable {
				t.Fatalf("Authorize = %+v, err %v, want a policy_unavailable denial", dec, err)
			}
		})
	}
}
