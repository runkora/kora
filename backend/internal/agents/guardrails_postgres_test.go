package agents

import (
	"context"
	"testing"
)

// TestPostgres_GuardrailsRoundTrip checks the guardrails column: NULL for the
// defaults, the set thresholds stored and read back, and a reset to NULL.
func TestPostgres_GuardrailsRoundTrip(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()

	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	got, err := r.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if !got.Guardrails.isEmpty() {
		t.Fatalf("new type guardrails = %+v, want defaults", got.Guardrails)
	}

	got.Guardrails = Guardrails{RepeatedCallWarn: new(2), WaitRearmPause: new(30)}
	if err := r.UpdateAgentType(ctx, *got); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	got, err = r.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if res := got.Guardrails.Resolve(); res.RepeatedCallWarn != 2 || res.WaitRearmPause != 30 || got.Guardrails.RepeatedCallPause != nil {
		t.Errorf("after update = %+v (resolved %+v)", got.Guardrails, res)
	}

	got.Guardrails = Guardrails{}
	if err := r.UpdateAgentType(ctx, *got); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	got, _ = r.GetAgentType(ctx, at.ID)
	if !got.Guardrails.isEmpty() {
		t.Errorf("after reset = %+v, want defaults", got.Guardrails)
	}
}
