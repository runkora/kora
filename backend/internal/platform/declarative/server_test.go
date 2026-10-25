package declarative

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

type fakeApplier struct {
	applied Item
	deleted [2]string
	listed  string
	result  Result
	items   []Declared
	err     error
}

func (f *fakeApplier) Apply(_ context.Context, item Item) (Result, error) {
	f.applied = item
	return f.result, f.err
}

func (f *fakeApplier) Delete(_ context.Context, tenantID, name string) error {
	f.deleted = [2]string{tenantID, name}
	return f.err
}

func (f *fakeApplier) List(_ context.Context, tenantID string) ([]Declared, error) {
	f.listed = tenantID
	return f.items, f.err
}

// claimsFromHeader stands in for token verification: X-Test-Principal holds
// "TYPE/name".
var claimsFromHeader = mw.NewMiddleware(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.Header.Get("X-Test-Principal"); p != "" {
			typ, name, _ := strings.Cut(p, "/")
			claims := &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: token.PrincipalType(typ), DisplayName: name}}
			r = r.WithContext(token.WithClaims(r.Context(), claims))
		}
		next.ServeHTTP(w, r)
	})
})

func newTestServer(t *testing.T, appliers map[string]Applier) *httptest.Server {
	t.Helper()
	logger := logging.NewLoggerWithOutput(io.Discard)
	c := NewController(nil, logger, appliers)
	c.auth = claimsFromHeader
	srv := httptest.NewServer(api.NewAPI(context.Background(), logger).Server(context.Background(), 0, "/", nil, c).Handler)
	t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, srv *httptest.Server, method, path, principal, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if principal != "" {
		req.Header.Set("X-Test-Principal", principal)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

const operator = "SYSTEM/operator"

func TestControllerApply(t *testing.T) {
	agents := &fakeApplier{result: Result{Outcome: OutcomeProjected, ID: "a1", Ready: true}}
	srv := newTestServer(t, map[string]Applier{KindAgents: agents, KindEnvironments: &fakeApplier{}})

	status, body := call(t, srv, http.MethodPost, "/internal/declared/agents", operator,
		`{"tenant_id":"acme","name":"reviewer","uid":"u1","generation":2,"spec":{"displayName":"R"},"refs":{"go-build":"e1"},"secret":"c2VjcmV0"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	var res Result
	if err := json.Unmarshal([]byte(body), &res); err != nil || res != agents.result {
		t.Errorf("result = %+v (%v), want %+v", res, err, agents.result)
	}
	got := agents.applied
	if got.TenantID != "acme" || got.Name != "reviewer" || got.UID != "u1" || got.Generation != 2 ||
		string(got.Spec) != `{"displayName":"R"}` || got.Refs["go-build"] != "e1" || string(got.Secret) != "secret" {
		t.Errorf("applied item = %+v", got)
	}
}

func TestControllerApplyErrors(t *testing.T) {
	failing := &fakeApplier{err: errors.New("db down")}
	srv := newTestServer(t, map[string]Applier{KindAgents: failing})
	cases := []struct {
		name, path, body string
		want             int
	}{
		{"unknown kind", "/internal/declared/widgets", `{"tenant_id":"a","name":"n","uid":"u","spec":{}}`, http.StatusNotFound},
		{"missing uid", "/internal/declared/agents", `{"tenant_id":"a","name":"n","spec":{}}`, http.StatusBadRequest},
		{"missing spec", "/internal/declared/agents", `{"tenant_id":"a","name":"n","uid":"u"}`, http.StatusBadRequest},
		{"bad json", "/internal/declared/agents", `{`, http.StatusBadRequest},
		{"applier error", "/internal/declared/agents", `{"tenant_id":"a","name":"n","uid":"u","spec":{}}`, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status, body := call(t, srv, http.MethodPost, tc.path, operator, tc.body); status != tc.want {
				t.Errorf("status %d, want %d: %s", status, tc.want, body)
			}
		})
	}
}

func TestControllerDeleteAndList(t *testing.T) {
	triggers := &fakeApplier{items: []Declared{{Name: "t1", UID: "u1"}}}
	srv := newTestServer(t, map[string]Applier{KindTriggers: triggers, KindTriggerSources: &fakeApplier{}})

	if status, _ := call(t, srv, http.MethodDelete, "/internal/declared/triggers/acme/t1", operator, ""); status != http.StatusNoContent {
		t.Errorf("delete status %d", status)
	}
	if triggers.deleted != [2]string{"acme", "t1"} {
		t.Errorf("deleted %v", triggers.deleted)
	}

	status, body := call(t, srv, http.MethodGet, "/internal/declared/triggers/acme", operator, "")
	if status != http.StatusOK || triggers.listed != "acme" {
		t.Fatalf("list status %d tenant %q", status, triggers.listed)
	}
	var list struct{ Items []Declared }
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Items) != 1 || list.Items[0] != (Declared{Name: "t1", UID: "u1"}) {
		t.Errorf("list body %s", body)
	}

	if _, body := call(t, srv, http.MethodGet, "/internal/declared/triggersources/acme", operator, ""); !strings.Contains(body, `"items":[]`) {
		t.Errorf("an empty list is [], got %s", body)
	}
}

func TestControllerAdmitsOnlyTheOperator(t *testing.T) {
	srv := newTestServer(t, map[string]Applier{KindAgents: &fakeApplier{}})
	for _, principal := range []string{"", "SYSTEM/kora-agents", "SERVICE/operator", "HUMAN/operator"} {
		if status, _ := call(t, srv, http.MethodGet, "/internal/declared/agents/acme", principal, ""); status != http.StatusForbidden {
			t.Errorf("principal %q: status %d, want 403", principal, status)
		}
	}
}

func TestControllerWithoutTokenIsUnauthorized(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	verifier := token.NewVerifier(context.Background(), "http://127.0.0.1:1/jwks", "kora", "kora-platform")
	c := NewController(verifier, logger, map[string]Applier{KindAgents: &fakeApplier{}})
	srv := httptest.NewServer(api.NewAPI(context.Background(), logger).Server(context.Background(), 0, "/", nil, c).Handler)
	t.Cleanup(srv.Close)
	if status, _ := call(t, srv, http.MethodGet, "/internal/declared/agents/acme", "", ""); status != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", status)
	}
}
