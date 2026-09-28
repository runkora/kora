package agents

import (
	"context"
	"testing"
)

// TestPostgres_SubagentPolicyRoundTrip checks the subagent policy columns: the
// defaults a type is created with, and that an update both sets and clears them.
func TestPostgres_SubagentPolicyRoundTrip(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()

	at := seedRunAgentType(t, r, tenant)
	got, err := r.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if got.MaxConcurrentSubagents != nil || got.NestedSubagents {
		t.Errorf("seeded type = cap %v nested %v, want nil and the zero value it was written with", got.MaxConcurrentSubagents, got.NestedSubagents)
	}

	two := 2
	got.MaxConcurrentSubagents, got.NestedSubagents = &two, true
	if err := r.UpdateAgentType(ctx, *got); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	got, err = r.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if got.MaxConcurrentSubagents == nil || *got.MaxConcurrentSubagents != 2 || !got.NestedSubagents {
		t.Fatalf("after update = cap %v nested %v, want 2 and true", got.MaxConcurrentSubagents, got.NestedSubagents)
	}

	got.MaxConcurrentSubagents = nil
	if err := r.UpdateAgentType(ctx, *got); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	got, _ = r.GetAgentType(ctx, at.ID)
	if got.MaxConcurrentSubagents != nil {
		t.Errorf("cap after clearing = %v, want nil", *got.MaxConcurrentSubagents)
	}

	// The CHECK constraint rejects a cap below 1 written around the service.
	zero := 0
	got.MaxConcurrentSubagents = &zero
	if err := r.UpdateAgentType(ctx, *got); err == nil {
		t.Error("expected the database to reject max_concurrent_subagents = 0")
	}
}
