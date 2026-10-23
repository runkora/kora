package toolgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

func TestUpstreamCredentials_ForwardIsGlobalOnly(t *testing.T) {
	caller := &Caller{Bearer: "agent-token", Delegation: "delegation-token"}
	cases := []struct {
		name           string
		srv            Server
		caller         *Caller
		wantAuth       string
		wantDelegation string
	}{
		{"global forward", Server{CallerIdentity: callerIdentityForward}, caller, "Bearer agent-token", "delegation-token"},
		{"global forward without delegation", Server{CallerIdentity: callerIdentityForward}, &Caller{Bearer: "agent-token"}, "Bearer agent-token", ""},
		{"global forward without bearer", Server{CallerIdentity: callerIdentityForward}, &Caller{Delegation: "delegation-token"}, "", ""},
		{"global forward introspection", Server{CallerIdentity: callerIdentityForward}, nil, "", ""},
		{"global none", Server{CallerIdentity: "none"}, caller, "", ""},
		{"tenant forward", Server{TenantID: "acme", CallerIdentity: callerIdentityForward}, caller, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewUpstreamCredentials("").Headers(context.Background(), tc.srv, tc.caller)
			if err != nil {
				t.Fatalf("Headers: %v", err)
			}
			if got := h.Get("Authorization"); got != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tc.wantAuth)
			}
			if got := h.Get(mw.DelegationTokenHeader); got != tc.wantDelegation {
				t.Errorf("delegation = %q, want %q", got, tc.wantDelegation)
			}
		})
	}
}

// redirectingUpstream redirects every request to a second server and counts
// the requests that reach it.
func redirectingUpstream(t *testing.T) (endpoint string, followed *atomic.Int32) {
	t.Helper()
	followed = new(atomic.Int32)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/mcp", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	return redirect.URL + "/mcp", followed
}

func TestProxyCallTool_DoesNotFollowRedirects(t *testing.T) {
	endpoint, followed := redirectingUpstream(t)
	srv := Server{Name: "kora", Endpoint: endpoint, CallerIdentity: callerIdentityForward}

	_, err := NewProxy(NewUpstreamCredentials(""), "test", logging.NewLogger()).
		CallTool(context.Background(), srv, "echo", nil, &Caller{Bearer: "agent-token"})
	if err == nil {
		t.Fatal("CallTool succeeded through a redirect")
	}
	if n := followed.Load(); n != 0 {
		t.Errorf("redirect target received %d request(s), want 0", n)
	}
}

func TestIntrospector_DoesNotFollowRedirects(t *testing.T) {
	endpoint, followed := redirectingUpstream(t)
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())

	if _, err := i.introspectOne(context.Background(), Server{Name: "x", Endpoint: endpoint}); err == nil {
		t.Fatal("introspection succeeded through a redirect")
	}
	if n := followed.Load(); n != 0 {
		t.Errorf("redirect target received %d request(s), want 0", n)
	}
}
