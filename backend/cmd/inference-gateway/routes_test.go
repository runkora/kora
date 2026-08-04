package main

import (
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
)

// resolveScope mirrors middleware.resolveRequiredScope (and the `_required_scope`
// rule in bundle/authz.rego): segment-bounded prefix match, exact method beats
// wildcard, longest matching prefix wins. It is reproduced here because the
// middleware helper is unexported; the behaviour it models is documented on
// ScopeEnforcementMiddleware.
func resolveScope(t *testing.T, routes []routescopes.RouteScope, method, path string) string {
	t.Helper()

	best, bestLen := "", -1
	for _, r := range routes {
		if !strings.HasPrefix(path, r.PathPrefix) {
			continue
		}
		if len(path) != len(r.PathPrefix) && !strings.HasSuffix(r.PathPrefix, "/") && path[len(r.PathPrefix)] != '/' {
			continue
		}
		if r.Method == method && len(r.PathPrefix) > bestLen {
			best, bestLen = r.RequiredScope, len(r.PathPrefix)
		}
	}
	if bestLen < 0 {
		t.Fatalf("no route matched %s %s", method, path)
	}
	return best
}

// TestInferenceRoutesUsageScopes pins the personal-vs-privileged split across the
// /v1/usage tree. The tenant-wide agent-type reads aggregate spend for every
// agent type in the tenant, so they require platform:analytics:read; the rest
// are derived from the caller's own claims and need authentication only.
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
		{name: "agent type list is tenant-wide", path: "/v1/usage/agent-types", scope: "platform:analytics:read"},
		{name: "agent type detail is tenant-wide", path: "/v1/usage/agent-type/type-abc", scope: "platform:analytics:read"},
		{name: "personal analytics", path: "/v1/analytics/me", scope: ""},
		{name: "tenant analytics", path: "/v1/analytics", scope: "platform:analytics:read"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveScope(t, routes, "GET", tt.path); got != tt.scope {
				t.Errorf("GET %s: required scope = %q, want %q", tt.path, got, tt.scope)
			}
		})
	}
}
