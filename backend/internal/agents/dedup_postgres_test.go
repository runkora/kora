package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func dedupRun(tenant, agentID, trigger, delivery string) Run {
	r := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: agentID, CreatedBy: "trigger:" + trigger}
	if trigger != "" {
		r.StartedBy = &StartedByTrigger{TriggerID: trigger, TriggerName: "T", DeliveryID: delivery}
	}
	return r
}

func TestPostgres_StartRunDedupesOnDelivery(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)

	first := dedupRun(tenant, at.ID, "trig-1", "deliv-1")
	if ins, err := r.CreateRun(ctx, first); err != nil || !ins {
		t.Fatalf("first CreateRun = %v, %v; want inserted", ins, err)
	}
	if ins, err := r.CreateRun(ctx, dedupRun(tenant, at.ID, "trig-1", "deliv-1")); err != nil || ins {
		t.Fatalf("duplicate CreateRun = %v, %v; want not inserted", ins, err)
	}
	got, err := r.GetRunByDelivery(ctx, tenant, "trig-1", "deliv-1")
	if err != nil || got.ID != first.ID {
		t.Fatalf("GetRunByDelivery = %v, %v; want %s", got, err, first.ID)
	}

	// Different trigger, same delivery: both insert.
	if ins, err := r.CreateRun(ctx, dedupRun(tenant, at.ID, "trig-2", "deliv-1")); err != nil || !ins {
		t.Errorf("other trigger = %v, %v; want inserted", ins, err)
	}
	// No delivery id: both insert.
	for i := 0; i < 2; i++ {
		if ins, err := r.CreateRun(ctx, dedupRun(tenant, at.ID, "trig-1", "")); err != nil || !ins {
			t.Errorf("no delivery #%d = %v, %v; want inserted", i, ins, err)
		}
	}
	// Same trigger and delivery in another tenant: inserts.
	other := tenant + "-b"
	atB := seedRunAgentType(t, r, other, IdentityModeAutonomous)
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM runs WHERE tenant_id=$1`, other)
		_, _ = r.pool.Exec(ctx, `DELETE FROM agent_types WHERE tenant_id=$1`, other)
	})
	if ins, err := r.CreateRun(ctx, dedupRun(other, atB.ID, "trig-1", "deliv-1")); err != nil || !ins {
		t.Errorf("other tenant = %v, %v; want inserted", ins, err)
	}
}
