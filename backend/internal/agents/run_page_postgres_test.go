package agents

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Keyset paging and run stats against real SQL; gated on ORBIT_TEST_DB_URL (see testRunRepo).

// seedRunAt writes a run in state with the given created_at and started_at.
func seedRunAt(t *testing.T, r *PostgresRepository, tenant, agentTypeID, createdBy string, state RunState, createdAt time.Time, startedAt *time.Time) string {
	t.Helper()
	ctx := context.Background()
	run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: agentTypeID, CreatedBy: createdBy}
	if err := r.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET state=$1, created_at=$2, started_at=$3 WHERE id=$4`,
		string(state), createdAt, startedAt, run.ID); err != nil {
		t.Fatalf("set run times: %v", err)
	}
	return run.ID
}

// walkRepo pages through ListRuns the way the service does and returns the run ids in order.
func walkRepo(t *testing.T, r *PostgresRepository, tenant string, q RunListQuery) []string {
	t.Helper()
	var ids []string
	for range 100 {
		runs, err := r.ListRuns(context.Background(), tenant, q)
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		for _, run := range runs {
			ids = append(ids, run.ID)
		}
		if len(runs) < q.Limit {
			return ids
		}
		last := runs[len(runs)-1]
		q.After = &runCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	t.Fatal("pagination did not terminate")
	return nil
}

func TestPostgres_ListRunsKeyset(t *testing.T) {
	r, tenant := testRunRepo(t)
	auto := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	other := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	del := seedRunAgentType(t, r, tenant, IdentityModeDelegated)

	// 24 runs in groups of four sharing a created_at, so page edges fall inside ties.
	base := time.Now().UTC().Add(-time.Hour)
	want := map[string]bool{}
	for i := range 24 {
		at, by := auto.ID, "bob"
		switch i % 4 {
		case 1:
			at = other.ID
		case 2:
			at, by = del.ID, "alice"
		case 3:
			at, by = del.ID, "bob"
		}
		id := seedRunAt(t, r, tenant, at, by, RunStateCompleted, base.Add(-time.Duration(i/4)*time.Minute), nil)
		if at != other.ID && (at != del.ID || by == "alice") {
			want[id] = true
		}
	}

	scope := RunScope{AgentTypeIDs: []string{auto.ID, del.ID}, Vis: RunVisibility{Subject: "alice"}}
	ids := walkRepo(t, r, tenant, RunListQuery{Scope: scope, Limit: 5})
	if len(ids) != len(want) {
		t.Fatalf("paged %d runs, want %d", len(ids), len(want))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || !want[id] {
			t.Fatalf("run %s is a duplicate or outside the scope", id)
		}
		seen[id] = true
	}

	// One unpaged read gives the same order the pages did.
	all, err := r.ListRuns(context.Background(), tenant, RunListQuery{Scope: scope, Limit: 100})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	for i, run := range all {
		if ids[i] != run.ID {
			t.Fatalf("paged order differs from the single read at %d", i)
		}
	}

	if runs, err := r.ListRuns(context.Background(), tenant, RunListQuery{Scope: RunScope{Vis: RunVisibility{SeeAll: true}}, Limit: 100}); err != nil || len(runs) != 0 {
		t.Errorf("an empty scope gave %d runs, %v; want none", len(runs), err)
	}
}

// D3: Own adds the caller's runs of any agent type, AgentTypeID narrows both halves,
// and the delegated-run rule still applies to other people's delegated runs.
func TestPostgres_ListRunsOwnRuns(t *testing.T) {
	r, tenant := testRunRepo(t)
	readable := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	hidden := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	del := seedRunAgentType(t, r, tenant, IdentityModeDelegated)
	now := time.Now().UTC()
	readableBob := seedRunAt(t, r, tenant, readable.ID, "bob", RunStateCompleted, now.Add(-4*time.Minute), nil)
	hiddenAlice := seedRunAt(t, r, tenant, hidden.ID, "alice", RunStateCompleted, now.Add(-3*time.Minute), nil)
	seedRunAt(t, r, tenant, hidden.ID, "bob", RunStateCompleted, now.Add(-2*time.Minute), nil)
	seedRunAt(t, r, tenant, del.ID, "bob", RunStateCompleted, now.Add(-time.Minute), nil)

	vis := RunVisibility{Subject: "alice"}
	tests := []struct {
		name  string
		scope RunScope
		want  []string
	}{
		{"readable agents and own runs", RunScope{AgentTypeIDs: []string{readable.ID, del.ID}, Vis: vis, Own: true}, []string{hiddenAlice, readableBob}},
		{"readable agents only", RunScope{AgentTypeIDs: []string{readable.ID, del.ID}, Vis: vis}, []string{readableBob}},
		{"narrowed to an unreadable agent", RunScope{AgentTypeIDs: []string{readable.ID}, Vis: vis, Own: true, AgentTypeID: hidden.ID}, []string{hiddenAlice}},
		{"empty subject owns nothing", RunScope{AgentTypeIDs: []string{readable.ID}, Own: true}, []string{readableBob}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := walkRepo(t, r, tenant, RunListQuery{Scope: tt.scope, Limit: 10}); !slices.Equal(got, tt.want) {
				t.Errorf("runs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPostgres_ListRunsStateFilter(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	now := time.Now().UTC()
	pending := seedRunAt(t, r, tenant, at.ID, "bob", RunStatePending, now.Add(-time.Minute), nil)
	active := seedRunAt(t, r, tenant, at.ID, "bob", RunStateActive, now.Add(-2*time.Minute), nil)
	seedRunAt(t, r, tenant, at.ID, "bob", RunStateCompleted, now.Add(-3*time.Minute), nil)

	runs, err := r.ListRuns(context.Background(), tenant, RunListQuery{
		Scope:  RunScope{AgentTypeIDs: []string{at.ID}, Vis: RunVisibility{SeeAll: true}},
		States: liveRunStates,
		Limit:  10,
	})
	if err != nil || len(runs) != 2 || runs[0].ID != pending || runs[1].ID != active {
		t.Errorf("live runs = %+v, %v; want the pending then the active run", runs, err)
	}
}

func TestPostgres_RunStatsTenantScope(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	a := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	b := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	del := seedRunAgentType(t, r, tenant, IdentityModeDelegated)
	hidden := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)

	now := time.Now().UTC()
	h := time.Hour
	started := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	seedRunAt(t, r, tenant, a.ID, "bob", RunStateActive, now.Add(-2*h), started(90*time.Minute))
	seedRunAt(t, r, tenant, a.ID, "bob", RunStatePending, now.Add(-h), nil)
	seedRunAt(t, r, tenant, b.ID, "bob", RunStateStarting, now.Add(-h), started(time.Minute))
	seedRunAt(t, r, tenant, b.ID, "bob", RunStateIdle, now.Add(-h), started(30*time.Minute))
	seedRunAt(t, r, tenant, b.ID, "bob", RunStateCompleted, now.Add(-40*24*h), nil)
	seedRunAt(t, r, tenant, del.ID, "alice", RunStateCompleting, now.Add(-h), started(10*time.Minute))
	seedRunAt(t, r, tenant, del.ID, "bob", RunStateActive, now.Add(-h), started(5*time.Hour))
	seedRunAt(t, r, tenant, hidden.ID, "bob", RunStateActive, now.Add(-h), started(9*time.Hour))
	seedFinishedRun(t, r, tenant, a.ID, "bob", RunStateCompleted, 3*h, 2*h)
	seedFinishedRun(t, r, tenant, b.ID, "bob", RunStateFailed, 3*h, 2*h)

	scope := RunScope{AgentTypeIDs: []string{a.ID, b.ID, del.ID}, Vis: RunVisibility{Subject: "alice"}}
	got, err := r.RunStats(ctx, tenant, scope, now.Add(-7*24*h), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RunStats: %v", err)
	}

	c := got.Current
	checks := []struct {
		name      string
		got, want int64
	}{
		{"window total", got.Total, 7},
		{"completed in the window", got.Completed, 1},
		{"failed in the window", got.Failed, 1},
		{"current total", c.Total, 8},
		{"live", c.Live, 4},
		{"active", c.ByState[RunStateActive], 1},
		{"idle", c.ByState[RunStateIdle], 1},
		{"completed now", c.ByState[RunStateCompleted], 2},
		{"live on a", c.LiveByAgentType[a.ID], 2},
		{"live on b", c.LiveByAgentType[b.ID], 1},
		{"live on del", c.LiveByAgentType[del.ID], 1},
		{"live on hidden", c.LiveByAgentType[hidden.ID], 0},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %d, want %d", ch.name, ch.got, ch.want)
		}
	}
	// The longest live run in scope started 90 minutes ago; bob's delegated and the hidden agent's are out of scope.
	if d := time.Duration(c.LongestLiveMs) * time.Millisecond; d < 89*time.Minute || d > 91*time.Minute {
		t.Errorf("longest live = %v, want about 90m", d)
	}

	empty, err := r.RunStats(ctx, tenant, RunScope{Vis: RunVisibility{SeeAll: true}}, now.Add(-7*24*h), now.Add(time.Minute))
	if err != nil || empty.Total != 0 || empty.Current.Total != 0 {
		t.Errorf("an empty scope gave %+v, %v; want zeros", empty, err)
	}
}

func TestPostgres_AgentRunStatsSingleAgentScope(t *testing.T) {
	r, tenant := testRunRepo(t)
	a := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	b := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	h := time.Hour
	for i := range 3 {
		seedFinishedRun(t, r, tenant, a.ID, fmt.Sprint("u", i), RunStateCompleted, 3*h, 2*h)
	}
	seedFinishedRun(t, r, tenant, b.ID, "bob", RunStateCompleted, 3*h, 2*h)

	now := time.Now()
	got, err := r.RunStats(context.Background(), tenant, RunScope{AgentTypeIDs: []string{a.ID}, Vis: RunVisibility{SeeAll: true}}, now.Add(-24*h), now)
	if err != nil || got.Total != 3 || got.Completed != 3 || got.Current.Total != 3 || got.Durations.MedianMs != h.Milliseconds() {
		t.Errorf("stats = %+v, %v; want a's three one-hour runs only", got, err)
	}
}

func TestPostgres_ListRunsTimeBounds(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	scope := RunScope{AgentTypeIDs: []string{at.ID}, Vis: RunVisibility{SeeAll: true}}
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	var ids []string
	for i := range 6 {
		ids = append(ids, seedRunAt(t, r, tenant, at.ID, "bob", RunStateCompleted, base.Add(-time.Duration(i)*time.Minute), nil))
	}
	// ids[0] is newest. after is inclusive and before is exclusive.
	after, before := base.Add(-4*time.Minute), base.Add(-time.Minute)

	if got := walkRepo(t, r, tenant, RunListQuery{Scope: scope, CreatedAfter: after, Limit: 2}); !slices.Equal(got, ids[:5]) {
		t.Errorf("created_after = %v, want %v", got, ids[:5])
	}
	if got := walkRepo(t, r, tenant, RunListQuery{Scope: scope, CreatedBefore: before, Limit: 2}); !slices.Equal(got, ids[2:]) {
		t.Errorf("created_before = %v, want %v", got, ids[2:])
	}
	if got := walkRepo(t, r, tenant, RunListQuery{Scope: scope, CreatedAfter: after, CreatedBefore: before, Limit: 2}); !slices.Equal(got, ids[2:5]) {
		t.Errorf("window = %v, want %v", got, ids[2:5])
	}
}
