package routes

import "testing"

// TestStore_RegisterReportsChange covers the signal the policy service uses to
// decide whether a registration needs the bundles recompiled. Services
// re-register on a timer, and an unchanged catalog must not look like a change.
func TestStore_RegisterReportsChange(t *testing.T) {
	reg := func() *Registration {
		return &Registration{
			Service: "inference-gateway",
			Routes:  []RouteScope{{PathPrefix: "/v1/chat", Method: "POST", RequiredScope: "inference:invoke"}},
			Scopes: []ScopeDeclaration{{
				Scope:        "inference:invoke",
				ResourceType: "inference",
				Action:       "invoke",
				PathSegments: []string{},
			}},
		}
	}

	s := NewStore()

	if !s.Register(reg()) {
		t.Error("first registration reported no change")
	}
	if s.Register(reg()) {
		t.Error("identical re-registration reported a change")
	}

	withRoute := reg()
	withRoute.Routes = append(withRoute.Routes, RouteScope{PathPrefix: "/v1/models", Method: "GET", RequiredScope: ""})
	if !s.Register(withRoute) {
		t.Error("added route reported no change")
	}

	withScope := withRoute
	withScope.Scopes = []ScopeDeclaration{{
		Scope:        "inference:invoke",
		ResourceType: "inference",
		Action:       "invoke",
		PathSegments: []string{"anthropic"},
	}}
	if !s.Register(withScope) {
		t.Error("changed scope declaration reported no change")
	}

	other := reg()
	other.TenantID = "tenant-1"
	if !s.Register(other) {
		t.Error("first registration for a second tenant reported no change")
	}
}
