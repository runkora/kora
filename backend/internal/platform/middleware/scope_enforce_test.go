package middleware

import (
	"reflect"
	"testing"

	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/policy"
)

func TestSplitScope_Roundtrips(t *testing.T) {
	cases := []struct {
		in           string
		wantResource string
		wantPath     []string
		wantAction   string
	}{
		{"agents:types:write", "agents", []string{"types"}, "write"},
		{"agents:types", "agents", nil, "types"},
		{"agents:runs:coder:read", "agents", []string{"runs", "coder"}, "read"},
		{"inference:invoke", "inference", nil, "invoke"},
		{"", "", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			gotResource, gotPath, gotAction := splitScope(c.in)
			if gotResource != c.wantResource {
				t.Errorf("resource: got %q, want %q", gotResource, c.wantResource)
			}
			if !reflect.DeepEqual(gotPath, c.wantPath) {
				t.Errorf("path: got %v, want %v", gotPath, c.wantPath)
			}
			if gotAction != c.wantAction {
				t.Errorf("action: got %q, want %q", gotAction, c.wantAction)
			}
			// Round-trip: rebuilding via DeriveRequiredScope must yield the input.
			if c.in == "" {
				return // empty case: DeriveRequiredScope("","",[]) = ":" — not a valid scope; skip
			}
			rebuilt := policy.DeriveRequiredScope(gotResource, gotPath, gotAction)
			if rebuilt != c.in {
				t.Errorf("roundtrip: DeriveRequiredScope(%q, %v, %q) = %q, want %q",
					gotResource, gotPath, gotAction, rebuilt, c.in)
			}
		})
	}
}

func TestResolveRequiredScope_ExactMethodBeatsWildcard(t *testing.T) {
	routes := []routescopes.RouteScope{
		{PathPrefix: "/api/v1/agent-types", Method: "*", RequiredScope: "agents:types:read"},
		{PathPrefix: "/api/v1/agent-types", Method: "POST", RequiredScope: "agents:types:write"},
	}
	got, ok := resolveRequiredScope(routes, "POST", "/api/v1/agent-types")
	if !ok || got != "agents:types:write" {
		t.Errorf("got (%q, %v), want (agents:types:write, true)", got, ok)
	}
}

func TestResolveRequiredScope_WildcardFallback(t *testing.T) {
	routes := []routescopes.RouteScope{
		{PathPrefix: "/api/v1/agent-types", Method: "*", RequiredScope: "agents:types:read"},
		{PathPrefix: "/api/v1/agent-types", Method: "POST", RequiredScope: "agents:types:write"},
	}
	got, ok := resolveRequiredScope(routes, "GET", "/api/v1/agent-types")
	if !ok || got != "agents:types:read" {
		t.Errorf("got (%q, %v), want (agents:types:read, true)", got, ok)
	}
}

func TestResolveRequiredScope_NoMatchDenies(t *testing.T) {
	routes := []routescopes.RouteScope{
		{PathPrefix: "/api/v1/agent-types", Method: "POST", RequiredScope: "agents:types:write"},
	}
	_, ok := resolveRequiredScope(routes, "GET", "/api/v1/unknown")
	if ok {
		t.Errorf("expected no match for /api/v1/unknown, got match")
	}
}

func TestResolveRequiredScope_PrefixMatch(t *testing.T) {
	routes := []routescopes.RouteScope{
		{PathPrefix: "/api/v1/agent-types/", Method: "PATCH", RequiredScope: "agents:types:write"},
	}
	got, ok := resolveRequiredScope(routes, "PATCH", "/api/v1/agent-types/abc-123")
	if !ok || got != "agents:types:write" {
		t.Errorf("got (%q, %v), want (agents:types:write, true)", got, ok)
	}
}

// A sibling path that merely shares a string prefix (no segment boundary) must not
// inherit a route's scope — "/api/v1/runs" must not match "/api/v1/runs-export".
func TestResolveRequiredScope_PrefixIsSegmentAnchored(t *testing.T) {
	routes := []routescopes.RouteScope{
		{PathPrefix: "/api/v1/runs", Method: "GET", RequiredScope: "agents:runs:read"},
	}
	if got, ok := resolveRequiredScope(routes, "GET", "/api/v1/runs-export"); ok {
		t.Errorf("got (%q, %v) for sibling path, want no match", got, ok)
	}
	// The exact path and a sub-resource still match.
	if got, ok := resolveRequiredScope(routes, "GET", "/api/v1/runs"); !ok || got != "agents:runs:read" {
		t.Errorf("exact path: got (%q, %v), want (agents:runs:read, true)", got, ok)
	}
	if got, ok := resolveRequiredScope(routes, "GET", "/api/v1/runs/abc/events"); !ok || got != "agents:runs:read" {
		t.Errorf("sub-resource: got (%q, %v), want (agents:runs:read, true)", got, ok)
	}
}

// The most specific (longest) matching prefix wins, independent of table order.
func TestResolveRequiredScope_LongestMatchOrderIndependent(t *testing.T) {
	general := routescopes.RouteScope{PathPrefix: "/api/v1/analytics", Method: "GET", RequiredScope: "platform:analytics:read"}
	specific := routescopes.RouteScope{PathPrefix: "/api/v1/analytics/me", Method: "GET", RequiredScope: ""}
	for _, routes := range [][]routescopes.RouteScope{
		{general, specific},
		{specific, general},
	} {
		got, ok := resolveRequiredScope(routes, "GET", "/api/v1/analytics/me")
		if !ok || got != "" {
			t.Errorf("got (%q, %v), want (\"\", true) — longest prefix should win", got, ok)
		}
	}
}
