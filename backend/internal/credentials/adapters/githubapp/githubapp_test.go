package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/credentials"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// stubSecrets is a credentials.SecretResolver returning a fixed PEM (or error)
// for tests.
type stubSecrets struct {
	pem []byte
	err error
}

func (s stubSecrets) Resolve(context.Context, string) ([]byte, error) { return s.pem, s.err }

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func mustConfig(t *testing.T, cfg config) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return b
}

func TestAdapter_CacheEvictsExpired(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }

	// Lower the threshold so the next store() triggers a sweep.
	orig := cacheEvictThreshold
	cacheEvictThreshold = 2
	defer func() { cacheEvictThreshold = orig }()

	// Two expired entries that are never re-requested (cached() alone would never
	// evict them), plus a live one.
	a.cache["dead1"] = credentials.Credential{ExpiresAt: now.Add(-time.Hour)}
	a.cache["dead2"] = credentials.Credential{ExpiresAt: now.Add(-time.Hour)}
	a.cache["live"] = credentials.Credential{ExpiresAt: now.Add(time.Hour)}

	// len(cache)==3 >= threshold(2) → store() sweeps expired entries before insert.
	a.store("new", credentials.Credential{ExpiresAt: now.Add(time.Hour)})

	for _, dead := range []string{"dead1", "dead2"} {
		if _, ok := a.cache[dead]; ok {
			t.Errorf("expired entry %q was not swept", dead)
		}
	}
	for _, keep := range []string{"live", "new"} {
		if _, ok := a.cache[keep]; !ok {
			t.Errorf("live entry %q was wrongly evicted", keep)
		}
	}
}

func TestAdapter_Resolve_MintsAndCaches(t *testing.T) {
	var (
		calls   int
		gotAuth string
		gotBody ghclient.InstallationTokenRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":                "ghs_exampletoken",
			"expires_at":           "2026-06-06T13:00:00Z",
			"permissions":          map[string]string{"contents": "write"},
			"repository_selection": "selected",
		})
	}))
	defer srv.Close()

	logger := logging.NewLoggerWithOutput(io.Discard)
	adapter := New(ghclient.NewClient(srv.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	adapter.now = func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) }

	conn := credentials.Connection{
		ID: "conn-1", TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp,
		BaseHost: "github.com", SecretRef: "k8s:orbit-conn-acme-github",
		Config: mustConfig(t, config{
			AppID: "12345", InstallationID: 42,
			Repositories: []string{"orbit"},
			Permissions:  map[string]string{"contents": "write"},
		}),
	}
	id := credentials.CallerIdentity{TenantID: "acme", PrincipalID: "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", RunID: "run-9", Action: "repo:write"}

	cred, err := adapter.Resolve(context.Background(), conn, id)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if cred.Token != "ghs_exampletoken" {
		t.Errorf("Token = %q, want ghs_exampletoken", cred.Token)
	}
	wantValue := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:ghs_exampletoken"))
	if cred.Header != "Authorization" || cred.Value != wantValue {
		t.Errorf("injection = %q: %q, want Authorization: %q", cred.Header, cred.Value, wantValue)
	}
	if want := time.Date(2026, 6, 6, 13, 0, 0, 0, time.UTC); !cred.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", cred.ExpiresAt, want)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Errorf("GitHub call Authorization = %q, want Bearer <app-jwt>", gotAuth)
	}
	if len(gotBody.Repositories) != 1 || gotBody.Repositories[0] != "orbit" {
		t.Errorf("request repositories = %v, want [orbit]", gotBody.Repositories)
	}

	// A second Resolve within validity must be served from cache (no new call).
	if _, err := adapter.Resolve(context.Background(), conn, id); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if calls != 1 {
		t.Errorf("github calls = %d, want 1 (second resolve should hit cache)", calls)
	}
}

