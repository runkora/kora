package apikey_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/apikey"
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
	a := apikey.New(secrets, logging.NewLoggerWithOutput(logs))
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
	a := apikey.New(stubSecrets{}, logging.NewLoggerWithOutput(&bytes.Buffer{}))
	if a.Type() != credentials.AdapterAPIKey {
		t.Errorf("Type() = %q, want %q", a.Type(), credentials.AdapterAPIKey)
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
		{http.MethodGet, apikey.ActionRead},
		{http.MethodHead, apikey.ActionRead},
		{http.MethodPost, apikey.ActionWrite},
		{http.MethodPut, apikey.ActionWrite},
		{http.MethodPatch, apikey.ActionWrite},
		{http.MethodDelete, apikey.ActionWrite},
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

func hdrs(c credentials.Credential) string {
	var parts []string
	for _, h := range c.Headers {
		parts = append(parts, h.Name+": "+h.Value)
	}
	return strings.Join(parts, "|")
}

func TestMint_Default(t *testing.T) {
	cred, err := mustOpen(t, "").Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got := hdrs(cred); got != "Authorization: Bearer "+secretToken || cred.Token != secretToken {
		t.Errorf("Mint = %+v", cred)
	}
	if !cred.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero", cred.ExpiresAt)
	}
}

func TestMint_HeadersWithoutPrefixIsRaw(t *testing.T) {
	cred, err := mustOpen(t, `{"headers":[{"name":"x-api-key"}]}`).Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got := hdrs(cred); got != "x-api-key: "+secretToken {
		t.Errorf("Mint = %q", got)
	}
}

func TestMint_MultipleHeaders(t *testing.T) {
	cfg := `{"headers":[{"name":"Authorization","prefix":"Token "},{"name":"x-api-key"}]}`
	cred, err := mustOpen(t, cfg).Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	want := "Authorization: Token " + secretToken + "|x-api-key: " + secretToken
	if got := hdrs(cred); got != want {
		t.Errorf("Mint = %q, want %q", got, want)
	}
}

func TestMint_TrimsTrailingNewline(t *testing.T) {
	c, err := open(t, stubSecrets{token: []byte(secretToken + "\n")}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := c.Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil || hdrs(cred) != "Authorization: Bearer "+secretToken {
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
	valid := []string{
		"", "null", "{}", `{"placeholderEnv":"NPM_TOKEN"}`,
		`{"headers":[{"name":"X-Token","prefix":""}],"placeholderEnv":"A_1"}`,
		`{"headers":[{"name":"` + strings.Repeat("a", 64) + `"}]}`,
		`{"headers":[{"name":"a","prefix":"` + strings.Repeat("p", 256) + `"}]}`,
	}
	for _, cfg := range valid {
		if _, err := open(t, stubSecrets{}, cfg, nil); err != nil {
			t.Errorf("Open(%q) = %v, want nil", cfg, err)
		}
	}
	var nine []string
	for i := range 9 {
		nine = append(nine, fmt.Sprintf(`{"name":"h%d"}`, i))
	}
	invalid := []string{
		`{"hedaers":[]}`,
		`{"header":"X"}`,
		`{"headers":[]}`,
		`{"headers":[{"name":"bad name"}]}`,
		`{"headers":[{"name":"X:Y"}]}`,
		`{"headers":[{"name":""}]}`,
		`{"headers":[{"name":"Host"}]}`,
		`{"headers":[{"name":"content-length"}]}`,
		`{"headers":[{"name":"Transfer-Encoding"}]}`,
		`{"headers":[{"name":"X-Forwarded-For"}]}`,
		`{"headers":[{"name":"x-envoy-foo"}]}`,
		`{"headers":[{"name":"X-Kora-Run-Id"}]}`,
		`{"headers":[{"name":"a"},{"name":"A"}]}`,
		`{"headers":[{"name":"a","secretRef":"x"}]}`,
		`{"headers":[{"name":"` + strings.Repeat("a", 65) + `"}]}`,
		`{"headers":[{"name":"a","prefix":"` + strings.Repeat("p", 257) + `"}]}`,
		`{"headers":[{"name":"a","prefix":"a\nb"}]}`,
		`{"headers":[` + strings.Join(nine, ",") + `]}`,
		`{"headers":"x"}`,
		`[]`,
		`{} {}`,
	}
	for _, cfg := range invalid {
		if _, err := open(t, stubSecrets{}, cfg, nil); !errors.Is(err, credentials.ErrInvalidConfig) {
			t.Errorf("Open(%q) err = %v, want credentials.ErrInvalidConfig", cfg, err)
		}
	}
}

func TestMint_EmptyTokenFails(t *testing.T) {
	for _, raw := range []string{"", " \n\t"} {
		c, err := open(t, stubSecrets{token: []byte(raw)}, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Mint(context.Background(), credentials.CallerIdentity{}); !errors.Is(err, apikey.ErrEmptySecret) {
			t.Errorf("Mint(%q) err = %v, want ErrEmptySecret", raw, err)
		}
	}
}
