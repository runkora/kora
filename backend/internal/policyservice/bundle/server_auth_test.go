package bundle

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
)

const authRegistry = `version: v1
tenants:
  - {id: a, state: active, namespace: kora-tenant-a}
  - {id: b, state: active}
  - {id: acme, state: active, namespace: acme-prod}
`

const td = "spiffe://runkora.test"

func newAuthServer(t *testing.T, registryBody string) *Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tenants.yaml")
	if registryBody != "" {
		if err := os.WriteFile(path, []byte(registryBody), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compiler := NewCompiler(Config{JWKSURL: "http://127.0.0.1:1/jwks", Issuer: "kora", Audience: "kora-platform"}, routes.NewStore())
	for _, tid := range []string{"a", "acme"} {
		if _, err := compiler.Compile(tid, nil, nil, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{
		compiler: compiler,
		tenants:  declarative.NewRegistry(path),
		logger:   logging.NewLoggerWithOutput(io.Discard),
	}
}

func fetch(s *Server, tenant, xfcc, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/bundles/"+tenant, nil)
	req.SetPathValue("tenant_id", tenant)
	req.RemoteAddr = remote
	if xfcc != "" {
		req.Header.Set("x-forwarded-client-cert", xfcc)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func uri(path string) string { return "By=spiffe://runkora.test/system/envoy;URI=" + td + path }

func TestServeHTTPBindsTenantCallerToItsTenant(t *testing.T) {
	s := newAuthServer(t, authRegistry)
	const pod = "10.0.0.5:4000"
	tests := []struct {
		name, tenant, xfcc string
		want               int
	}{
		{"own tenant", "a", uri("/tenant/kora-tenant-a/agent/x"), 200},
		{"own tenant sandbox run", "a", uri("/tenant/kora-tenant-a/agent/x/sandbox/run-1"), 200},
		{"other tenant", "b", uri("/tenant/kora-tenant-a/agent/x"), 403},
		{"custom namespace own tenant", "acme", uri("/tenant/acme-prod/agent/x"), 200},
		{"custom namespace other tenant", "a", uri("/tenant/acme-prod/agent/x"), 403},
		{"unknown namespace", "a", uri("/tenant/nowhere/agent/x"), 403},
		{"another tenant's default namespace name", "acme", uri("/tenant/kora-tenant-acme/agent/x"), 403},
		{"service principal own tenant", "a", uri("/tenant/kora-tenant-a/service/svc"), 200},
		{"malformed xfcc", "a", "garbage", 403},
		{"no xfcc from pod", "a", "", 403},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := fetch(s, tc.tenant, tc.xfcc, pod)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == 403 && rec.Header().Get("Content-Type") == "application/vnd.openpolicyagent.bundle" {
				t.Error("403 response carried bundle content type")
			}
		})
	}
}

func TestServeHTTPAllowsSystemComponentsForAnyTenant(t *testing.T) {
	s := newAuthServer(t, authRegistry)
	for _, c := range []string{"identity", "kora-agents", "skills-service", "inference-gateway", "policy-gateway", "tool-gateway", "kora-triggers", "egress-gateway"} {
		t.Run(c, func(t *testing.T) {
			for _, tenant := range []string{"a", "acme"} {
				if rec := fetch(s, tenant, uri("/system/"+c), "10.0.0.5:4000"); rec.Code != 200 {
					t.Errorf("%s reading %s: status %d", c, tenant, rec.Code)
				}
			}
		})
	}
}

func TestServeHTTPLoopback(t *testing.T) {
	s := newAuthServer(t, authRegistry)
	for remote, want := range map[string]int{
		"127.0.0.1:1234": 200,
		"[::1]:1234":     200,
		"127.0.0.6:1234": 403,
		"10.0.0.5:1234":  403,
		"nonsense":       403,
	} {
		if rec := fetch(s, "a", "", remote); rec.Code != want {
			t.Errorf("remote %s: status %d, want %d", remote, rec.Code, want)
		}
	}
}

func TestServeHTTPUnloadedRegistryDeniesTenantCallers(t *testing.T) {
	s := newAuthServer(t, "")
	if rec := fetch(s, "a", uri("/tenant/kora-tenant-a/agent/x"), "10.0.0.5:1"); rec.Code != 403 {
		t.Errorf("tenant caller: status %d, want 403", rec.Code)
	}
	if rec := fetch(s, "a", uri("/system/identity"), "10.0.0.5:1"); rec.Code != 200 {
		t.Errorf("system caller: status %d, want 200", rec.Code)
	}
}

func TestServeHTTPForbiddenBodyDoesNotRevealBundle(t *testing.T) {
	s := newAuthServer(t, authRegistry)
	xfcc := uri("/tenant/kora-tenant-b/agent/x")
	withBundle := fetch(s, "a", xfcc, "10.0.0.5:1")      // bundle exists
	withoutBundle := fetch(s, "zzz", xfcc, "10.0.0.5:1") // none compiled
	if withBundle.Code != 403 || withoutBundle.Code != 403 {
		t.Fatalf("statuses %d, %d, want 403", withBundle.Code, withoutBundle.Code)
	}
	if withBundle.Body.String() != withoutBundle.Body.String() {
		t.Errorf("403 bodies differ: %q vs %q", withBundle.Body, withoutBundle.Body)
	}
}
