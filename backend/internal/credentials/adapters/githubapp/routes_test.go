package githubapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	platformconns "github.com/runkora/kora/backend/internal/platform/connections"
)

// The classifier's categories and the connection's published actions match
// both ways, so no classification names an action no grant can hold.
func TestCategories_MatchVocabulary(t *testing.T) {
	declared := map[string]bool{}
	for _, a := range platformconns.ActionNames(platformconns.AdapterGitHubApp) {
		declared[a] = true
	}
	produced := map[string]bool{}
	for i := range restRoutes {
		if cls, ok := routeClassification(&restRoutes[i]); ok {
			for _, a := range append(append([]string{cls.Action}, cls.Alternates...), cls.AlsoRequired...) {
				produced[a] = true
			}
		}
	}
	for _, doc := range []string{`{ viewer { login } }`, `mutation { addStar(input:{starrableId:"x"}){clientMutationId} }`} {
		body, _ := json.Marshal(map[string]string{"query": doc})
		req := httptest.NewRequest(http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(body))
		cls, err := classifyGraphQL(req, "", nil)
		if err != nil {
			t.Fatalf("classify %s: %v", doc, err)
		}
		for _, a := range append([]string{cls.Action}, cls.Alternates...) {
			produced[a] = true
		}
	}
	for a := range produced {
		if !declared[a] {
			t.Errorf("classifier returns %s, which the vocabulary does not publish", a)
		}
	}
	for a := range declared {
		if category, _ := splitAction(a); category != catMetadata && category != catRepo && categoryPermissions[category] == nil {
			t.Errorf("vocabulary publishes %s, which no category mints", a)
		}
	}
	for category := range categoryPermissions {
		if !declared[category+":"+verbRead] && !declared[category+":"+verbWrite] {
			t.Errorf("category %s is not published", category)
		}
	}
}

// Every endpoint in GitHub's data classifies through its own template, and is
// allowed exactly when the data allows it. An allowed endpoint mints one of
// its own permission sets: the action with its required actions together,
// and each alternate alone.
func TestClassify_EveryRESTEndpoint(t *testing.T) {
	c := graphqlConnector(t)
	for i := range restRoutes {
		r := &restRoutes[i]
		if strings.HasPrefix(r.Path, "/search/") {
			continue // classified by its query, see TestConnector_Classify
		}
		req, err := http.NewRequest(r.Method, "https://api.github.com"+concretePath(r.Path), http.NoBody)
		if err != nil {
			t.Fatalf("%s %s: build request: %v", r.Method, r.Path, err)
		}
		if got := matchRoute(r.Method, pathSegments(req.URL.EscapedPath())); got != r {
			t.Errorf("%s %s matched %v", r.Method, r.Path, got)
			continue
		}
		got, err := c.Classify(req)
		switch {
		case !dataAllows(r):
			if !errors.Is(err, credentials.ErrUnsupportedAction) {
				t.Errorf("%s %s: err = %v, want ErrUnsupportedAction for permissions %v", r.Method, r.Path, err, r.Sets)
			}
		case err != nil:
			t.Errorf("%s %s: Classify: %v", r.Method, r.Path, err)
		case got.Action == ActionMetadataRead:
		default:
			sets := [][]string{append([]string{got.Action}, got.AlsoRequired...)}
			for _, alt := range got.Alternates {
				sets = append(sets, []string{alt})
			}
			for _, actions := range sets {
				minted := permissionSet{}
				for _, a := range actions {
					maps.Copy(minted, got.Permissions[a])
				}
				if !slices.ContainsFunc(r.Sets, func(s permissionSet) bool { return maps.Equal(withoutMetadata(s), minted) }) {
					t.Errorf("%s %s: %v mints %v, not a set of %v", r.Method, r.Path, actions, minted, r.Sets)
				}
			}
		}
	}
}

// dataAllows reads GitHub's data for an endpoint: callable by installation
// tokens, with a set every permission of which has a category, and not a write
// that needs only metadata.
func dataAllows(r *restRoute) bool {
	if r.Refused {
		return false
	}
	if len(r.Sets) == 0 {
		return r.Method == http.MethodGet
	}
	usable := false
	for _, s := range r.Sets {
		inside := true
		for perm := range withoutMetadata(s) {
			inside = inside && permissionCategory[perm] != ""
		}
		if inside && len(withoutMetadata(s)) == 0 && r.Method != http.MethodGet {
			return false
		}
		usable = usable || inside
	}
	return usable
}

func withoutMetadata(s permissionSet) permissionSet {
	out := maps.Clone(s)
	delete(out, catMetadata)
	return out
}

// concretePath fills a route template: the connection's owner and a repository
// on repository paths, one segment per parameter, two for a greedy one.
func concretePath(template string) string {
	segs := pathSegments(template)
	for i, s := range segs {
		switch {
		case !strings.HasPrefix(s, "{"):
		case segs[0] == "repos" && i == 1, segs[0] == "orgs" && i == 1:
			segs[i] = "example-org"
		case segs[0] == "repos" && i == 2:
			segs[i] = "widgets"
		case greedyParams[s] && i == len(segs)-1:
			segs[i] = "a/b"
		default:
			segs[i] = "p" + s[1:len(s)-1]
		}
	}
	return "/" + strings.Join(segs, "/")
}

