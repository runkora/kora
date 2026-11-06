package agents

import (
	"context"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// runsByID is a runGetter over fixed runs.
type runsByID map[string]Run

func (m runsByID) GetRun(_ context.Context, id string) (*Run, error) {
	if r, ok := m[id]; ok {
		return &r, nil
	}
	return nil, ErrNotFound
}

// sandboxAgentCtx is an AGENT caller whose per-run sandbox identity attests runID.
func sandboxAgentCtx(tenantID, slug, runID string) context.Context {
	return token.WithClaims(context.Background(), &token.PlatformClaims{
		Kora: token.KoraClaims{
			PrincipalType: token.PrincipalTypeAgent,
			TenantID:      tenantID,
			DisplayName:   slug,
			Claims: map[string]string{
				"spiffe_path": "/tenant/kora-tenant-" + tenantID + "/agent/" + slug + "/sandbox/" + runID,
				"run_id":      runID,
			},
		},
	})
}

func TestInheritedChain(t *testing.T) {
	runs := runsByID{
		"parent": {ID: "parent", TenantID: "t-1", AgentSlug: "triage", CausedBy: "ev-1", ChainDepth: 2, TriggerIDs: []string{"trg-a"}},
	}
	inherited := runChain{causedBy: "ev-1", depth: 3, triggers: []string{"trg-a"}}
	tests := []struct {
		name   string
		ctx    context.Context
		target string
		want   runChain
	}{
		{name: "sandbox run", ctx: sandboxAgentCtx("t-1", "triage", "parent"), want: inherited},
		{name: "delegated run", ctx: policy.WithDelegation(withAgentCtx(context.Background(), "t-1", "triage"), &policy.DelegationClaims{RunID: "parent"}), want: inherited},
		{name: "shared pod agent", ctx: withAgentCtx(context.Background(), "t-1", "triage")},
		{name: "another agent's run", ctx: sandboxAgentCtx("t-1", "coder", "parent")},
		{name: "another tenant's run", ctx: sandboxAgentCtx("t-2", "triage", "parent")},
		{name: "unknown run", ctx: sandboxAgentCtx("t-1", "triage", "gone")},
		{name: "a message to its own run", ctx: sandboxAgentCtx("t-1", "triage", "parent"), target: "parent"},
		{name: "a person", ctx: withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inheritedChain(tt.ctx, runs, tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if got.causedBy != tt.want.causedBy || got.depth != tt.want.depth || !slices.Equal(got.triggers, tt.want.triggers) {
				t.Errorf("inheritedChain = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A run an agent starts from an attested run carries that run's chain one step deeper.
func TestStartRun_InheritsTheCallingRunsChain(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	repo.runs["parent"] = &Run{ID: "parent", TenantID: "t1", AgentTypeID: at.ID, AgentSlug: "reviewer", CausedBy: "ev-1", ChainDepth: 1, TriggerIDs: []string{"trg-a"}}

	run, err := f.run.StartRun(sandboxAgentCtx("t1", "reviewer", "parent"), StartRunRequest{AgentTypeID: at.ID})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	stored, _ := repo.GetRun(context.Background(), run.ID)
	if stored.CausedBy != "ev-1" || stored.ChainDepth != 2 || !slices.Equal(stored.TriggerIDs, []string{"trg-a"}) {
		t.Errorf("chain = %s %d %v, want ev-1 2 [trg-a]", stored.CausedBy, stored.ChainDepth, stored.TriggerIDs)
	}
}
