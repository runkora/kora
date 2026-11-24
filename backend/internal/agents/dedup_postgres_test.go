package agents

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/runkora/kora/backend/cmd/kora-agents/migrations"
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

	// A released delivery (failed start) frees the key for a new run.
	if err := r.ReleaseRunDelivery(ctx, first.ID); err != nil {
		t.Fatalf("ReleaseRunDelivery: %v", err)
	}
	if ins, err := r.CreateRun(ctx, dedupRun(tenant, at.ID, "trig-1", "deliv-1")); err != nil || !ins {
		t.Errorf("after release = %v, %v; want inserted", ins, err)
	}

	// Different trigger, same delivery: both insert.
	if ins, err := r.CreateRun(ctx, dedupRun(tenant, at.ID, "trig-2", "deliv-1")); err != nil || !ins {
		t.Errorf("other trigger = %v, %v; want inserted", ins, err)
	}
	// No delivery id: both insert.
	for i := range 2 {
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

// The migration must apply on a database that already holds duplicate runs for one delivery:
// the oldest run keeps the delivery id, the others lose it, and no run is deleted.
func TestPostgres_RunsUniqueDeliveryMigrationCleansDuplicates(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)

	// Return to the pre-migration shape so duplicates can exist.
	if _, err := r.pool.Exec(ctx, `ALTER TABLE runs DROP CONSTRAINT runs_trigger_delivery_key`); err != nil {
		t.Fatalf("drop constraint: %v", err)
	}
	t.Cleanup(func() {
		// Leave the schema as the migration defines it, whatever the test did.
		_, _ = r.pool.Exec(ctx, `ALTER TABLE runs ADD CONSTRAINT runs_trigger_delivery_key UNIQUE (tenant_id, trigger_id, delivery_id)`)
	})

	oldest, newer := uuid.NewString(), uuid.NewString()
	insert := func(id string, age string) {
		t.Helper()
		if _, err := r.pool.Exec(ctx, `
			INSERT INTO runs (id, tenant_id, agent_type_id, state, pod_ordinal, created_by, created_at, updated_at,
				trigger_id, trigger_name, trigger_kind, delivery_id)
			VALUES ($1,$2,$3,'pending',-1,'trigger:trig-1', now() - $4::interval, now(), 'trig-1','T','webhook','deliv-1')`,
			id, tenant, at.ID, age); err != nil {
			t.Fatalf("seed run: %v", err)
		}
	}
	insert(newer, "1 minute")
	insert(oldest, "1 hour")

	body, err := fs.ReadFile(migrations.FS, "20261117021108_runs_unique_delivery.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	up, _, _ := strings.Cut(string(body), "-- +goose Down")
	if _, err := r.pool.Exec(ctx, up); err != nil {
		t.Fatalf("apply migration over duplicates: %v", err)
	}

	var kept, total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE delivery_id='deliv-1'), count(*) FROM runs WHERE tenant_id=$1 AND trigger_id='trig-1'`,
		tenant).Scan(&kept, &total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if kept != 1 || total != 2 {
		t.Fatalf("runs keeping delivery id = %d of %d, want 1 of 2", kept, total)
	}
	got, err := r.GetRunByDelivery(ctx, tenant, "trig-1", "deliv-1")
	if err != nil || got.ID != oldest {
		t.Fatalf("GetRunByDelivery = %v, %v; want oldest %s", got, err, oldest)
	}
}
