package githubapp

import (
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
)

// Contents, ref and release writes, and a git push, may also carry
// workflows:write, as GitHub's data lists for them. See ADR 2026-11-15 item 7.
func TestClassify_WorkflowsOptional(t *testing.T) {
	c := graphqlConnector(t)
	cases := []struct {
		method, url string
		optional    bool
	}{
		{http.MethodPut, "https://api.github.com/repos/example-org/widgets/contents/.github/workflows/ci.yml", true},
		{http.MethodDelete, "https://api.github.com/repos/example-org/widgets/contents/README.md", true},
		{http.MethodPost, "https://api.github.com/repos/example-org/widgets/git/refs", true},
		{http.MethodPatch, "https://api.github.com/repos/example-org/widgets/git/refs/heads/main", true},
		{http.MethodPost, "https://api.github.com/repos/example-org/widgets/releases", true},
		{http.MethodPatch, "https://api.github.com/repos/example-org/widgets/releases/7", true},
		{http.MethodPost, "https://github.com/example-org/widgets.git/git-receive-pack", true},
		{http.MethodDelete, "https://api.github.com/repos/example-org/widgets/git/refs/heads/old", false},
		{http.MethodGet, "https://api.github.com/repos/example-org/widgets/contents/README.md", false},
		{http.MethodPost, "https://github.com/example-org/widgets.git/git-upload-pack", false},
		{http.MethodPost, "https://api.github.com/repos/example-org/widgets/issues", false},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.url, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Classify(req)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if want := tc.optional; slices.Equal(got.Optional, []string{"workflows:write"}) != want || (!want && len(got.Optional) > 0) {
				t.Errorf("Optional = %v, want workflows:write %v", got.Optional, want)
			}
		})
	}
}

// A GraphQL token carries every category the grant held, plus metadata, and a
// baseline-only agent's token carries metadata alone. A held workflows:write
// joins a push token.
func TestMint_HeldCategories(t *testing.T) {
	cases := []struct {
		name string
		id   credentials.CallerIdentity
		want map[string]string
	}{
		{"held query categories", credentials.CallerIdentity{TenantID: "example", Action: "issues:read", AlsoAuthorized: []string{"pulls:read", "metadata:read"}, Qualifier: "kora"},
			map[string]string{"issues": "read", "pull_requests": "read", "metadata": "read"}},
		{"baseline only", credentials.CallerIdentity{TenantID: "example", Action: "metadata:read"}, map[string]string{"metadata": "read"}},
		{"push with workflows", credentials.CallerIdentity{TenantID: "example", Action: "contents:write", AlsoAuthorized: []string{"workflows:write"}, Qualifier: "kora"},
			map[string]string{"contents": "write", "workflows": "write", "metadata": "read"}},
		{"push without workflows", credentials.CallerIdentity{TenantID: "example", Action: "contents:write", Qualifier: "kora"},
			map[string]string{"contents": "write", "metadata": "read"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mintedPermissions(t, tc.id); !maps.Equal(got, tc.want) {
				t.Errorf("minted %v, want %v", got, tc.want)
			}
		})
	}
}

// A pulls-only grant's GraphQL token has no contents permission, so a query
// selecting a file's text cannot read it.
func TestMint_PullsOnlyGraphQLHasNoContents(t *testing.T) {
	body := `{"query":"{ repository(owner:\"example-org\", name:\"widgets\") { object(expression:\"HEAD:secrets.env\") { ... on Blob { text } } } }"}`
	cls, err := graphqlConnector(t).Classify(graphqlPost(t, body))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	// The broker authorizes with the one alternate a pulls-only grant holds.
	if !slices.Contains(cls.Alternates, "pulls:read") {
		t.Fatalf("pulls:read is not an alternate of %+v", cls)
	}
	got := mintedPermissions(t, credentials.CallerIdentity{TenantID: "example", Action: "pulls:read", Qualifier: cls.Qualifier})
	if _, ok := got["contents"]; ok {
		t.Errorf("pulls-only GraphQL token carries contents: %v", got)
	}
}
