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

	// A later start attempt overwrites the digest of an earlier one.
	for _, digest := range []string{"first", "second"} {
		if err := r.SetRunAgentConfig(ctx, run.ID, digest); err != nil {
			t.Fatalf("SetRunAgentConfig: %v", err)
		}
		got, err := r.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if got.AgentConfig == nil || *got.AgentConfig != digest {
			t.Errorf("agent_config = %v, want %q", got.AgentConfig, digest)
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