func TestRouteClassification(t *testing.T) {
	cases := []struct {
		name string
		sets []permissionSet
		want credentials.Classification
		ok   bool
	}{
		{"no permission is metadata", nil, credentials.Classification{Action: "metadata:read"}, true},
		{"either of two", []permissionSet{{"issues": "read"}, {"pull_requests": "read"}},
			credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}}, true},
		{"a superset is dropped", []permissionSet{{"contents": "write"}, {"contents": "write", "workflows": "write"}},
			credentials.Classification{Action: "contents:write"}, true},
		{"one set of two categories", []permissionSet{{"administration": "write", "members": "read"}},
			credentials.Classification{Action: "admin:write", AlsoRequired: []string{"members:read"}}, true},
		{"several sets of two keep the first", []permissionSet{{"administration": "write", "contents": "read"}, {"contents": "read", "repository_creation": "write"}},
			credentials.Classification{Action: "admin:write", AlsoRequired: []string{"contents:read"}}, true},
		{"a set outside the categories is skipped", []permissionSet{{"organization_administration": "read"}, {"members": "read"}},
			credentials.Classification{Action: "members:read"}, true},
		{"two permissions of one category", []permissionSet{{"actions": "read"}, {"checks": "read"}},
			credentials.Classification{Action: "ci:read"}, true},
		{"admin access is a write", []permissionSet{{"administration": "admin"}},
			credentials.Classification{Action: "admin:write"}, true},
		{"no set within the categories", []permissionSet{{"organization_self_hosted_runners": "read"}}, credentials.Classification{}, false},
		{"a metadata alternative needs only the baseline", []permissionSet{{"metadata": "read"}, {"contents": "write"}},
			credentials.Classification{Action: "metadata:read"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := routeClassification(&restRoute{Method: http.MethodGet, Sets: tc.sets})
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got.Action != tc.want.Action || !slices.Equal(got.Alternates, tc.want.Alternates) || !slices.Equal(got.AlsoRequired, tc.want.AlsoRequired) {
				t.Errorf("classification = %+v, want %+v", got, tc.want)
			}
		})
	}
	got, _ := routeClassification(&restRoute{Method: http.MethodPut, Sets: []permissionSet{{"administration": "admin"}}})
	if got.Permissions["admin:write"]["administration"] != "admin" {
		t.Errorf("admin access must mint admin, got %v", got.Permissions)
	}
	// The metadata baseline never authorizes a write.
	for _, sets := range [][]permissionSet{nil, {{"metadata": "read"}}} {
		if _, ok := routeClassification(&restRoute{Method: http.MethodDelete, Sets: sets}); ok {
			t.Errorf("DELETE needing %v classified, want refused", sets)
		}
	}
}

