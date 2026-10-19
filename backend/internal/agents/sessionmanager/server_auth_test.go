package sessionmanager

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// okHandler records that the request reached the route.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func newAuthController(t *testing.T, verifier *token.Verifier) *Controller {
	t.Helper()
	return &Controller{
		logger:   logging.NewLoggerWithOutput(io.Discard),
		verifier: verifier,
	}
}

// unreachableVerifier points at a JWKS URL that is never dialled. Every code
// path exercised here fails before key retrieval (missing or malformed
// bearer), so the verifier only needs to exist.
func unreachableVerifier(t *testing.T) *token.Verifier {
	t.Helper()
	return token.NewVerifier(context.Background(), "http://127.0.0.1:1/jwks.json", "orbit", "orbit-platform")
}

// ── route classification ──────────────────────────────────────────────────────

func TestMiddleware_HealthIsOpen(t *testing.T) {
	// The StatefulSet readiness probe hits /health with no token. Locking it
	// down would leave every agent pod permanently NotReady.
	ctrl := newAuthController(t, unreachableVerifier(t))

	var reached bool
	h := ctrl.Middleware()(okHandler(&reached))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", http.NoBody))

	if !reached {
		t.Fatal("/health did not reach the handler; readiness probes would fail")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestMiddleware_SessionRoutesRejectUnauthenticated(t *testing.T) {
	ctrl := newAuthController(t, unreachableVerifier(t))

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/session"},
		{http.MethodPost, "/session/run-1/messages"},
		{http.MethodPost, "/session/run-1/tool-results"},
		{http.MethodPost, "/session/run-1/interrupt"},
		{http.MethodDelete, "/session/run-1"},
	}

	for _, tc := range paths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var reached bool
			h := ctrl.Middleware()(okHandler(&reached))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, http.NoBody))

			if reached {
				t.Fatal("unauthenticated request reached the handler")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestMiddleware_InternalRoutesAreLoopbackOnly(t *testing.T) {
	ctrl := newAuthController(t, unreachableVerifier(t))

	tests := []struct {
		name       string
		remoteAddr string
		wantReach  bool
		wantStatus int
	}{
		// orbit-runtime-mcp and orbit-shell call http://localhost:8090 from
		// inside this container; the hop never leaves the pod.
		{"ipv4 loopback", "127.0.0.1:41234", true, http.StatusOK},
		{"ipv6 loopback", "[::1]:41234", true, http.StatusOK},
		// Istio sources inbound passthrough from 127.0.0.6 — loopback by
		// address, but it means the request came from off-pod through Envoy.
		{"istio inbound passthrough", "127.0.0.6:41234", false, http.StatusForbidden},
		{"another pod", "10.42.1.9:41234", false, http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			h := ctrl.Middleware()(okHandler(&reached))

			req := httptest.NewRequest(http.MethodPost, "/internal/session/run-1/provision-sandbox", http.NoBody)
			req.RemoteAddr = tc.remoteAddr
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if reached != tc.wantReach {
				t.Fatalf("reached = %v, want %v", reached, tc.wantReach)
			}
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// ── caller authorization ──────────────────────────────────────────────────────

func TestRequireExpectedCaller(t *testing.T) {
	tests := []struct {
		name       string
		claims     *token.PlatformClaims
		wantReach  bool
		wantStatus int
	}{
		{
			name: "orbit-agents system token is accepted",
			claims: &token.PlatformClaims{Kora: token.KoraClaims{
				PrincipalType: token.PrincipalTypeSystem,
				DisplayName:   "orbit-agents",
			}},
			wantReach:  true,
			wantStatus: http.StatusOK,
		},
		{
			// The whole point of the check: every in-mesh component can mint a
			// SYSTEM token, and scope enforcement treats SYSTEM as unrestricted.
			name: "another system component is rejected",
			claims: &token.PlatformClaims{Kora: token.KoraClaims{
				PrincipalType: token.PrincipalTypeSystem,
				DisplayName:   "orbit-triggers",
			}},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "an agent's own token is rejected",
			claims: &token.PlatformClaims{Kora: token.KoraClaims{
				PrincipalType: token.PrincipalTypeAgent,
				DisplayName:   "orbit-agents",
			}},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "a human token is rejected",
			claims: &token.PlatformClaims{Kora: token.KoraClaims{
				PrincipalType: token.PrincipalTypeHuman,
				DisplayName:   "dev@orbit.local",
			}},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing claims are rejected",
			claims:     nil,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := newAuthController(t, nil)

			var reached bool
			h := ctrl.requireExpectedCaller(okHandler(&reached))

			req := httptest.NewRequest(http.MethodPost, "/session", http.NoBody)
			if tc.claims != nil {
				req = req.WithContext(token.WithClaims(req.Context(), tc.claims))
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if reached != tc.wantReach {
				t.Fatalf("reached = %v, want %v (status %d)", reached, tc.wantReach, rec.Code)
			}
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// ── loopback classification ───────────────────────────────────────────────────

func TestIsLoopbackRequest(t *testing.T) {
	tests := []struct {
		remoteAddr string
		want       bool
	}{
		{"127.0.0.1:1234", true},
		{"[::1]:1234", true},
		{"[::ffff:127.0.0.1]:1234", true},
		{"127.0.0.6:1234", false},
		{"127.0.0.2:1234", false},
		{"10.42.0.7:1234", false},
		{"", false},
		{"not-an-address", false},

		// Port-less forms exercise the SplitHostPort fallback. net/http always
		// sets a host:port RemoteAddr for TCP, so these do not arise in
		// practice, but the fallback exists and its behaviour is pinned here
		// rather than assumed.
		{"127.0.0.1", true},
		{"::1", true},
		{"[::1]", true},

		// Unparseable input must deny. The fallback hands the whole string to
		// ParseAddr, and anything it rejects has to fail closed.
		{"invalid::", false},
		{"[::1]:notaport:extra", false},
	}

	for _, tc := range tests {
		t.Run(tc.remoteAddr, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/session/r/provision-sandbox", http.NoBody)
			req.RemoteAddr = tc.remoteAddr
			if got := isLoopbackRequest(req); got != tc.want {
				t.Errorf("isLoopbackRequest(%q) = %v, want %v", tc.remoteAddr, got, tc.want)
			}
		})
	}
}
