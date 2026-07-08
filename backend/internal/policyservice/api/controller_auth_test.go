package api

import (
	"strings"
	"testing"
)

// TestUncoveredScopes covers the grant-administration anti-escalation decision: a
// requested scope is allowed only when the caller holds a scope that covers it
// (exact, or a parent that prefixes it). See the 2026-06-30 amendment to the
// authorisation model.
func TestUncoveredScopes(t *testing.T) {
	tests := []struct {
		name      string
		held      []string
		requested []string
		want      []string // expected rejected scopes
	}{
		{
			name:      "exact match is covered",
			held:      []string{"inference:invoke", "agents:types:write"},
			requested: []string{"inference:invoke"},
			want:      nil,
		},
		{
			name:      "parent scope covers a more specific child",
			held:      []string{"inference:anthropic:use"},
			requested: []string{"inference:anthropic:claude-sonnet-4-6:use"},
			want:      nil,
		},
		{
			name:      "scope the caller does not hold is rejected",
			held:      []string{"inference:invoke"},
			requested: []string{"platform:grants:write"},
			want:      []string{"platform:grants:write"},
		},
		{
			name:      "a child does NOT cover its parent (no upward escalation)",
			held:      []string{"inference:anthropic:claude-sonnet-4-6:use"},
			requested: []string{"inference:anthropic:use"},
			want:      []string{"inference:anthropic:use"},
		},
		{
			name:      "mixed: only the uncovered ones are rejected",
			held:      []string{"inference:invoke", "agents:types:write"},
			requested: []string{"inference:invoke", "platform:grants:write", "agents:types:write"},
			want:      []string{"platform:grants:write"},
		},
		{
			name:      "caller holding nothing rejects everything",
			held:      nil,
			requested: []string{"inference:invoke"},
			want:      []string{"inference:invoke"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uncoveredScopes(tt.held, tt.requested)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("uncoveredScopes(%v, %v) = %v, want %v", tt.held, tt.requested, got, tt.want)
			}
		})
	}
}
