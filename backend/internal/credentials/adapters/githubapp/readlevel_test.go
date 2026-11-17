package githubapp

import (
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
)

// The broker authorizes a read held only as a write as the read itself, so the
// token carries read: capped by the installation for a category, without the
// write-only workflows, and exact for a REST route. A real write keeps write.
func TestMint_ReadsMintReadLevel(t *testing.T) {
	c := graphqlConnector(t)
	installed := map[string]string{"metadata": "read", "contents": "write", "workflows": "write", "actions": "write", "checks": "write", "statuses": "write", "issues": "write"}
	classify := func(method, url, body string) credentials.Classification {
		t.Helper()
		req, err := http.NewRequest(method, url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		cls, err := c.Classify(req)
		if err != nil {
			t.Fatalf("Classify %s %s: %v", method, url, err)
		}
		return cls
	}
	query := classify(http.MethodPost, "https://api.github.com/graphql", `{"query":"{ repository(owner:\"example-org\", name:\"widgets\") { id } }"}`)
	fetch := classify(http.MethodPost, "https://github.com/example-org/widgets.git/git-upload-pack", "")
	issue := classify(http.MethodGet, "https://api.github.com/repos/example-org/widgets/issues/1", "")
	put := classify(http.MethodPut, "https://api.github.com/repos/example-org/widgets/contents/README.md", "")
	fork := classify(http.MethodPost, "https://api.github.com/repos/example-org/widgets/forks", "")

	cases := []struct {
		name string
		id   credentials.CallerIdentity
		want map[string]string
	}{
		{"GraphQL query by a contents and workflows writer", credentials.CallerIdentity{Action: "contents:read", AlsoAuthorized: []string{"workflows:read"}, Permissions: query.Permissions},
			map[string]string{"contents": "read", "metadata": "read"}},
		{"GraphQL query by a ci writer, capped by the installation", credentials.CallerIdentity{Action: "ci:read", Permissions: query.Permissions},
			map[string]string{"actions": "read", "checks": "read", "statuses": "read", "metadata": "read"}},
		{"git fetch by a contents writer", credentials.CallerIdentity{Action: fetch.Action, Permissions: fetch.Permissions},
			map[string]string{"contents": "read", "metadata": "read"}},
		{"REST read by an issues writer", credentials.CallerIdentity{Action: issue.Action, Permissions: issue.Permissions},
			map[string]string{"issues": "read", "metadata": "read"}},
		{"a real write keeps its write", credentials.CallerIdentity{Action: put.Action, Permissions: put.Permissions},
			map[string]string{"contents": "write", "metadata": "read"}},
		{"a fork's required read met by a write mints read", credentials.CallerIdentity{Action: fork.Action, AlsoAuthorized: fork.AlsoRequired, Permissions: fork.Permissions},
			map[string]string{"administration": "write", "contents": "read", "metadata": "read"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.id.TenantID, tc.id.Qualifier = "example", "widgets"
			if got := mintedPermissionsOn(t, tc.id, installed); !maps.Equal(got, tc.want) {
				t.Errorf("minted %v, want %v", got, tc.want)
			}
		})
	}
	if fork.Action != "admin:write" || len(fork.AlsoRequired) != 1 || fork.AlsoRequired[0] != "contents:read" {
		t.Errorf("fork = %+v, want admin:write requiring contents:read", fork)
	}
}
