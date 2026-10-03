package main

import (
	"context"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"

	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// TestInferenceRoutesUsageScopes pins the personal-vs-privileged split across the
// /v1/usage tree. The agent-type reads report spend per agent, so they require
// agents:types:read; the rest are derived from the caller's own claims and need
// authentication only.
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
		{name: "agent type usage needs the agent scope", path: "/v1/usage/agent-type/token-probe", scope: "agents:types:read"},
		{name: "agent insights need the agent scope", path: "/v1/usage/agent-insights", scope: "agents:types:read"},
		{name: "tenant cap needs budget read", path: "/v1/budgets/tenant-cap", scope: "platform:budgets:read"},
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

// routeRecorder is a logger that keeps the routes the mux registers.
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

// TestUsageRoutesAreScopedUnlessPersonal walks the routes the gateway
// registers. A /v1/usage route with no entry of its own resolves to the open
// "/v1/usage" entry, and an empty scope passes any authenticated caller, so a
// new route that forgot its entry would expose tenant-wide agent spend. Only the
// routes derived from the caller's own claims may resolve to no scope.
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
