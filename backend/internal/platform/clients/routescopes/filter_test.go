package routescopes

import "testing"

// TestFilterScopedRoutes verifies that only routes carrying a required scope
// are kept: an empty-scope route registered with the policy service compiles to
// an empty required scope in the OPA bundle, which denies everyone.
func TestFilterScopedRoutes(t *testing.T) {
	in := []RouteScope{
		{PathPrefix: "/api/v1/agent-types", Method: "POST", RequiredScope: ""},
		{PathPrefix: "/api/v1/agent-types", Method: "GET", RequiredScope: "agents:types:read"},
		{PathPrefix: "/api/v1/runs", Method: "POST", RequiredScope: ""},
		{PathPrefix: "/api/v1/runs", Method: "GET", RequiredScope: "agents:runs:read"},
	}

	out := FilterScopedRoutes(in)

	if len(out) != 2 {
		t.Fatalf("expected 2 scoped routes, got %d: %+v", len(out), out)
	}
	for _, r := range out {
		if r.RequiredScope == "" {
			t.Errorf("empty-scope route survived the filter: %+v", r)
		}
	}
	if out[0].RequiredScope != "agents:types:read" || out[1].RequiredScope != "agents:runs:read" {
		t.Errorf("filter did not preserve input order: %+v", out)
	}
}

// TestFilterScopedRoutesEmptyInput verifies the filter returns an empty,
// non-nil slice rather than nil, so a caller can hand it straight to Register.
func TestFilterScopedRoutesEmptyInput(t *testing.T) {
	out := FilterScopedRoutes(nil)
	if out == nil {
		t.Fatal("expected a non-nil empty slice")
	}
	if len(out) != 0 {
		t.Fatalf("expected no routes, got %d", len(out))
	}
}
