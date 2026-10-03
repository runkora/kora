package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Postgres integration tests for the run outcome count. They run only when
// ORBIT_TEST_DB_URL points at a database with the orbit-agents migrations
// applied (see testRunRepo).

func seedOutcomeAgent(t *testing.T, r *PostgresRepository, tenant string, mode IdentityMode) AgentType {
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

// seedFinishedRun writes a run that finished `finished` ago and started `started` ago.
func seedFinishedRun(t *testing.T, r *PostgresRepository, tenant, agentTypeID, createdBy string, state RunState, started, finished time.Duration) {
	t.Helper()
	ctx := context.Background()
	run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: agentTypeID, CreatedBy: createdBy}
	if err := r.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET state=$1, created_at=$2, completed_at=$3 WHERE id=$4`,
		string(state), time.Now().Add(-started), time.Now().Add(-finished), run.ID); err != nil {
		t.Fatalf("finish run: %v", err)
	}
}

func TestPostgres_CountRunOutcomes(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	auto := seedOutcomeAgent(t, r, tenant, IdentityModeAutonomous)
	del := seedOutcomeAgent(t, r, tenant, IdentityModeDelegated)
	// The other tenant's run references this tenant's agent type, so it goes first.
	t.Cleanup(func() { _, _ = r.pool.Exec(ctx, `DELETE FROM runs WHERE tenant_id=$1`, tenant+"-other") })

	h := time.Hour
	seedFinishedRun(t, r, tenant, auto.ID, "alice", RunStateCompleted, 2*h, h)
	seedFinishedRun(t, r, tenant, auto.ID, "bob", RunStateFailed, 2*h, h)
	seedFinishedRun(t, r, tenant, auto.ID, "bob", RunStateActive, 2*h, h)               // not finished
	seedFinishedRun(t, r, tenant, auto.ID, "bob", RunStateCompleted, 41*24*h, 40*24*h)  // finished before the window
	seedFinishedRun(t, r, tenant, auto.ID, "alice", RunStateCompleted, 40*24*h, h)      // started before, finished inside
	seedFinishedRun(t, r, tenant, del.ID, "alice", RunStateCompleted, 2*h, h)           // alice's own delegated run
	seedFinishedRun(t, r, tenant, del.ID, "bob", RunStateCompleted, 2*h, h)             // bob's, private
	seedFinishedRun(t, r, tenant, del.ID, "bob", RunStateFailed, 2*h, h)                // bob's, private
	seedFinishedRun(t, r, tenant+"-other", auto.ID, "alice", RunStateCompleted, 2*h, h) // another tenant

	from, to := time.Now().Add(-7*24*h), time.Now().Add(time.Minute)
	cases := []struct {
		name string
		vis  RunVisibility
		want RunOutcomes
	}{
		{"a person sees the autonomous runs and their own delegated run", RunVisibility{Subject: "alice"}, RunOutcomes{Completed: 3, Failed: 1}},
		{"an admin sees every run", RunVisibility{SeeAll: true}, RunOutcomes{Completed: 4, Failed: 2}},
		{"an empty subject sees no delegated run", RunVisibility{}, RunOutcomes{Completed: 2, Failed: 1}},
	}
	for _, tc := range cases {
		got, err := r.CountRunOutcomes(ctx, tenant, tc.vis, from, to)
		if err != nil || got != tc.want {
			t.Errorf("%s: %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
}
