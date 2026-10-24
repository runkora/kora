package agents

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// A soft-deleted agent leaves every live lookup, keeps its row and runs for run reads, and frees its slug and environments.
func TestPostgres_SoftDeleteAgentType(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	env := testEnvironment(t, r, tenant, "go")
	at := testAgentType(tenant, AgentEnvironment{EnvironmentID: env.ID, IsDefault: true})
	if err := r.CreateAgentType(ctx, at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	now := time.Now().UTC()
	runs := map[RunState]string{}
	for _, state := range []RunState{RunStatePending, RunStateActive, RunStateIdle, RunStateCompleting, RunStateCompleted} {
		runs[state] = seedRunAt(t, r, tenant, at.ID, "alice", state, now, nil)
	}

	cancelled, err := r.CancelRunsByAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("CancelRunsByAgentType: %v", err)
	}
	if len(cancelled) != 4 {
		t.Errorf("cancelled %d runs, want the 4 unfinished ones", len(cancelled))
	}
	want := map[RunState]RunState{
		RunStatePending: RunStateFailed, RunStateActive: RunStateFailed,
		RunStateIdle: RunStateCompleted, RunStateCompleting: RunStateCompleted, RunStateCompleted: RunStateCompleted,
	}
	for from, id := range runs {
		run, err := r.GetRun(ctx, id)
		if err != nil {
			t.Fatalf("GetRun %s: %v", from, err)
		}
		if run.State != want[from] {
			t.Errorf("%s run is %s, want %s", from, run.State, want[from])
		}
		if from != RunStateCompleted && run.CompletedAt == nil {
			t.Errorf("%s run has no completed_at", from)
		}
	}

	if err := r.SoftDeleteAgentType(ctx, at.ID); err != nil {
		t.Fatalf("SoftDeleteAgentType: %v", err)
	}
	if err := r.SoftDeleteAgentType(ctx, at.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second SoftDeleteAgentType = %v, want ErrNotFound", err)
	}

	if _, err := r.GetAgentType(ctx, at.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAgentType = %v, want ErrNotFound", err)
	}
	if _, err := r.GetAgentTypeBySlug(ctx, tenant, at.Slug); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAgentTypeBySlug = %v, want ErrNotFound", err)
	}
	if list, _ := r.ListAgentTypes(ctx, tenant); len(list) != 0 {
		t.Errorf("ListAgentTypes = %d agents, want none", len(list))
	}
	if list, _ := r.ListAgentTypesByEnvironment(ctx, tenant, env.ID); len(list) != 0 {
		t.Errorf("ListAgentTypesByEnvironment = %d agents, want none", len(list))
	}
	if slugs, _ := r.ListAllAgentTypeSlugs(ctx); slices.Contains(slugs, at.Slug) {
		t.Errorf("ListAllAgentTypeSlugs still lists %q", at.Slug)
	}
	stale := at
	stale.Name = "edited"
	if err := r.UpdateAgentType(ctx, stale); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateAgentType = %v, want ErrNotFound", err)
	}

	got, err := r.GetAgentTypeWithDeleted(ctx, at.ID)
	if err != nil {
		t.Fatalf("GetAgentTypeWithDeleted: %v", err)
	}
	if got.State != AgentTypeStateDeleted || len(got.Environments) != 0 {
		t.Errorf("deleted agent = state %s, environments %v; want deleted with none", got.State, got.Environments)
	}
	if run, err := r.GetRun(ctx, runs[RunStateCompleted]); err != nil || run.AgentSlug != at.Slug || run.AgentName != at.Name {
		t.Errorf("GetRun = %+v, %v; want the deleted agent's slug and name on the run", run, err)
	}
	if list, _ := r.ListAgentTypesWithDeleted(ctx, tenant); len(list) != 1 {
		t.Errorf("ListAgentTypesWithDeleted = %d agents, want the deleted one", len(list))
	}
	listed, err := r.ListRuns(ctx, tenant, RunListQuery{Scope: RunScope{AgentTypeIDs: []string{at.ID}, Vis: RunVisibility{SeeAll: true}}, Limit: 100})
	if err != nil || len(listed) != len(runs) {
		t.Errorf("ListRuns = %d runs, %v; want all %d runs of the deleted agent", len(listed), err, len(runs))
	}

	if err := r.DeleteEnvironment(ctx, tenant, env.ID); err != nil {
		t.Errorf("DeleteEnvironment = %v, want the deleted agent to have released it", err)
	}

	reused := testAgentType(tenant)
	reused.Slug = at.Slug
	if err := r.CreateAgentType(ctx, reused); err != nil {
		t.Fatalf("CreateAgentType with a deleted agent's slug: %v", err)
	}
	live, err := r.GetAgentTypeBySlug(ctx, tenant, at.Slug)
	if err != nil || live.ID != reused.ID {
		t.Errorf("GetAgentTypeBySlug = %v, %v; want the new agent %s", live, err, reused.ID)
	}
	dup := testAgentType(tenant)
	dup.Slug = at.Slug
	if err := r.CreateAgentType(ctx, dup); !errors.Is(err, ErrSlugTaken) {
		t.Errorf("second live agent with the slug = %v, want ErrSlugTaken", err)
	}
}

// A run created while its agent is deleted is still claimed. The scheduler then fails it at once.
func TestPostgres_ClaimPendingRunsClaimsDeletedAgentsRuns(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	past := time.Now().UTC().Add(-time.Hour)
	id := queueRun(t, r, tenant, at.ID, past, &past)
	if err := r.SoftDeleteAgentType(context.Background(), at.ID); err != nil {
		t.Fatalf("SoftDeleteAgentType: %v", err)
	}
	if got := claimOwn(t, r, tenant, time.Now()); !slices.Equal(runIDs(got), []string{id}) {
		t.Errorf("claimed %v, want the deleted agent's run %s", runIDs(got), id)
	}
}
