package githubapp

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
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
			assertClassification(t, got, cls("contents:read", tc.qualifier))
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