// Requests the table alone would let through on the metadata baseline, or
// attribute to the wrong repository, are refused or narrowed.
func TestClassify_RESTEdgeCases(t *testing.T) {
	c := graphqlConnector(t)
	cases := []struct {
		method, url string
		want        credentials.Classification
		err         bool
	}{
		{method: http.MethodDelete, url: "https://api.github.com/installation/token", err: true},
		{method: http.MethodDelete, url: "https://api.github.com/orgs/example-org/packages/container/app", err: true},
		{method: http.MethodGet, url: "https://api.github.com/repos/example-org/widgets/stargazers", want: cls("metadata:read", "widgets")},
		{method: http.MethodGet, url: "https://api.github.com/repos/example-org/widgets/contents/../../../other/x/contents/a", err: true},
		{method: http.MethodGet, url: "https://api.github.com/repos/example-org/widgets/contents/%2e%2e/a", err: true},
		// Endpoints installation tokens cannot call never fall to /user/{account_id}.
		{method: http.MethodGet, url: "https://api.github.com/user/codespaces", err: true},
		{method: http.MethodGet, url: "https://api.github.com/user/emails", err: true},
		{method: http.MethodGet, url: "https://api.github.com/user/repos", err: true},
		{method: http.MethodGet, url: "https://api.github.com/user/%63odespaces", err: true},
		{method: http.MethodGet, url: "https://api.github.com/user/12345", want: cls("metadata:read", "")},
		{method: http.MethodHead, url: "https://api.github.com/repos/example-org/widgets/contents/README.md", want: readCls("contents:read", "widgets")},
		{method: http.MethodGet, url: "https://api.github.com/repos/example-org/widgets/contents/", want: readCls("contents:read", "widgets")},
		{method: http.MethodGet, url: "https://api.github.com/repos/example-org/widgets/tarball", want: readCls("contents:read", "widgets")},
		{method: http.MethodPost, url: "https://uploads.github.com/repos/example-org/widgets/releases/%2e%2e/assets", err: true},
		{method: http.MethodGet, url: "https://github.com/example-org/%2e%2e/info/lfs/locks", err: true},
	}
	for _, tc := range cases {
		req, err := http.NewRequest(tc.method, tc.url, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.Classify(req)
		if tc.err {
			if !errors.Is(err, credentials.ErrUnsupportedAction) {
				t.Errorf("%s %s: err = %v, want ErrUnsupportedAction (classification %+v)", tc.method, tc.url, err, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s %s: Classify: %v", tc.method, tc.url, err)
			continue
		}
		assertClassification(t, got, tc.want)
	}
}

func TestClassify_HandWrittenSurfaces(t *testing.T) {
	c := graphqlConnector(t)
	cases := []struct {
		name, method, url, body string
		want                    credentials.Classification
		err                     bool
	}{
		{name: "lfs download", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/objects/batch", body: `{"operation":"download","objects":[]}`, want: readCls("contents:read", "widgets")},
		{name: "lfs upload", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/objects/batch", body: `{"operation":"upload","objects":[]}`, want: cls("contents:write", "widgets")},
		{name: "lfs operation key is exact", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/objects/batch", body: `{"operation":"upload","Operation":"download"}`, want: cls("contents:write", "widgets")},
		{name: "lfs unknown operation", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/objects/batch", body: `{"operation":"delete"}`, err: true},
		{name: "lfs list locks", method: http.MethodGet, url: "https://github.com/example-org/widgets.git/info/lfs/locks", want: readCls("contents:read", "widgets")},
		{name: "lfs create lock", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/locks", body: `{}`, want: cls("contents:write", "widgets")},
		{name: "lfs verify locks", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/locks/verify", body: `{}`, want: cls("contents:write", "widgets")},
		{name: "lfs foreign owner", method: http.MethodGet, url: "https://github.com/evil/widgets.git/info/lfs/locks", err: true},
		{name: "release asset upload", method: http.MethodPost, url: "https://uploads.github.com/repos/example-org/widgets/releases/7/assets?name=a.tgz", want: cls("contents:write", "widgets")},
		{name: "release asset upload, foreign owner", method: http.MethodPost, url: "https://uploads.github.com/repos/evil/widgets/releases/7/assets", err: true},
		{name: "uploads host, other path", method: http.MethodGet, url: "https://uploads.github.com/repos/example-org/widgets", err: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Classify(req)
			if tc.err {
				if !errors.Is(err, credentials.ErrUnsupportedAction) {
					t.Fatalf("err = %v, want ErrUnsupportedAction (classification %+v)", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			assertClassification(t, got, tc.want)
		})
	}
}

// An encoded slash keeps a branch name in one segment, so the deeper route
// matches; a raw slash in a middle segment matches no route.
func TestClassify_BranchNameWithSlash(t *testing.T) {
	c := graphqlConnector(t)
	for url, want := range map[string]string{
		"https://api.github.com/repos/example-org/widgets/branches/release%2F1.0/protection": "admin:read",
		"https://api.github.com/repos/example-org/widgets/branches/release/1.0":              "contents:read",
	} {
		req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.Classify(req)
		if err != nil || got.Action != want {
			t.Errorf("%s: classification = %+v, %v, want %s", url, got, err, want)
		}
	}
}

// A whole-category mint asks only for what the installation holds, since
// GitHub refuses a token asking for more. Exact REST permissions are passed
// through as classified.
func TestMint_CappedByInstallation(t *testing.T) {
	installed := map[string]string{"metadata": "read", "contents": "write", "actions": "write", "checks": "read", "statuses": "read", "pull_requests": "write"}
	cases := []struct {
		name string
		id   credentials.CallerIdentity
		want map[string]string
	}{
		{"ci without attestations or artifact_metadata", credentials.CallerIdentity{TenantID: "example", Action: "ci:read", Qualifier: "kora"},
			map[string]string{"actions": "read", "checks": "read", "statuses": "read", "metadata": "read"}},
		{"a category the installation lacks is left out", credentials.CallerIdentity{TenantID: "example", Action: "pulls:read", AlsoAuthorized: []string{"issues:read", "metadata:read"}, Qualifier: "kora"},
			map[string]string{"pull_requests": "read", "metadata": "read"}},
		{"a write is capped at the installation's level", credentials.CallerIdentity{TenantID: "example", Action: "ci:write", Qualifier: "kora"},
			map[string]string{"actions": "write", "checks": "read", "statuses": "read", "metadata": "read"}},
		{"exact REST permissions are not intersected", credentials.CallerIdentity{TenantID: "example", Action: "ci:read", Qualifier: "kora",
			Permissions: map[string]map[string]string{"ci:read": {"attestations": "read"}}},
			map[string]string{"attestations": "read", "metadata": "read"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mintedPermissionsOn(t, tc.id, installed); !maps.Equal(got, tc.want) {
				t.Errorf("minted %v, want %v", got, tc.want)
			}
		})
	}
}
