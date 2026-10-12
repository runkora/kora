package agents

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The run scheduling queue against real SQL; gated on ORBIT_TEST_DB_URL (see testRunRepo).

// queueRun writes a pending run with an initial message and sets its queue columns.
func queueRun(t *testing.T, r *PostgresRepository, tenant, agentTypeID string, createdAt time.Time, notBefore *time.Time) string {
	t.Helper()
	ctx := context.Background()
	run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: agentTypeID, InitialMessage: "do " + createdAt.String()}
	if err := r.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET created_at=$1, not_before=$2 WHERE id=$3`, createdAt, notBefore, run.ID); err != nil {
		t.Fatalf("set queue columns: %v", err)
	}
	return run.ID
}

// claimOwn claims runs at now and keeps only this test's tenant, since the queue is shared.
func claimOwn(t *testing.T, r *PostgresRepository, tenant string, now time.Time) []Run {
	t.Helper()
	runs, err := r.ClaimPendingRuns(context.Background(), 1000, now)
	if err != nil {
		t.Fatalf("ClaimPendingRuns: %v", err)
	}
	return slices.DeleteFunc(runs, func(run Run) bool { return run.TenantID != tenant })
}

func runIDs(runs []Run) []string {
	ids := make([]string, len(runs))
	for i, run := range runs {
		ids[i] = run.ID
	}
	return ids
}

func TestPostgres_ClaimPendingRuns(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)

	older := queueRun(t, r, tenant, at.ID, past, &past)
	newer := queueRun(t, r, tenant, at.ID, past.Add(time.Minute), &past)
	queueRun(t, r, tenant, at.ID, past, &future)
	queueRun(t, r, tenant, at.ID, past, nil)
	active := queueRun(t, r, tenant, at.ID, past, &past)
	if _, err := r.pool.Exec(ctx, `UPDATE runs SET state='active' WHERE id=$1`, active); err != nil {
		t.Fatal(err)
	}

	got := claimOwn(t, r, tenant, time.Now())
	if want := []string{older, newer}; !slices.Equal(runIDs(got), want) {
		t.Fatalf("claimed %v, want %v", runIDs(got), want)
	}
	if got[0].InitialMessage != "do "+past.String() {
		t.Errorf("initial message = %q", got[0].InitialMessage)
	}
	if again := claimOwn(t, r, tenant, time.Now()); len(again) != 0 {
		t.Errorf("claimed runs twice: %v", runIDs(again))
	}
}

func TestPostgres_ClaimPendingRunsConcurrent(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	past := time.Now().UTC().Add(-time.Hour)
	want := make([]string, 0, 40)
	for range 40 {
		want = append(want, queueRun(t, r, tenant, at.ID, past, &past))
	}

	var mu sync.Mutex
	var got []string
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for {
				runs, err := r.ClaimPendingRuns(context.Background(), 3, time.Now())
				if err != nil {
					t.Errorf("ClaimPendingRuns: %v", err)
					return
				}
				if len(runs) == 0 {
					return
				}
				runs = slices.DeleteFunc(runs, func(run Run) bool { return run.TenantID != tenant })
				mu.Lock()
				got = append(got, runIDs(runs)...)
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("claimed %d runs (%d unique), want each of %d once", len(got), len(slices.Compact(slices.Clone(got))), len(want))
	}
}

func TestPostgres_ReleaseRunClaim(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)
	id := queueRun(t, r, tenant, at.ID, past, &past)

	claimedAt := time.Now()
	claimOwn(t, r, tenant, claimedAt)
	if err := r.AssignPod(ctx, id, 1); err != nil {
		t.Fatalf("AssignPod: %v", err)
	}
	if err := r.ReleaseRunClaim(ctx, id, claimedAt.Add(time.Second), past); !errors.Is(err, ErrConflict) {
		t.Fatalf("release with another claim token: err = %v, want ErrConflict", err)
	}
	if err := r.ReleaseRunClaim(ctx, id, claimedAt, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("ReleaseRunClaim: %v", err)
	}
	run, err := r.GetRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != RunStatePending || run.PodOrdinal != -1 || run.StartedAt != nil {
		t.Fatalf("released run = state %s, pod %d, started %v", run.State, run.PodOrdinal, run.StartedAt)
	}
	if got := claimOwn(t, r, tenant, time.Now()); len(got) != 0 {
		t.Fatalf("claimed a run before its not_before: %v", runIDs(got))
	}

	claimedAt = time.Now().Add(2 * time.Hour)
	if got := claimOwn(t, r, tenant, claimedAt); !slices.Equal(runIDs(got), []string{id}) {
		t.Fatalf("claimed %v after not_before, want %s", runIDs(got), id)
	}
	if err := r.SetRunState(ctx, id, RunStatePending, RunStateFailed); err != nil {
		t.Fatal(err)
	}
	if err := r.ReleaseRunClaim(ctx, id, claimedAt, past); !errors.Is(err, ErrConflict) {
		t.Fatalf("release of a failed run: err = %v, want ErrConflict", err)
	}
}

func TestPostgres_ReclaimStaleRunClaims(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)
	stale := queueRun(t, r, tenant, at.ID, past, &past)
	starting := queueRun(t, r, tenant, at.ID, past, &past)
	claimOwn(t, r, tenant, past)
	if err := r.AssignPod(ctx, starting, 0); err != nil {
		t.Fatalf("AssignPod: %v", err)
	}
	fresh := queueRun(t, r, tenant, at.ID, past, &past)
	claimOwn(t, r, tenant, time.Now())

	// The cutoff releases stale claims of every tenant in the shared database, which no other test holds.
	reclaimed, err := r.ReclaimStaleRunClaims(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("ReclaimStaleRunClaims: %v", err)
	}
	if !slices.Contains(reclaimed, stale) || slices.Contains(reclaimed, fresh) || slices.Contains(reclaimed, starting) {
		t.Fatalf("reclaimed %v, want %s and not %s or %s", reclaimed, stale, fresh, starting)
	}
	if got := claimOwn(t, r, tenant, time.Now()); !slices.Equal(runIDs(got), []string{stale}) {
		t.Fatalf("claimed %v after reclaim, want %s", runIDs(got), stale)
	}
}
