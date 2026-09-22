package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPostgres_ListRuns_CursorPaginationCoversEveryRowOnce exercises the real
// SQL: the ANY($n) readable-agent-type array parameter and the (created_at,
// id) row-tuple cursor comparison. Neither is exercised meaningfully by the
// mock, which reimplements the same rule in Go rather than asking Postgres to
// evaluate it.
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

// TestPostgres_RunStats_MatchesListRuns covers the aggregate query against the
// same fixtures the list query above proves correct: the counts must agree
// with a plain count of what ListRuns (unbounded) returns.
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
