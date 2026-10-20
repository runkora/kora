package main

import (
	"context"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// TestInferenceRoutesUsageScopes pins the route scopes across the /v1/usage tree.
// The agent reads are authenticated-only and filter per agent in the service; the
// rest are derived from the caller's own claims.
//
// It resolves through mw.ResolveRequiredScope, the function the enforcement
// middleware itself calls, rather than a local reimplementation of the matching
// rule. Restating the rule here would let this test agree with itself while
// disagreeing with enforcement, and a route silently inheriting a broader
// prefix's scope is the bug this table exists to catch.
func TestInferenceRoutesUsageScopes(t *testing.T) {
	routes := inferenceRoutes()

	tests := []struct {
		name  string
		path  string
		scope string
	}{
		{name: "usage log is personal", path: "/v1/usage", scope: ""},
		{name: "usage stats is personal", path: "/v1/usage/stats", scope: ""},
		{name: "personal budget", path: "/v1/usage/budget", scope: ""},
		{name: "run usage meter", path: "/v1/usage/run/run-123", scope: ""},
		{name: "agent type usage filters in the service", path: "/v1/usage/agent-type/token-probe", scope: ""},
		{name: "agent insights filter in the service", path: "/v1/usage/agent-insights", scope: ""},
		{name: "personal analytics", path: "/v1/analytics/me", scope: ""},
		{name: "tenant analytics", path: "/v1/analytics", scope: "platform:analytics:read"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mw.ResolveRequiredScope(routes, "GET", tt.path)
			if !ok {
				t.Fatalf("no route matched GET %s", tt.path)
			}
			if got != tt.scope {
				t.Errorf("GET %s: required scope = %q, want %q", tt.path, got, tt.scope)
			}
		})
	}
}

// routeRecorder reads the registered routes from the mux's log line, as it has no listing.
type routeRecorder struct {
	logging.Logger
	mu     sync.Mutex
	routes []string
}

func (r *routeRecorder) Info(_ context.Context, msg string, fields ...logging.Field) {
	if msg != "Registering sub-route" {
		return
	}
	for _, f := range fields {
		if f.Key == "route" {
			r.mu.Lock()
			r.routes = append(r.routes, f.Value.(string))
			r.mu.Unlock()
		}
	}
}

// TestUsageRoutesAreScopedUnlessPersonal fails on a /v1/usage route that falls to the
// open entry, which would expose tenant-wide spend to any authenticated caller.
// The listed routes are authenticated-only and scope their own response.
func TestUsageRoutesAreScopedUnlessPersonal(t *testing.T) {
	rec := &routeRecorder{Logger: logging.NewLoggerWithOutput(io.Discard)}
	mux := api.NewMux(context.Background(), rec)
	mux.SetBasePath("/v1")
	inferencegw.NewController(context.Background(), nil, nil, inferenceRoutes(), "/v1", nil, rec).Boot(mux)

	personal := map[string]bool{
		"/v1/usage":             true,
		"/v1/usage/stats":       true,
		"/v1/usage/budget":      true,
		"/v1/usage/run/{runID}": true,
		// Filtered per agent by Service.ReadableAgents.
		"/v1/usage/agent-insights":    true,
		"/v1/usage/agent-type/{slug}": true,
	}
	seenPersonal := map[string]bool{}
	wildcard := regexp.MustCompile(`\{[^}]*\}`)
	usageRoutes := 0
	for _, route := range rec.routes {
		method, path, _ := strings.Cut(route, " ")
		if path != "/v1/usage" && !strings.HasPrefix(path, "/v1/usage/") {
			continue
		}
		usageRoutes++
		scope, ok := mw.ResolveRequiredScope(inferenceRoutes(), method, wildcard.ReplaceAllString(path, "x"))
		switch {
		case personal[path]:
			seenPersonal[path] = true
		case !ok || scope == "":
			t.Errorf("%s resolves to no scope: add a specific entry to inferenceRoutes", route)
		}
	}
	if usageRoutes == 0 {
		t.Fatal("no /v1/usage routes were registered")
	}
	for path := range personal {
		if !seenPersonal[path] {
			t.Errorf("personal route %s is not registered: update the list", path)
		}
	}
}

// TestSystemOneRouteScope pins POST /v1/systemone to inference:invoke, beside
// embeddings and responses, and checks the controller registers it.
func TestSystemOneRouteScope(t *testing.T) {
	got, ok := mw.ResolveRequiredScope(inferenceRoutes(), "POST", "/v1/systemone")
	if !ok || got != "inference:invoke" {
		t.Errorf("POST /v1/systemone: scope = %q (matched %v), want inference:invoke", got, ok)
	}
	if got, ok := mw.ResolveRequiredScope(inferenceRoutes(), "GET", "/v1/systemone"); ok && got != "" {
		t.Errorf("GET /v1/systemone must not inherit a scope, got %q", got)
	}

	rec := &routeRecorder{Logger: logging.NewLoggerWithOutput(io.Discard)}
	mux := api.NewMux(context.Background(), rec)
	mux.SetBasePath("/v1")
	inferencegw.NewController(context.Background(), nil, nil, inferenceRoutes(), "/v1", nil, rec).Boot(mux)
	if slices.Contains(rec.routes, "POST /v1/systemone") {
		return
	}
	t.Errorf("POST /v1/systemone is not registered; routes: %v", rec.routes)
}