func TestAdapter_Resolve_GitHubError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	logger := logging.NewLoggerWithOutput(io.Discard)
	adapter := New(ghclient.NewClient(srv.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	conn := credentials.Connection{
		ID: "conn-1", SecretRef: "ref", Adapter: credentials.AdapterGitHubApp,
		Config: mustConfig(t, config{AppID: "1", InstallationID: 1}),
	}

	_, err := adapter.Resolve(context.Background(), conn, credentials.CallerIdentity{})
	if !errors.Is(err, ghclient.ErrUnexpectedStatus) {
		t.Fatalf("err = %v, want wrapped ErrUnexpectedStatus", err)
	}
}

// TestAdapter_Resolve_InstallationPermissionCeiling locks the translation of a
// GitHub 422 (the App installation lacks the requested permission level) into
// the adapter-agnostic ErrCredentialScopeRejected, so the broker returns a
// forbidden rather than a 500. See the 2026-07-22 category ADR.
func TestAdapter_Resolve_InstallationPermissionCeiling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"The level of access for permissions requested are not granted to this installation."}`))
	}))
	defer srv.Close()

	logger := logging.NewLoggerWithOutput(io.Discard)
	adapter := New(ghclient.NewClient(srv.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	conn := credentials.Connection{
		ID: "conn-1", SecretRef: "ref", Adapter: credentials.AdapterGitHubApp,
		Config: mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: "orbital"}),
	}

	_, err := adapter.Resolve(context.Background(), conn,
		credentials.CallerIdentity{Action: "issues:write", Qualifier: "orbit"})
	if !errors.Is(err, credentials.ErrCredentialScopeRejected) {
		t.Fatalf("err = %v, want wrapped ErrCredentialScopeRejected", err)
	}
	if !errors.Is(err, ghclient.ErrInstallationPermissions) {
		t.Fatalf("err = %v, want the underlying ErrInstallationPermissions preserved", err)
	}
}

func TestAdapter_Resolve_InvalidConfig(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	adapter := New(nil, stubSecrets{pem: testKeyPEM(t)}, logger)
	conn := credentials.Connection{
		ID: "conn-1", SecretRef: "ref", Adapter: credentials.AdapterGitHubApp,
		Config: mustConfig(t, config{}), // no app_id / installation_id
	}

	_, err := adapter.Resolve(context.Background(), conn, credentials.CallerIdentity{})
	if !errors.Is(err, credentials.ErrInvalidConfig) {
		t.Fatalf("err = %v, want wrapped ErrInvalidConfig", err)
	}
}

func TestAdapter_Type(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	if a.Type() != credentials.AdapterGitHubApp {
		t.Errorf("Type() = %q, want %q", a.Type(), credentials.AdapterGitHubApp)
	}
}

func TestAdapter_ClassifyAction(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	conn := credentials.Connection{
		Config: mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: "orbital"}),
	}

	cases := []struct {
		name      string
		method    string
		url       string
		want      string
		qualifier string
		err       bool
	}{
		// git smart-HTTP — discovery and pack transfer, with and without .git
		{"git_clone_discovery", http.MethodGet, "https://github.com/orbital/orbit.git/info/refs?service=git-upload-pack", "contents:read", "orbit", false},
		{"git_clone_no_dotgit", http.MethodGet, "https://github.com/orbital/orbit/info/refs?service=git-upload-pack", "contents:read", "orbit", false},
		{"git_push_discovery", http.MethodGet, "https://github.com/orbital/orbit.git/info/refs?service=git-receive-pack", "contents:write", "orbit", false},
		{"git_upload_pack", http.MethodPost, "https://github.com/orbital/orbit.git/git-upload-pack", "contents:read", "orbit", false},
		{"git_receive_pack", http.MethodPost, "https://github.com/orbital/orbit.git/git-receive-pack", "contents:write", "orbit", false},
		{"git_foreign_owner", http.MethodPost, "https://github.com/evil/orbit.git/git-upload-pack", "", "", true},

		// REST — bare repository object
		{"repo_get", http.MethodGet, "https://api.github.com/repos/orbital/orbit", "metadata:read", "orbit", false},
		{"repo_patch", http.MethodPatch, "https://api.github.com/repos/orbital/orbit", "admin:write", "orbit", false},
		{"repo_delete", http.MethodDelete, "https://api.github.com/repos/orbital/orbit", "admin:write", "orbit", false},

		// REST — category route families
		{"contents_get", http.MethodGet, "https://api.github.com/repos/orbital/orbit/contents/README.md", "contents:read", "orbit", false},
		{"commits_get", http.MethodGet, "https://api.github.com/repos/orbital/orbit/commits/abc", "contents:read", "orbit", false},
		{"commit_status", http.MethodGet, "https://api.github.com/repos/orbital/orbit/commits/abc/status", "checks:read", "orbit", false},
		{"branches_get", http.MethodGet, "https://api.github.com/repos/orbital/orbit/branches/main", "contents:read", "orbit", false},
		{"branch_protection", http.MethodPut, "https://api.github.com/repos/orbital/orbit/branches/main/protection", "admin:write", "orbit", false},
		{"issues_post", http.MethodPost, "https://api.github.com/repos/orbital/orbit/issues", "issues:write", "orbit", false},
		{"labels_get", http.MethodGet, "https://api.github.com/repos/orbital/orbit/labels", "issues:read", "orbit", false},
		{"pulls_get", http.MethodGet, "https://api.github.com/repos/orbital/orbit/pulls/1", "pulls:read", "orbit", false},
		{"pull_merge", http.MethodPut, "https://api.github.com/repos/orbital/orbit/pulls/1/merge", "contents:write", "orbit", false},
		{"actions_dispatch", http.MethodPost, "https://api.github.com/repos/orbital/orbit/actions/workflows/ci.yml/dispatches", "actions:write", "orbit", false},
		{"actions_secrets", http.MethodGet, "https://api.github.com/repos/orbital/orbit/actions/secrets", "admin:read", "orbit", false},
		{"actions_variables_put", http.MethodPut, "https://api.github.com/repos/orbital/orbit/actions/variables/FOO", "admin:write", "orbit", false},
		{"check_runs", http.MethodGet, "https://api.github.com/repos/orbital/orbit/check-runs/1", "checks:read", "orbit", false},
		{"statuses_post", http.MethodPost, "https://api.github.com/repos/orbital/orbit/statuses/abc", "checks:write", "orbit", false},
		{"hooks_post", http.MethodPost, "https://api.github.com/repos/orbital/orbit/hooks", "admin:write", "orbit", false},
		{"collaborators", http.MethodPut, "https://api.github.com/repos/orbital/orbit/collaborators/x", "admin:write", "orbit", false},
		{"dependabot_secrets", http.MethodGet, "https://api.github.com/repos/orbital/orbit/dependabot/secrets", "admin:read", "orbit", false},
		{"releases_post", http.MethodPost, "https://api.github.com/repos/orbital/orbit/releases", "contents:write", "orbit", false},

		// REST — uncategorized repo routes fall to the qualified catch-all
		{"deployments_catchall", http.MethodGet, "https://api.github.com/repos/orbital/orbit/deployments", "repo:read", "orbit", false},

		// REST — owner mismatch on the API surface is denied outright
		{"foreign_owner_api", http.MethodGet, "https://api.github.com/repos/evil/orbit", "", "", true},

		// Repo-less endpoints: unqualified
		{"graphql", http.MethodPost, "https://api.github.com/graphql", "repo:write", "", false},
		{"search", http.MethodGet, "https://api.github.com/search/code?q=x", "repo:read", "", false},
		{"rate_limit", http.MethodGet, "https://api.github.com/rate_limit", "metadata:read", "", false},
		{"user", http.MethodGet, "https://api.github.com/user", "metadata:read", "", false},
		{"installation_repos", http.MethodGet, "https://api.github.com/installation/repositories", "metadata:read", "", false},

		// GitHub Enterprise path prefixes on the appliance host
		{"ghe_issues", http.MethodGet, "https://ghe.example.com/api/v3/repos/orbital/orbit/issues", "issues:read", "orbit", false},
		{"ghe_graphql", http.MethodPost, "https://ghe.example.com/api/graphql", "repo:write", "", false},

		// Content-host family: reads only, qualified when owner-addressed
		{"codeload", http.MethodGet, "https://codeload.github.com/orbital/orbit/tar.gz/main", "contents:read", "orbit", false},
		{"raw", http.MethodGet, "https://raw.githubusercontent.com/orbital/orbit/main/README.md", "contents:read", "orbit", false},
		{"objects_opaque", http.MethodGet, "https://objects.githubusercontent.com/blobs/deadbeef", "contents:read", "", false},
		{"content_host_write", http.MethodPost, "https://codeload.github.com/orbital/orbit/tar.gz/main", "", "", true},

		// Anything else: verb-classified catch-all, unqualified
		{"web_page", http.MethodGet, "https://github.com/orbital", "repo:read", "", false},

		// Unknown verb (deny via ErrUnsupportedAction)
		{"unknown_verb", "PROPFIND", "https://github.com/x", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, nil)
			if err != nil {
				t.Fatalf("build req: %v", err)
			}
			got, err := a.ClassifyAction(req, conn)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				if !errors.Is(err, credentials.ErrUnsupportedAction) {
					t.Errorf("err = %v, want ErrUnsupportedAction", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.Action != tc.want || got.Qualifier != tc.qualifier {
				t.Errorf("ClassifyAction = %q/%q, want %q/%q", got.Action, got.Qualifier, tc.want, tc.qualifier)
			}
		})
	}
}

func TestAdapter_ClassifyAction_NoDeclaredOwner(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	conn := credentials.Connection{Config: mustConfig(t, config{AppID: "1", InstallationID: 1})}

	// Without a declared owner no qualifier is ever extracted — legacy
	// connections keep pre-qualifier semantics (unqualified scopes only).
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/orbital/orbit/issues", nil)
	got, err := a.ClassifyAction(req, conn)
	if err != nil {
		t.Fatalf("ClassifyAction: %v", err)
	}
	if got.Action != "issues:read" || got.Qualifier != "" {
		t.Errorf("got %q/%q, want issues:read with empty qualifier", got.Action, got.Qualifier)
	}
}

func TestAdapter_ClassifyAction_NilRequest(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	if _, err := a.ClassifyAction(nil, credentials.Connection{}); !errors.Is(err, credentials.ErrUnsupportedAction) {
		t.Errorf("nil req: err = %v, want ErrUnsupportedAction", err)
	}
}

func TestPermissionsFor(t *testing.T) {
	cases := []struct {
		name    string
		action  string
		ceiling map[string]string
		want    map[string]string
	}{
		{"metadata", "metadata:read", nil, map[string]string{"metadata": "read"}},
		{"contents_write", "contents:write", nil, map[string]string{"contents": "write", "metadata": "read"}},
		{"issues_read", "issues:read", nil, map[string]string{"issues": "read", "metadata": "read"}},
		{"checks_write", "checks:write", nil, map[string]string{"checks": "write", "statuses": "write", "metadata": "read"}},
		{"admin_read", "admin:read", nil, map[string]string{"administration": "read", "repository_hooks": "read", "secrets": "read", "dependabot_secrets": "read", "metadata": "read"}},
		{"ceiling_drops_and_lowers", "admin:write", map[string]string{"administration": "read", "metadata": "read"},
			map[string]string{"administration": "read", "metadata": "read"}},
		{"umbrella_mints_ceiling_verbatim", "repo:write", map[string]string{"contents": "write", "workflows": "write"},
			map[string]string{"contents": "write", "workflows": "write"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := permissionsFor(tc.action, tc.ceiling)
			if len(got) != len(tc.want) {
				t.Fatalf("permissionsFor(%q) = %v, want %v", tc.action, got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("permissionsFor(%q)[%s] = %q, want %q", tc.action, k, got[k], v)
				}
			}
		})
	}

	// The workflows permission is deliberately excluded from the umbrella's
	// derived union — it appears only when the ceiling declares it.
	if got := permissionsFor("repo:write", nil); got["workflows"] != "" {
		t.Errorf("umbrella derived union must not include workflows, got %q", got["workflows"])
	}
}

func TestAdapter_Resolve_QualifierNarrowsMint(t *testing.T) {
	var gotBody ghclient.InstallationTokenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_tok", "expires_at": "2026-07-22T13:00:00Z"})
	}))
	defer srv.Close()

	logger := logging.NewLoggerWithOutput(io.Discard)
	adapter := New(ghclient.NewClient(srv.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger)
	adapter.now = func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) }

	conn := credentials.Connection{
		ID: "conn-1", TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp,
		SecretRef: "ref",
		Config:    mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: "orbital"}),
	}
	id := credentials.CallerIdentity{TenantID: "acme", Action: "issues:write", Qualifier: "orbit"}

	if _, err := adapter.Resolve(context.Background(), conn, id); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(gotBody.Repositories) != 1 || gotBody.Repositories[0] != "orbit" {
		t.Errorf("minted repositories = %v, want [orbit]", gotBody.Repositories)
	}
	if gotBody.Permissions["issues"] != "write" || gotBody.Permissions["metadata"] != "read" {
		t.Errorf("minted permissions = %v, want issues:write + metadata:read", gotBody.Permissions)
	}
	if _, ok := gotBody.Permissions["contents"]; ok {
		t.Errorf("issues:write token must not carry contents, got %v", gotBody.Permissions)
	}
}
