package toolgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/runkora/kora/backend/internal/platform/logging"
	mcpkit "github.com/runkora/kora/backend/internal/platform/mcp"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
)

const (
	goodBearer = "bearer-secret-value"
	goodAPIKey = "api-key-secret-value"
	wrongKey   = "wrong-secret-value"
)

// secretsDir writes the mounted Secret's files and returns the directory.
func secretsDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for k, v := range files {
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var authHeaders = []AuthHeader{
	{Name: "Authorization", Prefix: "Bearer ", SecretKey: "ns.exa.0"},
	{Name: "x-api-key", SecretKey: "ns.exa.1"},
}

func TestUpstreamCredentials_StaticHeaders(t *testing.T) {
	dir := secretsDir(t, map[string]string{"ns.exa.0": goodBearer, "ns.exa.1": goodAPIKey})
	creds := NewUpstreamCredentials(dir)
	srv := Server{Name: "exa", AuthHeaders: authHeaders}

	// The caller is ignored for a server with its own credential.
	h, err := creds.Headers(context.Background(), srv, &Caller{Bearer: "agent-token"})
	if err != nil {
		t.Fatalf("Headers: %v", err)
	}
	if h.Get("Authorization") != "Bearer "+goodBearer || h.Get("X-Api-Key") != goodAPIKey {
		t.Errorf("headers = %v", h)
	}

	for _, key := range []string{"missing", "", "../ns.exa.0", "..data", "a/b"} {
		bad := Server{Name: "exa", AuthHeaders: []AuthHeader{{Name: "x-api-key", SecretKey: key}}}
		if _, err := creds.Headers(context.Background(), bad, nil); !errors.Is(err, ErrCredentialUnavailable) {
			t.Errorf("key %q: err = %v, want ErrCredentialUnavailable", key, err)
		}
	}
}

// authedUpstream serves one tool and demands both credential headers. A
// rejected request gets a JSON-RPC error that echoes what it was sent.
func authedUpstream(t *testing.T) (endpoint string, requests *atomic.Int32) {
	t.Helper()
	requests = new(atomic.Int32)
	upstream := mcpkit.NewServer("upstream", "v0")
	upstream.AddTool(&sdk.Tool{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		})
	handler := mcpkit.NewController("/mcp", upstream).Handler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		auth, key := r.Header.Get("Authorization"), r.Header.Get("X-Api-Key")
		if auth != "Bearer "+goodBearer || key != goodAPIKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"bad credentials %s %s"}}`, auth, key)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp", requests
}

func TestIntrospector_StaticCredentialLifecycle(t *testing.T) {
	endpoint, _ := authedUpstream(t)
	dir := secretsDir(t, map[string]string{"ns.exa.0": goodBearer, "ns.exa.1": goodAPIKey})
	var logs bytes.Buffer
	i := NewIntrospector(NewUpstreamCredentials(dir), "test", logging.NewLoggerWithOutput(&logs))
	servers := []Server{{TenantID: "acme", Name: "exa", Endpoint: endpoint, AuthHeaders: authHeaders}}
	snapshot := func() ServerSnapshot {
		i.Sync(context.Background(), servers, true)
		return i.Snapshot()[0]
	}

	if s := snapshot(); s.State != StateOK || len(s.Tools) != 1 {
		t.Fatalf("with the right key: %+v", s)
	}

	writeKey := func(v string) {
		if err := os.WriteFile(filepath.Join(dir, "ns.exa.1"), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeKey(wrongKey)
	s := snapshot()
	if s.State != StateDegraded || len(s.Tools) != 1 || s.LastErr != "auth_failed: HTTP 401" {
		t.Errorf("with a wrong key: state %s, tools %d, err %q", s.State, len(s.Tools), s.LastErr)
	}

	if err := os.Remove(filepath.Join(dir, "ns.exa.1")); err != nil {
		t.Fatal(err)
	}
	s = snapshot()
	if s.State != StateDegraded || len(s.Tools) != 1 || !strings.Contains(s.LastErr, "credential_unavailable") {
		t.Errorf("with no key: state %s, tools %d, err %q", s.State, len(s.Tools), s.LastErr)
	}

	// Rotation back applies on the next read, with no restart.
	writeKey(goodAPIKey)
	if s = snapshot(); s.State != StateOK {
		t.Errorf("after rotation: %+v", s)
	}

	for _, v := range []string{goodBearer, goodAPIKey, wrongKey} {
		if strings.Contains(logs.String(), v) {
			t.Errorf("logs hold credential %q:\n%s", v, logs.String())
		}
	}
	if !strings.Contains(logs.String(), `"reason":"auth_failed"`) || !strings.Contains(logs.String(), `"reason":"credential_unavailable"`) {
		t.Errorf("logs miss the failure reasons:\n%s", logs.String())
	}
}

func TestToolsCall_StaticCredentialFailsClosed(t *testing.T) {
	cases := []struct {
		name         string
		files        map[string]string
		wantMessage  string
		wantUpstream bool
	}{
		{"right key", map[string]string{"ns.exa.0": goodBearer, "ns.exa.1": goodAPIKey}, "", true},
		{"wrong key", map[string]string{"ns.exa.0": goodBearer, "ns.exa.1": wrongKey}, "upstream call failed", true},
		{"missing key", map[string]string{"ns.exa.0": goodBearer}, "upstream credential unavailable", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, requests := authedUpstream(t)
			reg := &Registry{servers: []Server{{TenantID: "acme", Name: "exa", Endpoint: endpoint, AuthHeaders: authHeaders}}, loaded: true}
			var logs bytes.Buffer
			logger := logging.NewLoggerWithOutput(&logs)
			c := NewMCPController(nil, &fakeEval{checkOK: true}, &memCatalog{}, reg,
				NewProxy(NewUpstreamCredentials(secretsDir(t, tc.files)), "test", logger), nil, "/v1", logger)

			claims := &platformtoken.PlatformClaims{}
			claims.Kora.TenantID = "acme"
			claims.Subject = "agent-1"
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{}}}`
			req := httptest.NewRequest(http.MethodPost, "/v1/mcp/exa", strings.NewReader(body)).WithContext(platformtoken.WithClaims(context.Background(), claims))
			req.SetPathValue("server", "exa")
			req.Header.Set("Authorization", "Bearer agent-token")
			rec := httptest.NewRecorder()
			c.handleMCP(rec, req)

			var resp jsonRPCResponse
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			gotMessage := ""
			if resp.Error != nil {
				gotMessage = resp.Error.Message
			}
			if gotMessage != tc.wantMessage {
				t.Errorf("error message = %q, want %q", gotMessage, tc.wantMessage)
			}
			if got := requests.Load() > 0; got != tc.wantUpstream {
				t.Errorf("upstream reached = %v, want %v", got, tc.wantUpstream)
			}
			for _, v := range []string{goodBearer, goodAPIKey, wrongKey} {
				if strings.Contains(rec.Body.String(), v) || strings.Contains(logs.String(), v) {
					t.Errorf("response or logs hold credential %q", v)
				}
			}
		})
	}
}

