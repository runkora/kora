package githubapp

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
)

// Every endpoint in GitHub's data classifies through its own template: to the
// categories of its permissions, or, when no permission set falls within the
// categories, to the classification error.
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
		want, ok := routeClassification(r)
		got, err := c.Classify(req)
		switch {
		case !ok && !errors.Is(err, credentials.ErrUnsupportedAction):
			t.Errorf("%s %s: err = %v, want ErrUnsupportedAction for permissions %v", r.Method, r.Path, err, r.Sets)
		case ok && err != nil:
			t.Errorf("%s %s: Classify: %v", r.Method, r.Path, err)
		case ok && (got.Action != want.Action || !slices.Equal(got.AlsoRequired, want.AlsoRequired)):
			t.Errorf("%s %s: classification = %+v, want %+v", r.Method, r.Path, got, want)
		case ok:
			for _, action := range append([]string{got.Action}, append(got.Alternates, got.AlsoRequired...)...) {
				if _, minted := got.Permissions[action]; !minted {
					t.Errorf("%s %s: no permissions for %s", r.Method, r.Path, action)
				}
			}
		}
	}
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := routeClassification(&restRoute{Sets: tc.sets})
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got.Action != tc.want.Action || !slices.Equal(got.Alternates, tc.want.Alternates) || !slices.Equal(got.AlsoRequired, tc.want.AlsoRequired) {
				t.Errorf("classification = %+v, want %+v", got, tc.want)
			}
		})
	}
	got, _ := routeClassification(&restRoute{Sets: []permissionSet{{"administration": "admin"}}})
	if got.Permissions["admin:write"]["administration"] != "admin" {
		t.Errorf("admin access must mint admin, got %v", got.Permissions)
	}
}

func TestClassify_HandWrittenSurfaces(t *testing.T) {
	c := graphqlConnector(t)
	cases := []struct {
		name, method, url, body string
		want                    credentials.Classification
		err                     bool
	}{
		{name: "lfs download", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/objects/batch", body: `{"operation":"download","objects":[]}`, want: cls("contents:read", "widgets")},
		{name: "lfs upload", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/objects/batch", body: `{"operation":"upload","objects":[]}`, want: cls("contents:write", "widgets")},
		{name: "lfs operation key is exact", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/objects/batch", body: `{"operation":"upload","Operation":"download"}`, want: cls("contents:write", "widgets")},
		{name: "lfs unknown operation", method: http.MethodPost, url: "https://github.com/example-org/widgets.git/info/lfs/objects/batch", body: `{"operation":"delete"}`, err: true},
		{name: "lfs list locks", method: http.MethodGet, url: "https://github.com/example-org/widgets.git/info/lfs/locks", want: cls("contents:read", "widgets")},
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
