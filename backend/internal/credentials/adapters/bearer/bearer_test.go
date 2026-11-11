package bearer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/bearer"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const secretToken = "ghp_supersecrettoken"

type stubSecrets struct {
	token []byte
	err   error
}

func (s stubSecrets) Resolve(context.Context, string) ([]byte, error) { return s.token, s.err }

func open(t *testing.T, secrets credentials.SecretResolver, cfg string, logs *bytes.Buffer) (credentials.Connector, error) {
	t.Helper()
	var out bytes.Buffer
	if logs == nil {
		logs = &out
	}
	a := bearer.New(secrets, logging.NewLoggerWithOutput(logs))
	conn := credentials.Connection{Name: "npm", SecretRef: "ref"}
	if cfg != "" {
		conn.Config = json.RawMessage(cfg)
	}
	return a.Open(conn)
}

func mustOpen(t *testing.T, cfg string) credentials.Connector {
	t.Helper()
	c, err := open(t, stubSecrets{token: []byte(secretToken)}, cfg, nil)
	if err != nil {
		t.Fatalf("Open(%q): %v", cfg, err)
	}
	return c
}

func TestAdapter_TypeAndActions(t *testing.T) {
	a := bearer.New(stubSecrets{}, logging.NewLoggerWithOutput(&bytes.Buffer{}))
	if a.Type() != credentials.AdapterBearer {
		t.Errorf("Type() = %q, want %q", a.Type(), credentials.AdapterBearer)
	}
	var got []string
	for _, d := range a.Actions() {
		got = append(got, d.Action)
	}
	if strings.Join(got, ",") != "read,write" {
		t.Errorf("Actions() = %v, want [read write]", got)
	}
}

func TestClassify(t *testing.T) {
	c := mustOpen(t, "")
	tests := []struct {
		method string
		want   string
	}{
		{http.MethodGet, bearer.ActionRead},
		{http.MethodHead, bearer.ActionRead},
		{http.MethodPost, bearer.ActionWrite},
		{http.MethodPut, bearer.ActionWrite},
		{http.MethodPatch, bearer.ActionWrite},
		{http.MethodDelete, bearer.ActionWrite},
	}
	for _, tt := range tests {
		req, _ := http.NewRequest(tt.method, "https://npm.pkg.github.com/x", nil)
		got, err := c.Classify(req)
		if err != nil || got.Action != tt.want {
			t.Errorf("Classify(%s) = %+v, %v; want %q", tt.method, got, err, tt.want)
		}
	}
}

func TestClassify_Unsupported(t *testing.T) {
	c := mustOpen(t, "")
	opts, _ := http.NewRequest(http.MethodOptions, "https://npm.pkg.github.com/x", nil)
	up, _ := http.NewRequest(http.MethodGet, "https://npm.pkg.github.com/x", nil)
	up.Header.Set("Connection", "Upgrade")
	up.Header.Set("Upgrade", "websocket")
	trace, _ := http.NewRequest(http.MethodTrace, "https://npm.pkg.github.com/x", nil)
	for name, req := range map[string]*http.Request{"options": opts, "upgrade": up, "trace": trace, "nil": nil} {
		if _, err := c.Classify(req); !errors.Is(err, credentials.ErrUnsupportedAction) {
			t.Errorf("Classify(%s) err = %v, want ErrUnsupportedAction", name, err)
		}
	}
}

func TestMint_Default(t *testing.T) {
	cred, err := mustOpen(t, "").Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if cred.Header != "Authorization" || cred.Value != "Bearer "+secretToken || cred.Token != secretToken {
		t.Errorf("Mint = %+v", cred)
	}
	if !cred.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero", cred.ExpiresAt)
	}
}

func TestMint_CustomHeaderAndPrefix(t *testing.T) {
	cred, err := mustOpen(t, `{"header":"X-Api-Key","prefix":"Token "}`).Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if cred.Header != "X-Api-Key" || cred.Value != "Token "+secretToken {
		t.Errorf("Mint = %+v", cred)
	}
}

func TestMint_EmptyPrefixIsRawToken(t *testing.T) {
	cred, err := mustOpen(t, `{"prefix":""}`).Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if cred.Value != secretToken {
		t.Errorf("Value = %q, want raw token", cred.Value)
	}
}

func TestMint_TrimsTrailingNewline(t *testing.T) {
	c, err := open(t, stubSecrets{token: []byte(secretToken + "\n")}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := c.Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil || cred.Value != "Bearer "+secretToken {
		t.Errorf("Mint = %+v, %v", cred, err)
	}
}

func TestMint_ResolveFailure(t *testing.T) {
	boom := errors.New("boom")
	c, err := open(t, stubSecrets{err: boom}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mint(context.Background(), credentials.CallerIdentity{}); !errors.Is(err, boom) {
		t.Errorf("Mint err = %v, want wrapping %v", err, boom)
	}
}

func TestMint_NeverLogsToken(t *testing.T) {
	var logs bytes.Buffer
	c, err := open(t, stubSecrets{token: []byte(secretToken)}, "", &logs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mint(context.Background(), credentials.CallerIdentity{TenantID: "t", RunID: "r"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), secretToken) {
		t.Errorf("logs contain the token: %s", logs.String())
	}
}

func TestOpen_Config(t *testing.T) {
	valid := []string{"", "null", "{}", `{"placeholderEnv":"NPM_TOKEN"}`, `{"header":"X-Token","prefix":"","placeholderEnv":"A_1"}`}
	for _, cfg := range valid {
		if _, err := open(t, stubSecrets{}, cfg, nil); err != nil {
			t.Errorf("Open(%q) = %v, want nil", cfg, err)
		}
	}
	invalid := []string{
		`{"hedaer":"X"}`,
		`{"header":"bad name"}`,
		`{"header":"X:Y"}`,
		`{"header":"Host"}`,
		`{"header":"content-length"}`,
		`{"header":"Transfer-Encoding"}`,
		`{"header":"Connection"}`,
		`{"prefix":"a\nb"}`,
		`{"header":1}`,
		`[]`,
		`{} {}`,
	}
	for _, cfg := range invalid {
		if _, err := open(t, stubSecrets{}, cfg, nil); !errors.Is(err, bearer.ErrInvalidConfig) {
			t.Errorf("Open(%q) err = %v, want ErrInvalidConfig", cfg, err)
		}
	}
}

func TestMint_EmptyTokenFails(t *testing.T) {
	for _, raw := range []string{"", " \n\t"} {
		c, err := open(t, stubSecrets{token: []byte(raw)}, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Mint(context.Background(), credentials.CallerIdentity{}); err == nil {
			t.Errorf("Mint(%q) succeeded, want error", raw)
		}
	}
}
