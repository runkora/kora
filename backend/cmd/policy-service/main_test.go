package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestSelfRegisteredRoutesDeclareTheirScopes guards the two halves of the
// policy service's own registration against drifting apart: a route whose
// required scope is not in the published vocabulary compiles into the bundle
// with nothing able to satisfy it.
func TestSelfRegisteredRoutesDeclareTheirScopes(t *testing.T) {
	declared := make(map[string]struct{})
	for _, s := range selfRegisteredScopes() {
		declared[s.Scope] = struct{}{}
	}

	routeScopes := selfRegisteredRoutes()
	if len(routeScopes) == 0 {
		t.Fatal("the policy service registers no routes for itself")
	}
	for _, r := range routeScopes {
		if r.RequiredScope == "" {
			t.Errorf("route %s %s has no required scope", r.Method, r.PathPrefix)
			continue
		}
		if _, ok := declared[r.RequiredScope]; !ok {
			t.Errorf("route %s %s requires undeclared scope %q", r.Method, r.PathPrefix, r.RequiredScope)
		}
	}
}

// TestPolicyServiceRoutesCoverTheRegisteredTable keeps the locally enforced
// route table (ScopeEnforcementMiddleware) a superset of the table registered
// with the route store, with identical scopes for the shared entries. The two
// enforcement layers disagreeing is how a route ends up guarded in one place
// and open in the other.
func TestPolicyServiceRoutesCoverTheRegisteredTable(t *testing.T) {
	local := make(map[string]string)
	for _, r := range policyServiceRoutes() {
		local[r.Method+" "+r.PathPrefix] = r.RequiredScope
	}

	for _, r := range selfRegisteredRoutes() {
		key := r.Method + " " + r.PathPrefix
		got, ok := local[key]
		if !ok {
			t.Errorf("registered route %q is absent from the local enforcement table", key)
			continue
		}
		if got != r.RequiredScope {
			t.Errorf("route %q: registered scope %q, locally enforced scope %q", key, r.RequiredScope, got)
		}
	}
}

func TestNotTimerPoll_LeavesTimerPollsUntraced(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	chain := mw.MiddlewareChain(mw.TracingMiddleware(notTimerPoll("/api/v1")))
	handler := chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	tests := []struct {
		method, path string
		wantSpans    int
	}{
		{http.MethodGet, "/api/v1/policy/bundles/tenant-1", 0},
		{http.MethodGet, "/api/v1/policy/bundles/gateway", 0},
		{http.MethodGet, "/api/v1/policy/grants", 1},
		{http.MethodPost, "/api/v1/policy/bundles/tenant-1", 1},
		{http.MethodPost, "/api/v1/policy/routes/register", 0},
		{http.MethodGet, "/api/v1/policy/routes", 1},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			before := len(recorder.Ended())
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tt.method, tt.path, nil))
			if got := len(recorder.Ended()) - before; got != tt.wantSpans {
				t.Errorf("spans = %d, want %d", got, tt.wantSpans)
			}
		})
	}
}
