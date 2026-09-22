package agents

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration test for the run→trigger backlink. Runs only when
// ORBIT_TEST_DB_URL points at a database with the orbit-agents migrations
// applied; otherwise it skips. Mirrors drafts_postgres_test.go.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/agents/ -run Postgres_RunTrigger
func testRunRepo(t *testing.T) (*PostgresRepository, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run run-attribution Postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	tenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM runs WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_types WHERE tenant_id=$1`, tenant)
		pool.Close()
	})
	return NewPostgresRepository(pool), tenant
}

func seedRunAgentType(t *testing.T, r *PostgresRepository, tenant string) AgentType {
	t.Helper()
	at := AgentType{
		ID:                      uuid.NewString(),
		TenantID:                tenant,
		Name:                    "Reviewer",
		Slug:                    "reviewer-" + uuid.NewString()[:8],
		State:                   AgentTypeStateActive,
		MaxConcurrentRunsPerPod: 5,
		ToolScopes:              []string{},
		GrantScopes:             []string{},
		Connectors:              []Connector{},
		MCPTools:                []string{},
	}
	if err := r.CreateAgentType(context.Background(), at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	return at
}

// The attribution round-trips through the real columns, and a run nobody
// attributed reads back with none — the absence is what "a person started this"
// means everywhere downstream.
func TestPostgres_RunTriggerAttribution(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant)

	started := Run{
		ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID, CreatedBy: "trigger:trig-1",
		StartedBy: &StartedByTrigger{
			TriggerID:   "trig-1",
			TriggerName: "Nightly review",
			DeliveryID:  "deliv-1",
		},
	}
	if err := r.CreateRun(ctx, started); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	got, err := r.GetRun(ctx, started.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.StartedBy == nil {
		t.Fatal("started_by lost in the round trip")
	}
	if *got.StartedBy != *started.StartedBy {
		t.Errorf("started_by = %+v, want %+v", got.StartedBy, started.StartedBy)
	}

	human := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID, CreatedBy: "user-1"}
	if err := r.CreateRun(ctx, human); err != nil {
		t.Fatalf("CreateRun(human): %v", err)
	}
	gotHuman, err := r.GetRun(ctx, human.ID)
	if err != nil {
		t.Fatalf("GetRun(human): %v", err)
	}
	if gotHuman.StartedBy != nil {
		t.Errorf("started_by = %+v, want nil for a human-started run", gotHuman.StartedBy)
	}

	// The list path shares selectRun, so the attribution has to survive it too:
	// the runs list is where a person notices a run they did not start.
	runs, _, err := r.ListRuns(ctx, tenant, RunListFilter{
		AgentTypeID:          at.ID,
		ReadableAgentTypeIDs: []string{at.ID},
		Visibility:           RunVisibility{SeeAll: true},
	})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	var found bool
	for _, run := range runs {
		if run.ID == started.ID {
			found = true
			if run.StartedBy == nil || run.StartedBy.TriggerName != "Nightly review" {
				t.Errorf("listed run started_by = %+v, want the attribution", run.StartedBy)
			}
		}
	}
	if !found {
		t.Error("the trigger-started run is missing from the listing")
	}
}

// The name is a snapshot, not a reference: nothing joins to a triggers table
// that lives in another service's schema, so a run started by a trigger that no
// longer exists still reads as something a person recognizes.
func TestPostgres_RunTriggerAttribution_NoForeignKey(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant)

	run := Run{
		ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID, CreatedBy: "trigger:ghost",
		StartedBy: &StartedByTrigger{TriggerID: "no-such-trigger", TriggerName: "Deleted trigger"},
	}
	if err := r.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun with an unknown trigger id must be accepted: %v", err)
	}
	got, err := r.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.StartedBy == nil || got.StartedBy.TriggerName != "Deleted trigger" {
		t.Errorf("started_by = %+v, want the snapshot name", got.StartedBy)
	}
	if got.StartedBy.DeliveryID != "" {
		t.Errorf("delivery_id = %q, want it empty when none was recorded", got.StartedBy.DeliveryID)
	}
}
