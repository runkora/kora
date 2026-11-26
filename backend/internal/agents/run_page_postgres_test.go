package agents

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Keyset paging and run stats against real SQL; gated on KORA_TEST_DB_URL (see testRunRepo).

// seedRunAt writes a run in state with the given created_at and started_at.
func seedRunAt(t *testing.T, r *PostgresRepository, tenant, agentTypeID, createdBy string, state RunState, createdAt time.Time, startedAt *time.Time) string {
	t.Helper()
	ctx := context.Background()
	run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: agentTypeID, CreatedBy: createdBy}
	if _, err := r.CreateRun(ctx, run); err != nil {
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
		{"narrowed to an unreadable agent", RunScope{AgentTypeIDs: []string{readable.ID}, Vis: vis, Own: true, OnlyAgentTypeIDs: []string{hidden.ID}}, []string{hiddenAlice}},
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

func TestPostgres_ListRunsStartedByTrigger(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	a := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	b := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	now := time.Now().UTC()
	triggered := func(at string, state RunState, age time.Duration) string {
		run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at, CreatedBy: "trigger:trig-1", StartedBy: &StartedByTrigger{TriggerID: "trig-1", TriggerName: "PR comments"}}
		if _, err := r.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		if _, err := r.pool.Exec(ctx, `UPDATE runs SET state=$1, created_at=$2 WHERE id=$3`, string(state), now.Add(-age), run.ID); err != nil {
			t.Fatalf("set run state: %v", err)
		}
		return run.ID
	}
	wantA := triggered(a.ID, RunStateIdle, time.Minute)
	wantB := triggered(b.ID, RunStateActive, 2*time.Minute)
	triggered(a.ID, RunStateCompleted, 3*time.Minute)
	seedRunAt(t, r, tenant, a.ID, "bob", RunStateActive, now.Add(-4*time.Minute), nil)

	runs, err := r.ListRuns(ctx, tenant, RunListQuery{
		Scope:            RunScope{AgentTypeIDs: []string{a.ID, b.ID}, Vis: RunVisibility{SeeAll: true}},
		States:           []RunState{RunStateActive, RunStateIdle},
		StartedByTrigger: true,
		Limit:            10,
	})
	if err != nil || len(runs) != 2 || runs[0].ID != wantA || runs[1].ID != wantB {
		t.Fatalf("trigger runs = %+v, %v; want the idle run of a then the active run of b", runs, err)
	}
	if runs[0].StartedBy == nil || runs[0].StartedBy.TriggerName != "PR comments" {
		t.Errorf("started_by = %+v, want the trigger attribution", runs[0].StartedBy)
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

// For each scope and filter combination, ListRuns returns exactly the seeded runs that readsRun and
// matchesRun accept, paged in twos across created_at ties. The run stream relies on the two agreeing.
func TestPostgres_RunMatchAgreesWithListRuns(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	// carol has a profile only in another tenant, which must not name her runs here.
	other := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM user_profiles WHERE tenant_id = ANY($1)`, []string{tenant, other})
	})
	if _, err := r.pool.Exec(ctx, `INSERT INTO user_profiles (id, tenant_id, email, display_name) VALUES ('carol', $1, 'carol@example.com', 'Carol Elsewhere')`, other); err != nil {
		t.Fatal(err)
	}
	newAgent := func(name string, mode IdentityMode) string {
		at := seedRunAgentType(t, r, tenant, mode)
		if _, err := r.pool.Exec(ctx, `UPDATE agent_types SET name=$1 WHERE id=$2`, name, at.ID); err != nil {
			t.Fatal(err)
		}
		return at.ID
	}
	deploy, review, del := newAgent("Deploy Bot", IdentityModeAutonomous), newAgent("Reviewer", IdentityModeAutonomous), newAgent("Helper", IdentityModeDelegated)
	names := map[string]string{"alice": "Alice Liddell", "bob": "Bob Builder"}
	for id, name := range names {
		if _, err := r.pool.Exec(ctx, `INSERT INTO user_profiles (id, tenant_id, email, display_name) VALUES ($1, $2, $1 || '@example.com', $3)`, id, tenant, name); err != nil {
			t.Fatal(err)
		}
	}

	// kind "legacy" is a trigger run from before trigger_kind was recorded.
	type spec struct {
		agent, by, kind, summary string
		state                    RunState
		terminated               bool
	}
	specs := []spec{
		{deploy, "alice", "", "Fix the login bug", RunStateActive, false},
		{review, "bob", "", "Review 100% coverage", RunStateIdle, false},
		{deploy, "trigger-source:s1", "webhook", "", RunStatePending, false},
		{review, "trigger:t2", "scheduled", "Nightly report", RunStateCompleted, false},
		{deploy, "trigger:t3", "event", "Ran 1000 checks", RunStateFailed, false},
		{review, "trigger:t4", "manual", "", RunStateActive, true},
		{del, "bob", "", "Secret review", RunStateActive, false},
		{del, "alice", "", "Alice's review", RunStateIdle, false},
		{deploy, "alice", "", "Deploy the docs", RunStateCompleted, true},
		{review, "trigger-source:s1", "webhook", "Review the PR", RunStateStarting, false},
		{deploy, "carol", "", "Carol's run", RunStateActive, false},
		{review, "trigger:t5", "legacy", "Review the backlog", RunStateIdle, false},
		{deploy, "", "", "Nobody's run", RunStatePending, false},
		{del, "trigger:t6", "webhook", "Delegated hook", RunStateActive, false},
		{del, "alice", "", "Alice's ended review", RunStateFailed, true},
	}
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	var ids []string
	for i, s := range specs {
		run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: s.agent, CreatedBy: s.by}
		switch s.kind {
		case "":
		case "legacy":
			run.StartedBy = &StartedByTrigger{TriggerID: "trig-legacy", TriggerName: "Hook legacy"}
		default:
			run.StartedBy = &StartedByTrigger{TriggerID: "trig-" + s.kind, TriggerName: "Hook " + s.kind, TriggerKind: s.kind}
		}
		if _, err := r.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		// Runs share a created_at in threes, so page edges fall inside ties.
		if _, err := r.pool.Exec(ctx, `UPDATE runs SET created_at=$1, summary=NULLIF($2, ''), state=$3,
			deleted_at=CASE WHEN $4 THEN now() END WHERE id=$5`,
			base.Add(-time.Duration(i/3)*time.Minute), s.summary, string(s.state), s.terminated, run.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, run.ID)
	}
	// The stream tests the rows it loads, so the predicate is checked against GetRunRows' rows.
	loaded, err := r.GetRunRows(ctx, tenant, ids)
	if err != nil || len(loaded) != len(ids) {
		t.Fatalf("GetRunRows: %d rows, %v", len(loaded), err)
	}
	rows := map[string]*Run{}
	for i := range loaded {
		rows[loaded[i].ID] = &loaded[i]
	}

	queries := []RunListQuery{
		{},
		{States: []RunState{RunStateActive}},
		{States: []RunState{RunStateIdle, RunStateCompleted, RunStateFailed}},
		{StartedByTrigger: true},
		{CreatedAfter: base.Add(-2 * time.Minute)},
		{CreatedBefore: base.Add(-2 * time.Minute)},
		{CreatedAfter: base.Add(-3 * time.Minute), CreatedBefore: base.Add(-time.Minute)},
		{CreatedByMe: true},
		{TriggerKinds: []string{"webhook"}},
		{TriggerKinds: []string{"webhook", "scheduled"}},
		{TriggerKinds: []string{triggerKindNone}},
		{TriggerKinds: []string{triggerKindNone, "event", "manual"}},
		{Search: "LOGIN"},
		{Search: "deploy"},
		{Search: "builder"},
		{Search: "100%"},
		{Search: "review"},
		{Search: "hook"},
		{Search: "elsewhere"},
		{CreatedByMe: true, TriggerKinds: []string{triggerKindNone}, Search: "review"},
		{TriggerKinds: []string{"webhook"}, Search: "review", States: []RunState{RunStateStarting}},
		{StartedByTrigger: true, CreatedAfter: base.Add(-time.Minute)},
		{CreatedByMe: true, Search: "nothing like it"},
	}
	ats := []string{deploy, review, del}
	scopes := map[string]RunScope{
		"bob sees all":         {AgentTypeIDs: ats, Vis: RunVisibility{SeeAll: true, Subject: "bob"}},
		"no subject":           {AgentTypeIDs: ats, Vis: RunVisibility{SeeAll: true}},
		"alice":                {AgentTypeIDs: ats, Vis: RunVisibility{Subject: "alice"}},
		"alice, two agents":    {AgentTypeIDs: []string{deploy, del}, Vis: RunVisibility{Subject: "alice"}},
		"alice, own runs":      {AgentTypeIDs: []string{review}, Vis: RunVisibility{Subject: "alice"}, Own: true},
		"bob, own runs only":   {Vis: RunVisibility{Subject: "bob"}, Own: true},
		"own, no subject":      {Vis: RunVisibility{}, Own: true},
		"alice, own, narrowed": {AgentTypeIDs: ats, Vis: RunVisibility{Subject: "alice"}, Own: true, OnlyAgentTypeIDs: []string{del}},
		"nothing granted":      {Vis: RunVisibility{SeeAll: true, Subject: "bob"}},
	}
	for scopeName, scope := range scopes {
		for _, q := range queries {
			t.Run(fmt.Sprintf("%s %+v", scopeName, q), func(t *testing.T) {
				q.Scope = scope
				var want []string
				for id, row := range rows {
					if readsRun(tenant, scope, row) && q.matchesRun(row) {
						want = append(want, id)
					}
				}
				q.Limit = 2
				paged := walkRepo(t, r, tenant, q)
				q.Limit = 100
				single, err := r.ListRuns(ctx, tenant, q)
				if err != nil {
					t.Fatal(err)
				}
				var singleIDs []string
				for _, run := range single {
					singleIDs = append(singleIDs, run.ID)
					if run.CreatedByName != names[run.CreatedBy] {
						t.Errorf("run %s created_by_name %q, want %q", run.ID, run.CreatedByName, names[run.CreatedBy])
					}
				}
				if !slices.Equal(paged, singleIDs) {
					t.Fatalf("pages %v differ from the single read %v", paged, singleIDs)
				}
				got := slices.Sorted(slices.Values(paged))
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Errorf("runs %v, want %v", got, want)
				}
			})
		}
	}
	// A predicate that matched nothing would agree with an empty list.
	if n := len(walkRepo(t, r, tenant, RunListQuery{Scope: scopes["bob sees all"], Limit: 100})); n != 12 {
		t.Errorf("unfiltered see-all list holds %d runs, want the 12 live ones", n)
	}
}
