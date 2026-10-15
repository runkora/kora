package githubapp

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestConnector_Owner(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	c := open(t, a, credentials.Connection{Config: mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: " RunKora "})})
	if got := c.(*connector).Owner(); got != "runkora" {
		t.Errorf("Owner() = %q, want runkora", got)
	}
}

func TestConnector_RequestOwner(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	c := open(t, a, validConn(t)).(*connector)
	cases := []struct {
		url  string
		want string
	}{
		{"https://github.com/RunKora/kora.git/info/refs?service=git-upload-pack", "runkora"},
		{"https://github.com/runkora", ""},
		{"https://api.github.com/repos/clear-route/orbit/pulls", "clear-route"},
		{"https://api.github.com/orgs/runkora/repos", "runkora"},
		{"https://api.github.com/users/octocat", "octocat"},
		{"https://api.github.com/graphql", ""},
		{"https://api.github.com/search/issues?q=repo:runkora/kora", ""},
		{"https://api.github.com/user", ""},
		{"https://api.github.com/repositories/123", ""},
		{"https://codeload.github.com/runkora/kora/tar.gz/main", "runkora"},
		{"https://raw.githubusercontent.com/runkora/kora/main/README.md", "runkora"},
		{"https://objects.githubusercontent.com/github-production-release-asset/1/2", ""},
		{"https://ghe.example.com/team/app.git/git-upload-pack", "team"},
		{"https://ghe.example.com/api/v3/repos/team/app/issues", "team"},
		{"https://ghe.example.com/api/graphql", ""},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			if got := c.RequestOwner(httptest.NewRequest("GET", tc.url, nil)); got != tc.want {
				t.Errorf("RequestOwner = %q, want %q", got, tc.want)
			}
		})
	}
}
