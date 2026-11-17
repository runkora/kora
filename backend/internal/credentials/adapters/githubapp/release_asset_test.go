package githubapp

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
)

// A release asset download is addressed by repository id, and classifies as a
// contents read of the repository that id resolves to.
func TestClassifyResolved_ReleaseAsset(t *testing.T) {
	const asset = "https://release-assets.githubusercontent.com/github-production-release-asset/%s/abc?sp=r&sig=x"
	resolved := map[string]string{"1": "example-org/widgets", "2": "contoso/widgets"}
	c := graphqlConnector(t).(*connector)
	cases := []struct {
		id, qualifier string
	}{
		{"1", "widgets"},
		{"2", ""}, // another owner's repository stays unqualified, as on the other content hosts
		{"3", ""}, // an id that does not resolve stays unqualified
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf(asset, tc.id), http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.ClassifyResolved(req, resolved)
			if err != nil {
				t.Fatalf("ClassifyResolved: %v", err)
			}
			assertClassification(t, got, readCls("contents:read", tc.qualifier))
			if owners := c.RequestOwners(req); !slices.Equal(owners.RepoIDs, []string{tc.id}) {
				t.Errorf("repository ids = %v, want [%s]", owners.RepoIDs, tc.id)
			}
		})
	}
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf(asset, "1"), http.NoBody)
	if _, err := c.ClassifyResolved(req, resolved); err == nil {
		t.Error("a write to a release asset classified")
	}
}

// A read's write alternate mints the read's exact permissions, so a write
// grant reading a route gets a read token. git fetch accepts contents:write.
func TestAcceptWrites(t *testing.T) {
	c := graphqlConnector(t)
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/example-org/widgets/pulls/7/files", http.NoBody)
	cls, err := c.Classify(req)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !slices.Contains(cls.Alternates, "pulls:write") || !maps.Equal(cls.Permissions["pulls:write"], cls.Permissions["pulls:read"]) {
		t.Fatalf("classification = %+v, want pulls:write minted as pulls:read", cls)
	}
	got := mintedPermissions(t, credentials.CallerIdentity{TenantID: "example", Action: "pulls:write", Qualifier: "widgets", Permissions: cls.Permissions})
	if got["pull_requests"] != "read" {
		t.Errorf("write grant reading files minted %v, want pull_requests:read", got)
	}

	fetch, _ := http.NewRequest(http.MethodPost, "https://github.com/example-org/widgets.git/git-upload-pack", http.NoBody)
	if cls, err := c.Classify(fetch); err != nil || !slices.Equal(cls.Alternates, []string{"contents:write"}) {
		t.Errorf("git fetch = %+v (err %v), want contents:write as an alternate", cls, err)
	}
}
