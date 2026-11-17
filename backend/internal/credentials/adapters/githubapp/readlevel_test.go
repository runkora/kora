package githubapp

import (
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
)

// A write that authorizes a read mints that category's read permissions, capped
// by the installation, while a real write keeps its exact permissions.
func TestMint_WriteAuthorizingReadMintsRead(t *testing.T) {
	c := graphqlConnector(t)
	installed := map[string]string{"metadata": "read", "contents": "write", "workflows": "write", "actions": "write", "checks": "write", "statuses": "write"}
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
	put := classify(http.MethodPut, "https://api.github.com/repos/example-org/widgets/contents/README.md", "")
	fork := classify(http.MethodPost, "https://api.github.com/repos/example-org/widgets/forks", "")

	cases := []struct {
		name string
		id   credentials.CallerIdentity
		want map[string]string
	}{
		{"GraphQL query by a contents writer", credentials.CallerIdentity{Action: "contents:write", Qualifier: "widgets", Permissions: query.Permissions},
			map[string]string{"contents": "read", "metadata": "read"}},
		// workflows exists only at write, so a read-level token leaves it out.
		{"GraphQL query by a contents and workflows writer", credentials.CallerIdentity{Action: "contents:write", AlsoAuthorized: []string{"workflows:write"}, Qualifier: "widgets", Permissions: query.Permissions},
			map[string]string{"contents": "read", "metadata": "read"}},
		{"GraphQL query by a workflows reader", credentials.CallerIdentity{Action: "workflows:read", AlsoAuthorized: []string{"contents:read"}, Qualifier: "widgets", Permissions: query.Permissions},
			map[string]string{"contents": "read", "metadata": "read"}},
		{"GraphQL query by a ci writer, capped by the installation", credentials.CallerIdentity{Action: "ci:write", Qualifier: "widgets", Permissions: query.Permissions},
			map[string]string{"actions": "read", "checks": "read", "statuses": "read", "metadata": "read"}},
		{"git fetch by a contents writer", credentials.CallerIdentity{Action: "contents:write", Qualifier: "widgets", Permissions: fetch.Permissions},
			map[string]string{"contents": "read", "metadata": "read"}},
		{"a real write keeps its write", credentials.CallerIdentity{Action: "contents:write", Qualifier: "widgets", Permissions: put.Permissions},
			map[string]string{"contents": "write", "metadata": "read"}},
		{"a fork's required read met by a write mints read", credentials.CallerIdentity{Action: fork.Action, AlsoAuthorized: fork.AlsoRequired, Qualifier: "widgets", Permissions: fork.Permissions},
			map[string]string{"administration": "write", "contents": "read", "metadata": "read"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.id.TenantID = "example"
			if got := mintedPermissionsOn(t, tc.id, installed); !maps.Equal(got, tc.want) {
				t.Errorf("minted %v, want %v", got, tc.want)
			}
		})
	}
	if fork.Action != "admin:write" || len(fork.AlsoRequired) != 1 || fork.AlsoRequired[0] != "contents:read" {
		t.Errorf("fork = %+v, want admin:write requiring contents:read", fork)
	}
}
