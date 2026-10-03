package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Postgres integration tests for the insights queries. They run only when
// ORBIT_TEST_DB_URL points at a database with the orbit-agents migrations
// applied (see testRunRepo).

func seedInsightsAgentType(t *testing.T, r *PostgresRepository, tenant string, mode IdentityMode) AgentType {
	t.Helper()
	at := AgentType{
		ID: uuid.NewString(), TenantID: tenant, Name: "A", Slug: "a-" + uuid.NewString()[:8],
		State: AgentTypeStateActive, MaxConcurrentRunsPerPod: 5, IdentityMode: mode,
		GrantScopes: []string{}, Connectors: []Connector{}, MCPTools: []string{},
	}
	if err := r.CreateAgentType(context.Background(), at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	return at
}

func seedTerminalRun(t *testing.T, r *PostgresRepository, tenant, agentTypeID, createdBy string, state RunState, age time.Duration) {
	t.Helper()
	ctx := context.Background()
	run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: agentTypeID, CreatedBy: createdBy}
	if err := r.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET state=$1, completed_at=$2 WHERE id=$3`, string(state), time.Now().Add(-age), run.ID); err != nil {
		t.Fatalf("set run state: %v", err)
	}
}

func TestPostgres_ListAgentTypeRefs(t *testing.T) {
	r, tenant := testRunRepo(t)
	auto := seedInsightsAgentType(t, r, tenant, IdentityModeAutonomous)
	del := seedInsightsAgentType(t, r, tenant, IdentityModeDelegated)

	refs, err := r.ListAgentTypeRefs(context.Background(), tenant)
	if err != nil {
		t.Fatalf("ListAgentTypeRefs: %v", err)
	}
	got := map[string]AgentTypeRef{}
	for _, ref := range refs {
		got[ref.ID] = ref
	}
	if len(got) != 2 || got[auto.ID].Delegated || !got[del.ID].Delegated || got[del.ID].Slug != del.Slug {
		t.Errorf("refs = %+v", refs)
	}

	other, err := r.ListAgentTypeRefs(context.Background(), tenant+"-other")
	if err != nil || len(other) != 0 {
		t.Errorf("another tenant's refs = %+v, %v; want none", other, err)
	}
}

func TestPostgres_CountRunOutcomes_Visibility(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	auto := seedInsightsAgentType(t, r, tenant, IdentityModeAutonomous)
	del := seedInsightsAgentType(t, r, tenant, IdentityModeDelegated)

	seedTerminalRun(t, r, tenant, auto.ID, "alice", RunStateCompleted, time.Hour)
	seedTerminalRun(t, r, tenant, auto.ID, "bob", RunStateFailed, time.Hour)
	seedTerminalRun(t, r, tenant, auto.ID, "bob", RunStateActive, time.Hour)               // not terminal
	seedTerminalRun(t, r, tenant, auto.ID, "bob", RunStateCompleted, 40*24*time.Hour)      // finished outside 7d
	seedTerminalRun(t, r, tenant, del.ID, "alice", RunStateCompleted, time.Hour)           // alice's own
	seedTerminalRun(t, r, tenant, del.ID, "bob", RunStateCompleted, time.Hour)             // bob's, private
	seedTerminalRun(t, r, tenant, del.ID, "bob", RunStateFailed, time.Hour)                // bob's, private
	seedTerminalRun(t, r, tenant+"-other", auto.ID, "alice", RunStateCompleted, time.Hour) // other tenant

	// Started long before the window but finished inside it: counted.
	long := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: auto.ID, CreatedBy: "alice"}
	if err := r.CreateRun(ctx, long); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET state='completed', created_at=$1, completed_at=$2 WHERE id=$3`,
		time.Now().Add(-40*24*time.Hour), time.Now().Add(-time.Hour), long.ID); err != nil {
		t.Fatalf("age run: %v", err)
	}

	since := time.Now().Add(-7 * 24 * time.Hour)

	alice, err := r.CountRunOutcomes(ctx, RunOutcomeScope{TenantID: tenant, Since: since, AllRows: []string{auto.ID}, OwnRows: []string{del.ID}, Subject: "alice"})
	if err != nil {
		t.Fatalf("CountRunOutcomes alice: %v", err)
	}
	if alice != (RunOutcomes{Completed: 3, Failed: 1}) {
		t.Errorf("alice = %+v, want 3 completed 1 failed (autonomous runs plus her own delegated run)", alice)
	}

	admin, err := r.CountRunOutcomes(ctx, RunOutcomeScope{TenantID: tenant, Since: since, AllRows: []string{auto.ID, del.ID}, Subject: "root"})
	if err != nil {
		t.Fatalf("CountRunOutcomes admin: %v", err)
	}
	if admin != (RunOutcomes{Completed: 4, Failed: 2}) {
		t.Errorf("admin = %+v, want 4 completed 2 failed", admin)
	}

	noSubject, err := r.CountRunOutcomes(ctx, RunOutcomeScope{TenantID: tenant, Since: since, OwnRows: []string{del.ID}})
	if err != nil {
		t.Fatalf("CountRunOutcomes empty subject: %v", err)
	}
	if noSubject != (RunOutcomes{}) {
		t.Errorf("empty subject = %+v, want nothing (fail closed)", noSubject)
	}

	nothing, err := r.CountRunOutcomes(ctx, RunOutcomeScope{TenantID: tenant, Since: since})
	if err != nil || nothing != (RunOutcomes{}) {
		t.Errorf("no agents = %+v, %v", nothing, err)
	}
}
