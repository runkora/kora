package sessionmanager

import (
	"fmt"
	"testing"
)

func TestSubagentPolicyEnv(t *testing.T) {
	tests := []struct {
		req  CreateSessionRequest
		want []string
	}{
		{CreateSessionRequest{}, nil},
		{CreateSessionRequest{MaxConcurrentSubagents: 1}, []string{"CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS=1"}},
		{CreateSessionRequest{MaxSubagentSpawnDepth: 1}, []string{"CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH=1"}},
		{CreateSessionRequest{MaxConcurrentSubagents: 3, MaxSubagentSpawnDepth: 1}, []string{"CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS=3", "CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH=1"}},
	}
	for _, tt := range tests {
		if got := subagentPolicyEnv(tt.req); fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("subagentPolicyEnv(%+v) = %v, want %v", tt.req, got, tt.want)
		}
	}
}
