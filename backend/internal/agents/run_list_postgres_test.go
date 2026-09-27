package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPostgres_ListRuns_CursorPaginationCoversEveryRowOnce exercises the real
// SQL: the ANY($n) readable-agent-type parameter and the (created_at, id)
// cursor comparison, neither of which the mock exercises meaningfully.
func TestPostgres_ListRuns_CursorPaginationCoversEveryRowOnce(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant)

	const total = 23
	base := time.Now().UTC()
	ids := make([]string, total)
	for i := range total {
		run := Run{
			ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID,
			State: RunStateActive, CreatedAt: base.Add(-time.Duration(i) * time.Second),
		}
		if err := r.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun %d: %v", i, err)
		}
		ids[i] = run.ID
	}

	filter := RunListFilter{
		ReadableAgentTypeIDs: []string{at.ID},
		Visibility:           RunVisibility{SeeAll: true},
		Limit:                7,
	}

	var seen []string
	for page := 0; ; page++ {
		if page > total {
			t.Fatalf("did not terminate after %d pages", page)
		}
		runs, hasMore, err := r.ListRuns(ctx, tenant, filter)
		if err != nil {
			t.Fatalf("ListRuns page %d: %v", page, err)
		}
		for _, run := range runs {
			seen = append(seen, run.ID)
		}
		if !hasMore {
			break
		}
		last := runs[len(runs)-1]
		filter.CursorTime, filter.CursorID = &last.CreatedAt, &last.ID
	}

	if len(seen) != total {
		t.Fatalf("saw %d runs across all pages, want %d", len(seen), total)
	}
	seenSet := make(map[string]struct{}, len(seen))
	for _, id := range seen {
		if _, dup := seenSet[id]; dup {
			t.Errorf("run %s returned twice across pages", id)
		}
		seenSet[id] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := seenSet[id]; !ok {
			t.Errorf("run %s never appeared in any page", id)
		}
	}
}

// TestPostgres_ListRuns_EmptyReadableSetFailsClosed proves the ANY($n) clause
// against an empty array excludes every row, rather than the NULL/unrestricted
// behaviour a subtly wrong query could produce.
func TestPostgres_ListRuns_EmptyReadableSetFailsClosed(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant)

	run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID, State: RunStateActive}
	if err := r.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	runs, hasMore, err := r.ListRuns(ctx, tenant, RunListFilter{
		ReadableAgentTypeIDs: nil,
		Visibility:           RunVisibility{SeeAll: true},
	})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("len(runs) = %d, want 0", len(runs))
	}
	if hasMore {
		t.Error("hasMore = true, want false")
	}
}

// TestPostgres_RunStats_MatchesListRuns covers the aggregate query: its
// counts must agree with a plain count of what ListRuns (unbounded) returns.
func TestPostgres_RunStats_MatchesListRuns(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant)

	// CreateRun always inserts as pending — every run starts there and moves
	// through SetRunState's compare-and-swap transitions. Seeding an arbitrary
	// terminal state for this fixture goes around that machine directly.
	states := []RunState{RunStateActive, RunStateIdle, RunStateCompleted, RunStateCompleted, RunStateFailed}
	for i, state := range states {
		run := Run{
			ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID,
			CreatedAt: time.Now().UTC().Add(-time.Duration(i) * time.Second),
		}
		if err := r.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun %d: %v", i, err)
		}
		if _, err := r.pool.Exec(ctx, `UPDATE runs SET state=$1 WHERE id=$2`, string(state), run.ID); err != nil {
			t.Fatalf("seed state %d: %v", i, err)
		}
	}

	filter := RunListFilter{
		ReadableAgentTypeIDs: []string{at.ID},
		Visibility:           RunVisibility{SeeAll: true},
	}

	stats, err := r.RunStats(ctx, tenant, filter)
	if err != nil {
		t.Fatalf("RunStats: %v", err)
	}
	if stats.Total != len(states) {
		t.Errorf("Total = %d, want %d", stats.Total, len(states))
	}
	if stats.Active != 2 {
		t.Errorf("Active = %d, want 2 (active + idle)", stats.Active)
	}
	if stats.Completed != 2 {
		t.Errorf("Completed = %d, want 2", stats.Completed)
	}
	if stats.Failed != 1 {
		t.Errorf("Failed = %d, want 1", stats.Failed)
	}

	filter.Limit = 0
	runs, _, err := r.ListRuns(ctx, tenant, filter)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != stats.Total {
		t.Errorf("ListRuns returned %d rows, RunStats.Total = %d: must agree", len(runs), stats.Total)
	}
}

