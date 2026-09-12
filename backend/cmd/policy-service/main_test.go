package main

import "testing"

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
