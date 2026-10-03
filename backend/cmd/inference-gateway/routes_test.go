package main

import (
	"testing"

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
		{name: "agent type list is tenant-wide", path: "/v1/usage/agent-types", scope: "agents:types:read"},
		{name: "agent type detail follows agent visibility", path: "/v1/usage/agent-type/token-probe", scope: "agents:types:read"},
		{name: "agent insights follow agent visibility", path: "/v1/usage/agent-insights", scope: "agents:types:read"},
		{name: "agent budgets follow agent visibility", path: "/v1/usage/agent-budgets", scope: "agents:types:read"},
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
