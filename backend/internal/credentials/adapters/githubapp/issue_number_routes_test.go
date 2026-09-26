package githubapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/credentials"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Issues and pull requests share one number sequence and GitHub serves both
// under /issues/{n}, checking pull_requests when the target is a pull request.
// Those routes carry pulls as an alternate so a caller holding it gets a token
// GitHub will accept for a pull request.
func TestClassify_IssueNumberRoutesCarryPullsAlternate(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	conn := credentials.Connection{Config: mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: "orbital"})}

	cases := []struct {
		name       string
		method     string
		path       string
		action     string
		alternates []string
	}{
		{"react on an issue or PR body", http.MethodPost, "/repos/orbital/orbit/issues/1054/reactions", "issues:write", []string{"pulls:write"}},
		{"comment on the conversation", http.MethodPost, "/repos/orbital/orbit/issues/1054/comments", "issues:write", []string{"pulls:write"}},
		{"read one by number", http.MethodGet, "/repos/orbital/orbit/issues/1054", "issues:read", []string{"pulls:read"}},
		{"react on a conversation comment", http.MethodPost, "/repos/orbital/orbit/issues/comments/99/reactions", "issues:write", []string{"pulls:write"}},
		{"label by number", http.MethodPost, "/repos/orbital/orbit/issues/1054/labels", "issues:write", []string{"pulls:write"}},

		// No ambiguity, so no alternate.
		{"create an issue", http.MethodPost, "/repos/orbital/orbit/issues", "issues:write", nil},
		{"list issues", http.MethodGet, "/repos/orbital/orbit/issues", "issues:read", nil},
		{"repository labels", http.MethodPost, "/repos/orbital/orbit/labels", "issues:write", nil},
		{"pull request route", http.MethodPost, "/repos/orbital/orbit/pulls/1054/reviews", "pulls:write", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, "https://api.github.com"+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := open(t, a, conn).Classify(req)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if got.Action != tc.action {
				t.Errorf("action = %q, want %q", got.Action, tc.action)
			}
			if !slices.Equal(got.Alternates, tc.alternates) {
				t.Errorf("alternates = %v, want %v", got.Alternates, tc.alternates)
			}
		})
	}
}

// mintPermissions mints through the adapter against a stubbed GitHub API and
// returns the permissions the installation token was requested with.
func mintPermissions(t *testing.T, id credentials.CallerIdentity) map[string]string {
	t.Helper()
	var body ghclient.InstallationTokenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_tok", "expires_at": "2026-07-22T13:00:00Z"})
	}))
	t.Cleanup(srv.Close)

	logger := logging.NewLoggerWithOutput(io.Discard)
	a := New(ghclient.NewClient(srv.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	a.now = func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) }
	conn := credentials.Connection{
		ID: "conn-1", TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp,
		SecretRef: "ref",
		Config:    mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: "orbital"}),
	}
	if _, err := open(t, a, conn).Mint(context.Background(), id); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	return body.Permissions
}

// A token for an issue-number route carries pull_requests only when the grant
// authorized it. Without it the token is exactly as narrow as before.
func TestMint_AlsoAuthorizedWidensByExactlyThoseActions(t *testing.T) {
	t.Run("pulls authorized", func(t *testing.T) {
		perms := mintPermissions(t, credentials.CallerIdentity{
			TenantID: "acme", Action: "issues:write", Qualifier: "orbit",
			AlsoAuthorized: []string{"pulls:write"},
		})
		if perms["issues"] != "write" || perms["pull_requests"] != "write" {
			t.Errorf("permissions = %v, want issues:write and pull_requests:write", perms)
		}
		if perms["contents"] == "write" {
			t.Errorf("pulls:write must not widen contents beyond read, got %v", perms)
		}
	})

	t.Run("pulls not authorized", func(t *testing.T) {
		perms := mintPermissions(t, credentials.CallerIdentity{
			TenantID: "acme", Action: "issues:write", Qualifier: "orbit",
		})
		if perms["issues"] != "write" {
			t.Errorf("permissions = %v, want issues:write", perms)
		}
		if _, ok := perms["pull_requests"]; ok {
			t.Errorf("token carries pull_requests without it being authorized: %v", perms)
		}
	})

	t.Run("a read alternate does not lower a write", func(t *testing.T) {
		perms := mintPermissions(t, credentials.CallerIdentity{
			TenantID: "acme", Action: "pulls:write", Qualifier: "orbit",
			AlsoAuthorized: []string{"pulls:read"},
		})
		if perms["pull_requests"] != "write" {
			t.Errorf("pull_requests = %q, want write", perms["pull_requests"])
		}
	})
}
