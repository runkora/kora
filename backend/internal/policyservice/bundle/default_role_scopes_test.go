package bundle

import (
	"testing"

	"github.com/clear-route/orbit/internal/platform/policy"
)

// TestPlatformAdminCoversAllMCPServers locks in that the platform-admin role
// grants MCP access via the covering scope mcp:use, so any MCP server — orbit,
// github, or one added later — is usable without enumerating a per-server scope
// in DefaultRoleScopes. Guards against a regression back to mcp:orbit:use, which
// only covered the orbit server (why the github MCP was unusable).
func TestPlatformAdminCoversAllMCPServers(t *testing.T) {
	admin := DefaultRoleScopes["platform-admin"]

	covers := func(required string) bool {
		for _, g := range admin {
			if policy.ScopeCovers(g, required) {
				return true
			}
		}
		return false
	}

	for _, tool := range []string{
		"mcp:orbit:agents:create:use",
		"mcp:github:create_issue:use",
		"mcp:github:issues:create:use",
		"mcp:a-server-added-later:some_tool:use",
	} {
		if !covers(tool) {
			t.Errorf("platform-admin should cover %q via mcp:use, but does not", tool)
		}
	}

	// The MCP cover must not leak into unrelated resource types: a different
	// first segment (inference) is never covered by an mcp scope.
	if covers("inference:anthropic:claude-sonnet-4-6:use") {
		t.Error("platform-admin unexpectedly covers an inference model scope")
	}
}
