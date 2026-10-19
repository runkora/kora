package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func TestAgentTypeCallerAccess(t *testing.T) {
	tests := []struct {
		name      string
		granted   []string
		principal token.PrincipalType
		mode      IdentityMode
		wantRun   bool
		wantEdit  bool
	}{
		{"read only", nil, token.PrincipalTypeHuman, IdentityModeAutonomous, false, false},
		{"per-slug run write", []string{"agents:runs:bot-autonomous:write"}, token.PrincipalTypeHuman, IdentityModeAutonomous, true, false},
		{"other slug run write", []string{"agents:runs:other:write"}, token.PrincipalTypeHuman, IdentityModeAutonomous, false, false},
		{"parent run write", []string{"agents:runs:write"}, token.PrincipalTypeHuman, IdentityModeDelegated, true, false},
		{"delegated run write runs a delegated agent", []string{"agents:runs:delegated:write"}, token.PrincipalTypeHuman, IdentityModeDelegated, true, false},
		{"delegated run write does not run an autonomous agent", []string{"agents:runs:delegated:write"}, token.PrincipalTypeHuman, IdentityModeAutonomous, false, false},
		{"mode write edits that mode", []string{"agents:types:delegated:write"}, token.PrincipalTypeHuman, IdentityModeDelegated, false, true},
		{"mode write does not edit the other mode", []string{"agents:types:delegated:write"}, token.PrincipalTypeHuman, IdentityModeAutonomous, false, false},
		{"parent types write edits any mode", []string{"agents:types:write"}, token.PrincipalTypeHuman, IdentityModeAutonomous, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newModeTestService(t, append(tc.granted, "agents:types:read")...)
			at := seedModeAgentType(t, repo, tc.mode)
			ctx := withPrincipalCtx(context.Background(), "t-1", tc.principal, "user-1")

			list, err := svc.ListAgentTypes(ctx)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(list) != 1 {
				t.Fatalf("list returned %d agent types, want 1", len(list))
			}
			got, err := svc.GetAgentType(ctx, at.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			for _, a := range []AgentType{list[0], *got} {
				if a.CanRun == nil || a.CanEdit == nil || *a.CanRun != tc.wantRun || *a.CanEdit != tc.wantEdit {
					t.Errorf("can_run=%v can_edit=%v, want %v %v", a.CanRun, a.CanEdit, tc.wantRun, tc.wantEdit)
				}
			}
		})
	}
}

// erroringRunEvaluator grants agent-type reads and fails every other check.
type erroringRunEvaluator struct{ allowAllEvaluator }

func (erroringRunEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	if in.PathSegments[0] == "types" && in.Action == "read" {
		return policy.Result{Allow: true}, nil
	}
	return policy.Result{}, errors.New("bundle unavailable")
}

func TestAgentTypeCallerAccess_EvaluatorErrorLeavesFlagsFalse(t *testing.T) {
	svc, repo := newModeTestService(t)
	svc.authz = NewAuthz(erroringRunEvaluator{})
	at := seedModeAgentType(t, repo, IdentityModeAutonomous)
	ctx := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")

	got, err := svc.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("get should succeed when the access check fails: %v", err)
	}
	if got.CanRun == nil || got.CanEdit == nil || *got.CanRun || *got.CanEdit {
		t.Errorf("can_run=%v can_edit=%v, want both false", got.CanRun, got.CanEdit)
	}
}
