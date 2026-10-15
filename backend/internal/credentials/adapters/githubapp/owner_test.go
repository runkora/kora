package githubapp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/credentials"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestConnector_Owner(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	c := open(t, a, credentials.Connection{Config: mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: " Fabrikam "})})
	if got := c.(*connector).Owner(); got != "fabrikam" {
		t.Errorf("Owner() = %q, want fabrikam", got)
	}
}

func TestConnector_RequestOwners_Path(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	c := open(t, a, validConn(t)).(*connector)
	cases := []struct {
		url  string
		want string
	}{
		{"https://github.com/Fabrikam/widgets.git/info/refs?service=git-upload-pack", "fabrikam"},
		{"https://github.com/fabrikam", ""},
		{"https://api.github.com/repos/contoso/gadgets/pulls", "contoso"},
		{"https://api.github.com/orgs/fabrikam/repos", "fabrikam"},
		{"https://api.github.com/users/octocat", "octocat"},
		{"https://api.github.com/user", ""},
		{"https://api.github.com/repositories/123", ""},
		{"https://codeload.github.com/fabrikam/widgets/tar.gz/main", "fabrikam"},
		{"https://raw.githubusercontent.com/fabrikam/widgets/main/README.md", "fabrikam"},
		{"https://objects.githubusercontent.com/github-production-release-asset/1/2", ""},
		{"https://ghe.example.com/team/app.git/git-upload-pack", "team"},
		{"https://ghe.example.com/api/v3/repos/team/app/issues", "team"},
		{"https://api.github.com./repos/runkora/kora", "runkora"},
		{"https://objects.githubusercontent.com./a/b", ""},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got := c.RequestOwners(httptest.NewRequest(http.MethodGet, tc.url, nil))
			if strings.Join(got.Owners, ",") != tc.want {
				t.Errorf("owners = %v, want %q", got.Owners, tc.want)
			}
		})
	}
}

func TestConnector_RequestOwners_Search(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	c := open(t, a, validConn(t)).(*connector)
	cases := []struct {
		q    string
		want []string
	}{
		{"repo:RunKora/kora is:pr", []string{"runkora"}},
		{"bug org:clear-route", []string{"clear-route"}},
		{`user:"octocat"`, []string{"octocat"}},
		{"repo:runkora/kora org:clear-route", []string{"runkora", "clear-route"}},
		{"-org:runkora bug", nil},
		{"NOT org:runkora bug", nil},
		{"state:open bug", nil},
		{"repo:noslash", nil},
		{"(org:runkora OR org:clear-route) bug", []string{"runkora", "clear-route"}},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "https://api.github.com/search/issues", nil)
			req.URL.RawQuery = url.Values{"q": {tc.q}}.Encode()
			got := c.RequestOwners(req)
			if !slices.Equal(got.Owners, tc.want) || got.Subject != subjectSearch {
				t.Errorf("owners = %v (%s), want %v", got.Owners, got.Subject, tc.want)
			}
		})
	}
}

func TestConnector_RequestOwners_GraphQL(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	c := open(t, a, validConn(t)).(*connector)
	cases := []struct {
		name       string
		body       string
		wantOwners []string
		wantIDs    []string
	}{
		{"repository owner", `{"query":"{ repository(owner: \"RunKora\", name: \"kora\") { id } }"}`, []string{"runkora"}, nil},
		{"organization login", `{"query":"{ organization(login: \"clear-route\") { id } }"}`, []string{"clear-route"}, nil},
		{"repositoryOwner login", `{"query":"{ repositoryOwner(login: \"runkora\") { id } }"}`, []string{"runkora"}, nil},
		{"user login", `{"query":"{ user(login: \"octocat\") { id } }"}`, []string{"octocat"}, nil},
		{"owner from variable", `{"query":"query($o: String!) { repository(owner: $o, name: \"kora\") { id } }","variables":{"o":"runkora"}}`, []string{"runkora"}, nil},
		{"owner from variable default", `{"query":"query($o: String = \"runkora\") { repository(owner: $o, name: \"kora\") { id } }"}`, []string{"runkora"}, nil},
		{"owner inside fragment", `{"query":"query { ...F } fragment F on Query { organization(login: \"clear-route\") { id } }"}`, []string{"clear-route"}, nil},
		{"owners spanning two accounts", `{"query":"{ a: repository(owner: \"runkora\", name: \"kora\") { id } b: organization(login: \"clear-route\") { id } }"}`, []string{"clear-route", "runkora"}, nil},
		{"node and nodes ids", `{"query":"{ node(id: \"PR_1\") { id } nodes(ids: [\"I_2\", \"I_3\"]) { id } }"}`, nil, []string{"I_2", "I_3", "PR_1"}},
		{"mutation input ids", `{"query":"mutation($in: AddLabelsToLabelableInput!) { addLabelsToLabelable(input: $in) { clientMutationId } }","variables":{"in":{"labelableId":"I_9","labelIds":["LA_1"],"clientMutationId":"abc"}}}`, nil, []string{"I_9", "LA_1"}},
		{"excluded id names", `{"query":"mutation { updateProjectV2ItemFieldValue(input: {projectId: \"PVT_1\", itemId: \"PVTI_1\", fieldId: \"F_1\", value: {singleSelectOptionId: \"abc\", iterationId: \"it\"}, clientMutationId: \"x\"}) { clientMutationId } }"}`, nil, []string{"F_1", "PVTI_1", "PVT_1"}},
		{"viewer only", `{"query":"{ viewer { login } }"}`, nil, nil},
		{"unparseable", `{"query":"{ repository("}`, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://api.github.com/graphql", strings.NewReader(tc.body))
			got := c.RequestOwners(req)
			if !slices.Equal(got.Owners, tc.wantOwners) || !slices.Equal(got.NodeIDs, tc.wantIDs) || got.Subject != subjectGraphQL {
				t.Errorf("got owners %v ids %v (%s), want %v %v", got.Owners, got.NodeIDs, got.Subject, tc.wantOwners, tc.wantIDs)
			}
			if body, _ := io.ReadAll(req.Body); string(body) != tc.body {
				t.Errorf("body not restored for forwarding")
			}
		})
	}
}

