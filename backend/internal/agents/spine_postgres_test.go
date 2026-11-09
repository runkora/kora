package agents

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// recorded returns a tenant's events in the order they were written, from the
// outbox and from the log a running relay may already have moved them to.
func recorded(t *testing.T, r *PostgresRepository, tenant string) []events.Envelope {
	t.Helper()
	rows, err := r.pool.Query(context.Background(), `
		SELECT envelope FROM (
		    SELECT envelope FROM event_outbox WHERE tenant_id = $1
		    UNION ALL SELECT envelope FROM events WHERE tenant_id = $1) e
		ORDER BY envelope->>'id'`, tenant)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	raw, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		t.Fatal(err)
	}
	out := make([]events.Envelope, len(raw))
	for i, b := range raw {
		if err := json.Unmarshal(b, &out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func eventTypes(envs []events.Envelope) []events.Type {
	out := make([]events.Type, len(envs))
	for i, e := range envs {
		out[i] = e.Type
	}
	return out
}

// runEvents keeps the agents.run events. Seeding an agent records agents.agent.created.v1.
func runEvents(envs []events.Envelope) []events.Envelope {
	return slices.DeleteFunc(envs, func(e events.Envelope) bool { return e.Type.Category() != "agents.run" })
}

func TestPostgres_TerminateRun(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	live := queueRun(t, r, tenant, at.ID, time.Now(), nil)
	ended := queueRun(t, r, tenant, at.ID, time.Now(), nil)
	if err := r.SetRunState(ctx, ended, RunStatePending, RunStateCompleted); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AppendRunEvent(ctx, RunEvent{RunID: live, EventType: "tool_use", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]RunState{live: RunStateTerminated, ended: RunStateCompleted} {
		run, err := r.TerminateRun(ctx, id)
		if err != nil {
			t.Fatalf("TerminateRun: %v", err)
		}
		if run.State != want || run.DeletedAt == nil {
			t.Errorf("state %s, deleted_at %v, want %s and set", run.State, run.DeletedAt, want)
		}
		if _, err := r.TerminateRun(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("second terminate = %v, want ErrNotFound", err)
		}
	}
	if evs, _ := r.ListRunEvents(ctx, live, 0); len(evs) != 1 {
		t.Errorf("terminated run keeps %d run_events, want 1", len(evs))
	}
	if got, _ := r.GetRun(ctx, live); got == nil || got.State != RunStateTerminated {
		t.Errorf("detail read = %+v, want the terminated run", got)
	}
	scope := RunScope{AgentTypeIDs: []string{at.ID}}
	if runs, _ := r.ListRuns(ctx, tenant, RunListQuery{Scope: scope, Limit: 10}); len(runs) != 0 {
		t.Errorf("run list holds %d terminated runs", len(runs))
	}
	stats, err := r.RunStats(ctx, tenant, scope, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 0 || stats.Completed != 0 || stats.Current.Total != 0 {
		t.Errorf("stats count terminated runs: %+v", stats)
	}

	var terminated []events.Envelope
	for _, e := range recorded(t, r, tenant) {
		if e.Type == events.RunTerminated {
			terminated = append(terminated, e)
		}
	}
	if len(terminated) != 2 {
		t.Fatalf("terminated events = %d, want 2", len(terminated))
	}
	for _, e := range terminated {
		if e.Agent != at.Slug || e.Subject != "runs/"+e.Run {
			t.Errorf("terminated event = %+v", e)
		}
	}
}

func TestPostgres_RunEventCommitsWithItsStateChange(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	id := queueRun(t, r, tenant, at.ID, time.Now(), nil)

	rollback := errors.New("roll back")
	err := storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		if err := r.SetRunState(ctx, id, RunStatePending, RunStateFailed); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if got := eventTypes(runEvents(recorded(t, r, tenant))); !slices.Equal(got, []events.Type{events.RunCreated}) {
		t.Fatalf("after rollback events = %v, want only created", got)
	}
	if run, _ := r.GetRun(ctx, id); run.State != RunStatePending {
		t.Fatalf("rolled-back state = %s", run.State)
	}

	if err := r.SetRunState(ctx, id, RunStatePending, RunStateFailed); err != nil {
		t.Fatal(err)
	}
	if got := eventTypes(runEvents(recorded(t, r, tenant))); !slices.Equal(got, []events.Type{events.RunCreated, events.RunFailed}) {
		t.Errorf("events = %v, want created and failed", got)
	}
}

func TestPostgres_BulkRunUpdatesRecordAnEventPerRun(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	idle := queueRun(t, r, tenant, at.ID, time.Now(), nil)
	pending := queueRun(t, r, tenant, at.ID, time.Now(), nil)
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET state = 'idle' WHERE id = $1`, idle); err != nil {
		t.Fatal(err)
	}
	cancelled, err := r.CancelRunsByAgentType(ctx, at.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cancelled) != 2 {
		t.Fatalf("cancelled %d runs, want 2", len(cancelled))
	}

	stale := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	reapedID := queueRun(t, r, tenant, stale.ID, time.Now(), nil)
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET updated_at = now() - interval '1 hour' WHERE id = $1`, reapedID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReapStaleRuns(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}

	want := map[string]events.Type{idle: events.RunCompleted, pending: events.RunFailed, reapedID: events.RunFailed}
	agent := map[string]string{idle: at.Slug, pending: at.Slug, reapedID: stale.Slug}
	for _, e := range runEvents(recorded(t, r, tenant)) {
		if e.Type == events.RunCreated {
			continue
		}
		if want[e.Run] != e.Type || e.Agent != agent[e.Run] || e.Tenant != tenant {
			t.Errorf("event %s for run %s agent %s, want %s for agent %s", e.Type, e.Run, e.Agent, want[e.Run], agent[e.Run])
		}
		delete(want, e.Run)
	}
	if len(want) != 0 {
		t.Errorf("no event for %v", want)
	}
}

func TestPostgres_TriggerMessageExtendsTheChain(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	first, deeper := uuid.NewString(), uuid.NewString()
	run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID, StartedBy: &StartedByTrigger{TriggerID: "trg-a"}, CausedBy: first, ChainDepth: 1}
	if err := r.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	for _, m := range []RunMessage{
		{RunID: run.ID, Content: "more", From: &StartedByTrigger{TriggerID: "trg-b"}, CausedBy: deeper, ChainDepth: 3},
		{RunID: run.ID, Content: "again", From: &StartedByTrigger{TriggerID: "trg-a"}, CausedBy: uuid.NewString(), ChainDepth: 2},
		{RunID: run.ID, Content: "from a person"},
	} {
		if err := r.AppendRunMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := r.GetRun(ctx, run.ID)
	if got.CausedBy != deeper || got.ChainDepth != 3 || !slices.Equal(got.TriggerIDs, []string{"trg-a", "trg-b"}) {
		t.Errorf("chain = %s %d %v, want %s 3 [trg-a trg-b]", got.CausedBy, got.ChainDepth, got.TriggerIDs, deeper)
	}
	envs := runEvents(recorded(t, r, tenant))
	if got := eventTypes(envs); !slices.Equal(got, []events.Type{events.RunCreated, events.RunMessageReceived, events.RunMessageReceived, events.RunMessageReceived}) {
		t.Fatalf("events = %v", got)
	}
	created, last := envs[0], envs[len(envs)-1]
	if created.CausedBy != first || created.ChainDepth != 1 || !slices.Equal(created.Triggers, []string{"trg-a"}) {
		t.Errorf("created chain = %s %d %v", created.CausedBy, created.ChainDepth, created.Triggers)
	}
	if last.CausedBy != deeper || last.ChainDepth != 3 || len(last.Triggers) != 2 {
		t.Errorf("last message chain = %s %d %v", last.CausedBy, last.ChainDepth, last.Triggers)
	}
}

// An ended or terminated run cannot move back to a live state.
func TestPostgres_EndedRunKeepsItsState(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	ended := queueRun(t, r, tenant, at.ID, time.Now(), nil)
	if err := r.SetRunState(ctx, ended, RunStatePending, RunStateCompleted); err != nil {
		t.Fatal(err)
	}
	if err := r.SetRunState(ctx, ended, RunStateCompleted, RunStateActive); !errors.Is(err, ErrConflict) {
		t.Errorf("completed to active = %v, want ErrConflict", err)
	}
	live := queueRun(t, r, tenant, at.ID, time.Now(), nil)
	if _, err := r.TerminateRun(ctx, live); err != nil {
		t.Fatal(err)
	}
	if err := r.SetRunState(ctx, live, RunStateTerminated, RunStateActive); !errors.Is(err, ErrConflict) {
		t.Errorf("terminated to active = %v, want ErrConflict", err)
	}
	if err := r.MarkRunFailed(ctx, live); err != nil {
		t.Fatal(err)
	}
	if run, _ := r.GetRun(ctx, live); run.State != RunStateTerminated {
		t.Errorf("terminated run state = %s after MarkRunFailed", run.State)
	}
}

func TestPostgres_AgentAndEnvironmentChangesRecordEvents(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	env := testEnvironment(t, r, tenant, "go")
	env.Description = "the go toolchain"
	if err := r.UpdateEnvironment(ctx, env); err != nil {
		t.Fatal(err)
	}
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	at.Name = "Reviewer v2"
	at.Environments = []AgentEnvironment{{EnvironmentID: env.ID, IsDefault: true}}
	if err := r.UpdateAgentType(ctx, at); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateAgentType(ctx, at); err != nil {
		t.Fatal(err)
	}
	if err := r.SoftDeleteAgentType(ctx, at.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteEnvironment(ctx, tenant, env.ID); err != nil {
		t.Fatal(err)
	}

	envs := recorded(t, r, tenant)
	want := []events.Type{events.EnvironmentCreated, events.EnvironmentUpdated, events.AgentCreated, events.AgentUpdated, events.AgentDeleted, events.EnvironmentDeleted}
	if got := eventTypes(envs); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	changed := map[events.Type][]string{}
	for _, e := range envs {
		changed[e.Type] = e.Data.Changed
	}
	if !slices.Equal(changed[events.EnvironmentUpdated], []string{"description"}) {
		t.Errorf("environment changed = %v", changed[events.EnvironmentUpdated])
	}
	for _, e := range envs {
		if entity, _ := e.Data.Entity.(map[string]any); entity["system_prompt"] != nil || entity["connectors"] != nil {
			t.Errorf("%s entity carries configuration: %v", e.Type, entity)
		}
	}
	if !slices.Equal(changed[events.AgentUpdated], []string{"environment_ids", "name"}) {
		t.Errorf("agent changed = %v", changed[events.AgentUpdated])
	}
}
