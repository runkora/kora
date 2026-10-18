package policy

import (
	"context"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// grantedEvaluator allows a Check when a granted scope covers the derived scope.
type grantedEvaluator struct {
	Evaluator
	granted []string
}

func (e grantedEvaluator) Check(_ context.Context, in CheckInput) (Result, error) {
	required := DeriveRequiredScope(in.ResourceType, in.PathSegments, in.Action)
	return Result{Allow: slices.ContainsFunc(e.granted, func(g string) bool { return ScopeCovers(g, required) })}, nil
}

func TestAgentRunAllowed(t *testing.T) {
	tests := []struct {
		name      string
		granted   []string
		delegated bool
		action    string
		want      bool
	}{
		{"per-agent write", []string{"agents:runs:coder:write"}, false, "write", true},
		{"parent write", []string{"agents:runs:write"}, false, "write", true},
		{"other agent", []string{"agents:runs:other:write"}, true, "write", false},
		{"delegated write on a delegated agent", []string{"agents:runs:delegated:write"}, true, "write", true},
		{"delegated write on an autonomous agent", []string{"agents:runs:delegated:write"}, false, "write", false},
		{"delegated write does not read", []string{"agents:runs:delegated:write"}, true, "read", false},
		{"per-agent read", []string{"agents:runs:coder:read"}, false, "read", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := token.WithClaims(context.Background(), &token.PlatformClaims{Kora: token.KoraClaims{TenantID: "t1"}})
			got, err := AgentRunAllowed(ctx, grantedEvaluator{granted: tt.granted}, "coder", tt.delegated, tt.action)
			if err != nil || got != tt.want {
				t.Errorf("AgentRunAllowed = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestAgentRunAllowed_NoClaimsDenies(t *testing.T) {
	got, err := AgentRunAllowed(context.Background(), grantedEvaluator{granted: []string{"agents:runs:write"}}, "coder", true, "write")
	if err != nil || got {
		t.Errorf("AgentRunAllowed without claims = %v, %v; want false", got, err)
	}
}