// The lookup mints a read-only token from the installation's permissions and
// returns only the ids GitHub resolves.
func TestConnector_NodeOwners(t *testing.T) {
	var minted ghclient.InstallationTokenRequest
	var lookups int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/app/installations/42":
			_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]string{"contents": "write", "issues": "read", "metadata": "read", "administration": "write"}})
		case "/app/installations/42/access_tokens":
			_ = json.NewDecoder(r.Body).Decode(&minted)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_lookup", "expires_at": "2026-06-06T13:00:00Z"})
		case "/graphql":
			lookups++
			if r.Header.Get("Authorization") != "Bearer ghs_lookup" {
				t.Errorf("lookup authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":   map[string]any{"n0": map[string]any{"repository": map[string]any{"owner": map[string]any{"login": "RunKora"}}}, "n1": nil, "n2": map[string]any{"owner": map[string]any{"login": "clear-route"}}},
				"errors": []any{map[string]any{"message": "Could not resolve to a node"}},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	logger := logging.NewLoggerWithOutput(io.Discard)
	a := New(ghclient.NewClient(srv.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	a.now = func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) }
	c := open(t, a, credentials.Connection{ID: "acme|runkora", SecretRef: "ref", Config: mustConfig(t, config{AppID: "1", InstallationID: 42, Owner: "runkora"})}).(*connector)

	got, err := c.NodeOwners(t.Context(), []string{"PR_1", "garbage", "O_1"}, []string{"contents", "issues"})
	if err != nil {
		t.Fatalf("NodeOwners: %v", err)
	}
	if len(got) != 2 || got["PR_1"] != "runkora" || got["O_1"] != "clear-route" {
		t.Errorf("owners = %v, want PR_1=runkora O_1=clear-route", got)
	}
	want := map[string]string{"contents": "read", "issues": "read", "metadata": "read"}
	if len(minted.Permissions) != len(want) || minted.Permissions["contents"] != "read" || minted.Permissions["issues"] != "read" || minted.Permissions["administration"] != "" {
		t.Errorf("lookup token permissions = %v, want %v", minted.Permissions, want)
	}
	if _, err := c.NodeOwners(t.Context(), []string{"PR_1"}, []string{"contents", "issues"}); err != nil || lookups != 2 {
		t.Errorf("second lookup err %v, lookups %d", err, lookups)
	}
}

// An installation granting none of the lookup permissions mints no token, since
// an empty permission set would carry every installation permission.
func TestConnector_NodeOwnersWithoutLookupPermissions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/42" {
			t.Errorf("unexpected call to %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]string{"administration": "write"}})
	}))
	defer srv.Close()
	logger := logging.NewLoggerWithOutput(io.Discard)
	a := New(ghclient.NewClient(srv.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	c := open(t, a, credentials.Connection{ID: "x", SecretRef: "ref", Config: mustConfig(t, config{AppID: "1", InstallationID: 42})}).(*connector)
	if _, err := c.NodeOwners(t.Context(), []string{"PR_1"}, []string{"repo"}); !errors.Is(err, errNoLookupPermissions) {
		t.Errorf("err = %v, want errNoLookupPermissions", err)
	}
}
