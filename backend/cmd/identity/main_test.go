package main

import (
	"testing"

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// TestIdentityRoutes_UserDirectory locks that listing users needs
// identity:users:read while one profile stays authenticated-only at the route.
func TestIdentityRoutes_UserDirectory(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/identity/users", "identity:users:read"},
		{"/api/v1/identity/users/user-1", ""},
		{"/api/v1/identity/users/names", ""},
		{"/api/v1/identity/preferences", ""},
	}
	for _, tc := range cases {
		got, ok := mw.ResolveRequiredScope(identityRoutes(), "GET", tc.path)
		if !ok || got != tc.want {
			t.Errorf("GET %s resolves to %q (matched %v), want %q", tc.path, got, ok, tc.want)
		}
	}
}

// TestIdentityRegistration_PublishesUsersRead locks that the scoped list route
// and its scope are what identity publishes to the policy service.
func TestIdentityRegistration_PublishesUsersRead(t *testing.T) {
	routes := publishedIdentityRoutes()
	if len(routes) != 1 || routes[0].PathPrefix != "/api/v1/identity/users" || routes[0].RequiredScope != "identity:users:read" {
		t.Errorf("registered routes = %+v, want only the users list route", routes)
	}
	scopes := identityScopes()
	if len(scopes) != 1 || scopes[0].Scope != routes[0].RequiredScope {
		t.Errorf("declared scopes = %+v, want identity:users:read", scopes)
	}
}
