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

	cases := []struct {
		name   string
		method string
		url    string
		want   string
		err    bool
	}{
		// git smart-HTTP — discovery
		{"git_clone_discovery", http.MethodGet, "https://github.com/orbit/orbit.git/info/refs?service=git-upload-pack", "repo:read", false},
		{"git_push_discovery", http.MethodGet, "https://github.com/orbit/orbit.git/info/refs?service=git-receive-pack", "repo:write", false},

		// git smart-HTTP — pack transfer
		{"git_upload_pack", http.MethodPost, "https://github.com/orbit/orbit.git/git-upload-pack", "repo:read", false},
		{"git_receive_pack", http.MethodPost, "https://github.com/orbit/orbit.git/git-receive-pack", "repo:write", false},

		// REST verb mapping (any path that isn't a recognized git path)
		{"rest_get", http.MethodGet, "https://api.github.com/repos/orbit/orbit", "repo:read", false},
		{"rest_head", http.MethodHead, "https://api.github.com/repos/orbit/orbit", "repo:read", false},
		{"rest_post", http.MethodPost, "https://api.github.com/repos/orbit/orbit/issues", "repo:write", false},
		{"rest_patch", http.MethodPatch, "https://api.github.com/repos/orbit/orbit/issues/1", "repo:write", false},
		{"rest_delete", http.MethodDelete, "https://api.github.com/repos/orbit/orbit/issues/1", "repo:write", false},

		// info/refs without a service= query is malformed and unsupported
		{"info_refs_no_service", http.MethodGet, "https://github.com/orbit/orbit.git/info/refs", "repo:read", false},

		// Unknown verb (deny via ErrUnsupportedAction)
		{"unknown_verb", "PROPFIND", "https://github.com/x", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, nil)
			if err != nil {
				t.Fatalf("build req: %v", err)
			}
			got, err := a.ClassifyAction(req)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				if !errors.Is(err, credentials.ErrUnsupportedAction) {
					t.Errorf("err = %v, want ErrUnsupportedAction", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("ClassifyAction = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAdapter_ClassifyAction_NilRequest(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	if _, err := a.ClassifyAction(nil); !errors.Is(err, credentials.ErrUnsupportedAction) {
		t.Errorf("nil req: err = %v, want ErrUnsupportedAction", err)
	}
}