// Any upstream error status from a server sent credentials is reported by
// status alone. The body may echo what it was sent.
func TestUpstreamErrors_DoNotCarryCredentials(t *testing.T) {
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad request %s %s"}}`,
			r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"))
	}))
	t.Cleanup(echo.Close)
	dir := secretsDir(t, map[string]string{"ns.exa.0": goodBearer, "ns.exa.1": goodAPIKey})
	srv := Server{Name: "exa", Endpoint: echo.URL + "/mcp", AuthHeaders: authHeaders}
	creds := NewUpstreamCredentials(dir)

	_, introErr := NewIntrospector(creds, "test", testLogger()).introspectOne(context.Background(), srv)
	_, callErr := NewProxy(creds, "test", testLogger()).CallTool(context.Background(), srv, "search", nil, nil)
	for _, err := range []error{introErr, callErr} {
		if !errors.Is(err, ErrUpstreamStatus) || strings.Contains(err.Error(), goodBearer) || strings.Contains(err.Error(), goodAPIKey) {
			t.Errorf("err = %v, want ErrUpstreamStatus without the credential", err)
		}
	}
}

func TestUpstreamError_RedactsSentValues(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+goodBearer)
	_, upstream := upstreamClient(0, "test", h)
	err := upstream.upstreamError(fmt.Errorf("decode: unexpected %s", goodBearer))
	if strings.Contains(err.Error(), goodBearer) || !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("err = %q", err)
	}
}