// TestPostgres_RunStats_AverageCompletedDuration covers the average-duration
// aggregate itself: AVG(EXTRACT(EPOCH FROM (completed_at - started_at)) *
// 1000) FILTER (...). The mock's equivalent (TestGetRunStats_*) exercises the
// same contract in Go, not the SQL, so this is the only coverage of the
// expression's arithmetic and FILTER semantics against a real database.
func TestPostgres_RunStats_AverageCompletedDuration(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	at := seedRunAgentType(t, r, tenant)

	base := time.Now().UTC()
	seedCompleted := func(startedAt, completedAt time.Time) string {
		run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID, CreatedAt: base}
		if err := r.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		if _, err := r.pool.Exec(ctx,
			`UPDATE runs SET state='completed', started_at=$1, completed_at=$2 WHERE id=$3`,
			startedAt, completedAt, run.ID,
		); err != nil {
			t.Fatalf("seed started/completed_at: %v", err)
		}
		return run.ID
	}

	seedCompleted(base, base.Add(10*time.Second))
	seedCompleted(base, base.Add(30*time.Second))

	// Completed but never started: must be excluded by the FILTER, not just
	// coerced to zero, which would skew the average low.
	unstarted := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID, CreatedAt: base}
	if err := r.CreateRun(ctx, unstarted); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET state='completed' WHERE id=$1`, unstarted.ID); err != nil {
		t.Fatalf("seed unstarted completed run: %v", err)
	}

	filter := RunListFilter{
		ReadableAgentTypeIDs: []string{at.ID},
		Visibility:           RunVisibility{SeeAll: true},
	}
	stats, err := r.RunStats(ctx, tenant, filter)
	if err != nil {
		t.Fatalf("RunStats: %v", err)
	}
	if stats.Total != 3 {
		t.Errorf("Total = %d, want 3", stats.Total)
	}
	if stats.AvgCompletedDurationMs == nil {
		t.Fatal("AvgCompletedDurationMs is nil, want a value")
	}
	want := int64(20_000) // (10s + 30s) / 2, the unstarted run excluded
	if *stats.AvgCompletedDurationMs != want {
		t.Errorf("AvgCompletedDurationMs = %d, want %d", *stats.AvgCompletedDurationMs, want)
	}
}

// TestPostgres_RunVisibility_DelegatedRunsFollowTheSubject runs the
// delegated-run visibility clause in runListWhere against a real database.
// Every other Postgres test reads with SeeAll, so this is the only coverage of
// that SQL. ListRuns and RunStats share the clause, so both are checked.
func TestPostgres_RunVisibility_DelegatedRunsFollowTheSubject(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()

	autonomous := seedRunAgentType(t, r, tenant)
	delegated := seedRunAgentType(t, r, tenant)
	if _, err := r.pool.Exec(ctx, `UPDATE agent_types SET identity_mode='delegated' WHERE id=$1`, delegated.ID); err != nil {
		t.Fatalf("mark agent type delegated: %v", err)
	}

	base := time.Now().UTC()
	seed := func(agentTypeID, createdBy string) string {
		run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: agentTypeID, CreatedBy: createdBy, CreatedAt: base}
		if err := r.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		return run.ID
	}
	aliceDelegated1 := seed(delegated.ID, "alice")
	aliceDelegated2 := seed(delegated.ID, "alice")
	bobDelegated := seed(delegated.ID, "bob")
	bobAutonomous := seed(autonomous.ID, "bob")

	cases := []struct {
		name       string
		visibility RunVisibility
		want       []string
	}{
		{"admin sees every run", RunVisibility{SeeAll: true}, []string{aliceDelegated1, aliceDelegated2, bobDelegated, bobAutonomous}},
		{"subject sees own delegated runs and all autonomous runs", RunVisibility{Subject: "alice"}, []string{aliceDelegated1, aliceDelegated2, bobAutonomous}},
		{"empty subject sees no delegated run", RunVisibility{}, []string{bobAutonomous}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filter := RunListFilter{
				ReadableAgentTypeIDs: []string{autonomous.ID, delegated.ID},
				Visibility:           tc.visibility,
				Limit:                50,
			}
			runs, _, err := r.ListRuns(ctx, tenant, filter)
			if err != nil {
				t.Fatalf("ListRuns: %v", err)
			}
			got := make(map[string]bool, len(runs))
			for _, run := range runs {
				got[run.ID] = true
			}
			if len(got) != len(tc.want) {
				t.Errorf("ListRuns returned %d runs, want %d", len(got), len(tc.want))
			}
			for _, id := range tc.want {
				if !got[id] {
					t.Errorf("ListRuns is missing run %s", id)
				}
			}

			stats, err := r.RunStats(ctx, tenant, filter)
			if err != nil {
				t.Fatalf("RunStats: %v", err)
			}
			if stats.Total != len(tc.want) {
				t.Errorf("RunStats.Total = %d, want %d", stats.Total, len(tc.want))
			}
		})
	}
}

// TestPostgres_RunStats_ScopedToOneAgentType covers the agent-type path of
// RunStats, which the agent detail page reads: counts cover only that agent
// type, and the per-agent-type active breakdown belongs to the tenant-wide
// call alone.
func TestPostgres_RunStats_ScopedToOneAgentType(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	a := seedRunAgentType(t, r, tenant)
	b := seedRunAgentType(t, r, tenant)

	base := time.Now().UTC()
	seed := func(agentTypeID string, state RunState) {
		run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: agentTypeID, CreatedAt: base}
		if err := r.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		if _, err := r.pool.Exec(ctx, `UPDATE runs SET state=$1 WHERE id=$2`, string(state), run.ID); err != nil {
			t.Fatalf("set run state: %v", err)
		}
	}
	seed(a.ID, RunStateActive)
	seed(a.ID, RunStateCompleted)
	seed(a.ID, RunStateFailed)
	seed(b.ID, RunStateActive)

	readable := []string{a.ID, b.ID}
	scoped, err := r.RunStats(ctx, tenant, RunListFilter{
		AgentTypeID:          a.ID,
		ReadableAgentTypeIDs: readable,
		Visibility:           RunVisibility{SeeAll: true},
	})
	if err != nil {
		t.Fatalf("RunStats scoped: %v", err)
	}
	if scoped.Total != 3 || scoped.Active != 1 || scoped.Completed != 1 || scoped.Failed != 1 {
		t.Errorf("scoped stats = total %d active %d completed %d failed %d, want 3/1/1/1",
			scoped.Total, scoped.Active, scoped.Completed, scoped.Failed)
	}
	if len(scoped.ActiveByAgentType) != 0 {
		t.Errorf("scoped ActiveByAgentType = %v, want empty", scoped.ActiveByAgentType)
	}

	tenantWide, err := r.RunStats(ctx, tenant, RunListFilter{
		ReadableAgentTypeIDs: readable,
		Visibility:           RunVisibility{SeeAll: true},
	})
	if err != nil {
		t.Fatalf("RunStats tenant-wide: %v", err)
	}
	if tenantWide.Total != 4 || tenantWide.Active != 2 {
		t.Errorf("tenant-wide stats = total %d active %d, want 4/2", tenantWide.Total, tenantWide.Active)
	}
	if tenantWide.ActiveByAgentType[a.ID] != 1 || tenantWide.ActiveByAgentType[b.ID] != 1 {
		t.Errorf("tenant-wide ActiveByAgentType = %v, want 1 for each agent type", tenantWide.ActiveByAgentType)
	}
}
