package main

import (
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// Fire has its own authenticated-only entry, so the triggers:write entry for creation never applies to it.
func TestRouteScopes_Fire(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{"POST", "/api/v1/trigger-fires/3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43", ""},
		{"POST", "/api/v1/triggers", "triggers:write"},
	}
	for _, tc := range tests {
		got, ok := mw.ResolveRequiredScope(routeScopes(), tc.method, tc.path)
		if !ok || got != tc.want {
			t.Errorf("%s %s resolved to %q (matched %v), want %q", tc.method, tc.path, got, ok, tc.want)
		}
	}
	// The published table is the one enforced in-process, so the edge sees fire as authenticated-only too.
	if !slices.Contains(routeScopes(), routescopes.RouteScope{PathPrefix: "/api/v1/trigger-fires", Method: "POST", RequiredScope: ""}) {
		t.Error("the fire route must be published as authenticated-only")
	}
}
