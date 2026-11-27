package bundle

import (
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/policy"
)

func TestScopeIndexKey(t *testing.T) {
	cases := map[string]string{
		"agents:types:read":           "agents|read",
		"agents:read":                 "agents|read",
		"mcp:github:create_issue:use": "mcp|use",
		"egress:connect":              "egress|connect",
		"admin":                       "admin|admin",
	}
	for scope, want := range cases {
		if got := scopeIndexKey(scope); got != want {
			t.Errorf("scopeIndexKey(%q) = %q, want %q", scope, got, want)
		}
	}
}

// TestRoleScopeIndex_HoldsEveryCoveringScope checks that a decision reading only the
// index group of its required scope finds every role scope that covers it.
func TestRoleScopeIndex_HoldsEveryCoveringScope(t *testing.T) {
	scopes := []string{
		"agents:read", "agents:types:read", "agents:types:write", "agents:runs:coder:read",
		"mcp:invoke", "mcp:kora:use", "mcp:github:create_issue:use", "inference:use",
		"inference:anthropic:use", "egress:connect", "admin", "platform:roles:read",
	}
	required := append(slices.Clone(scopes), "agents:types:coder:read", "mcp:github:list:use",
		"inference:anthropic:claude:use", "admin:read", "platform:read", "x")
	doc := newRolesDocument(map[string][]string{"r": scopes})
	for _, req := range required {
		group := doc.RoleScopeIndex["r"][scopeIndexKey(req)]
		for _, sc := range scopes {
			if policy.ScopeCovers(sc, req) && !slices.Contains(group, sc) {
				t.Errorf("%q covers %q but is not in index group %q", sc, req, scopeIndexKey(req))
			}
		}
	}
	total := 0
	for _, g := range doc.RoleScopeIndex["r"] {
		total += len(g)
	}
	if total != len(scopes) {
		t.Errorf("index holds %d scopes, want %d", total, len(scopes))
	}
}
