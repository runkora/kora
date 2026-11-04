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

// TestStore_RoutesForTenantKeepsTablesApart: the rego gives global routes
// precedence, so the tenant bundle must carry them separately.
func TestStore_RoutesForTenantKeepsTablesApart(t *testing.T) {
	s := NewStore()
	s.Register(&Registration{Service: "global", Routes: []RouteScope{{PathPrefix: "/g", Method: "GET"}}})
	s.Register(&Registration{Service: "svc", TenantID: "t1", Routes: []RouteScope{{PathPrefix: "/t1", Method: "GET"}}})
	s.Register(&Registration{Service: "svc", TenantID: "t2", Routes: []RouteScope{{PathPrefix: "/t2", Method: "GET"}}})

	tests := []struct {
		tenant         string
		wantGlobal     string
		wantTenantLen  int
		wantTenantPath string
	}{
		{"t1", "/g", 1, "/t1"},
		{"none", "/g", 0, ""},
	}
	for _, tc := range tests {
		global, tenant := s.RoutesForTenant(tc.tenant)
		if len(global) != 1 || global[0].PathPrefix != tc.wantGlobal {
			t.Errorf("%s: global = %+v", tc.tenant, global)
		}
		if tenant == nil || len(tenant) != tc.wantTenantLen || (tc.wantTenantLen == 1 && tenant[0].PathPrefix != tc.wantTenantPath) {
			t.Errorf("%s: tenant = %+v", tc.tenant, tenant)
		}
	}
}
