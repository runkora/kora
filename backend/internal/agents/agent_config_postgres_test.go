package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// A run's agent_config digest round-trips through real SQL; gated on KORA_TEST_DB_URL (see testRunRepo).
func TestPostgres_SetRunAgentConfig(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	runID := uuid.NewString()
	if _, err := r.CreateRun(ctx, Run{ID: runID, TenantID: tenant, AgentTypeID: at.ID}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	run, err := r.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.AgentConfig != nil {
		t.Fatalf("a new run has agent_config %q, want none", *run.AgentConfig)
	}

	// A set digest is kept until it is cleared.
	for _, step := range []struct{ set, want string }{{"first", "first"}, {"second", "first"}, {"", ""}, {"second", "second"}} {
		if step.set == "" {
			err = r.ClearRunAgentConfig(ctx, run.ID)
		} else {
			err = r.SetRunAgentConfig(ctx, run.ID, step.set)
		}
		if err != nil {
			t.Fatalf("write agent_config %q: %v", step.set, err)
		}
		got, err := r.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if (step.want == "") != (got.AgentConfig == nil) || (got.AgentConfig != nil && *got.AgentConfig != step.want) {
			t.Errorf("after %q agent_config = %v, want %q", step.set, got.AgentConfig, step.want)
		}
	}
	listed, err := r.ListRuns(ctx, tenant, RunListQuery{Limit: 10, Scope: RunScope{AgentTypeIDs: []string{at.ID}}})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(listed) != 1 || listed[0].AgentConfig == nil || *listed[0].AgentConfig != "second" {
		t.Errorf("listed runs %+v, want one with agent_config second", listed)
	}
	if err := r.SetRunAgentConfig(ctx, uuid.NewString(), "x"); err != nil {
		t.Errorf("SetRunAgentConfig on a missing run: %v", err)
	}
}
