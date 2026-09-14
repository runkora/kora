package bundle

import (
	"slices"
	"testing"

	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

func entry(scope string) *vocabulary.ScopeEntry { return &vocabulary.ScopeEntry{Scope: scope} }

// TestAdminScopes pins the derivation: the admin holds every registered scope
// that no other registered scope covers, and nothing else.
func TestAdminScopes(t *testing.T) {
	t.Parallel()

	got := AdminScopes([]*vocabulary.ScopeEntry{
		entry("inference:use"),
		entry("inference:anthropic:use"),
		entry("inference:anthropic:claude-sonnet-4-6:use"),
		entry("inference:list"),
		entry("mcp:orbit:use"),
		entry("mcp:orbit:agents:use"),
		entry("mcp:orbit:agents:create:use"),
		entry("mcp:github:use"),
		entry("platform:grants:read"),
		entry("platform:grants:write"),
		entry("trigger:fire"),
		entry("triggers:read"),
	})
	want := []string{"inference:list", "inference:use", "mcp:github:use", "mcp:orbit:use", "platform:grants:read", "platform:grants:write", "trigger:fire", "triggers:read"}
	if !slices.Equal(got, want) {
		t.Fatalf("AdminScopes = %v, want %v", got, want)
	}
}
