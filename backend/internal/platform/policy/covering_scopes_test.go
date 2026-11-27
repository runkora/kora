package policy

import (
	"slices"
	"testing"
)

// TestCoveringScopes_MatchesScopeCovers checks that a scope's covering set is
// exactly the scopes ScopeCovers says cover it.
func TestCoveringScopes_MatchesScopeCovers(t *testing.T) {
	universe := []string{
		"agents:read", "agents:types:read", "agents:types:coder:read", "agents:types:write",
		"agents:runs:read", "mcp:use", "mcp:github:use", "mcp:github:create_issue:use",
		"mcp:invoke", "inference:anthropic:claude:use", "inference:anthropic:use", "inference:use",
		"egress:connect", "admin", "admin:read", "x:y:z:w:read", "x:read", "x:y:read", "x:y:z:read",
	}
	for _, b := range universe {
		got := CoveringScopes(b)
		if got[0] != b {
			t.Errorf("CoveringScopes(%q)[0] = %q, want the scope itself", b, got[0])
		}
		for _, a := range universe {
			if ScopeCovers(a, b) != slices.Contains(got, a) {
				t.Errorf("ScopeCovers(%q, %q) = %v, but CoveringScopes(%q) = %v", a, b, ScopeCovers(a, b), b, got)
			}
		}
		for _, a := range got {
			if !ScopeCovers(a, b) {
				t.Errorf("CoveringScopes(%q) holds %q, which does not cover it", b, a)
			}
		}
	}
}
