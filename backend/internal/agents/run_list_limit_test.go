package agents

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/policy"
)

// GET /api/v1/runs backs the dashboard's activity history. It returns every run
// by default: filterRuns runs after any row cap, so a truncated response is
// indistinguishable from a complete one and rows go missing silently.

// pathAllowEvaluator allows only candidates whose resource path contains
// segment, so a test can permit one agent type's runs and deny the rest.
type pathAllowEvaluator struct {
	allowAllEvaluator
	segment string
}

func (e pathAllowEvaluator) Query(_ context.Context, in policy.QueryInput) ([]string, error) {
	out := make([]string, 0, len(in.Candidates))
	for _, c := range in.Candidates {
		if slices.Contains(c.PathSegments, e.segment) {
			out = append(out, c.ID)
		}
	}
	return out, nil
}

// seedRunsAcrossTypes seeds n runs for one tenant, spread over typeCount agent
// types, with strictly decreasing created_at so "newest" is unambiguous.
func seedRunsAcrossTypes(t *testing.T, repo *mockRepository, tenant string, n, typeCount int) {
	t.Helper()
	base := time.Now()
	for i := range n {
		seedRun(t, repo, Run{
			ID:          fmt.Sprintf("run-%04d", i),
			TenantID:    tenant,
			AgentTypeID: fmt.Sprintf("at-%d", i%typeCount),
			State:       RunStateActive,
			CreatedAt:   base.Add(-time.Duration(i) * time.Second),
		})
	}
}

func TestListRuns_ReturnsEveryRunByDefault(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	const total = 640
	seedRunsAcrossTypes(t, repo, "t-1", total, 3)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != total {
		t.Errorf("len(runs) = %d, want %d: an unspecified limit must not truncate the history", len(runs), total)
	}
}

func TestListRuns_NegativeLimitIsAlsoUnbounded(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 300, 2)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", -1)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 300 {
		t.Errorf("len(runs) = %d, want 300", len(runs))
	}
}

func TestListRuns_ExplicitLimitIsHonoured(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 40, 2)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 10 {
		t.Fatalf("len(runs) = %d, want 10", len(runs))
	}
	// run-0000 is newest; an explicit limit keeps the newest, not an arbitrary slice.
	for i, r := range runs {
		want := fmt.Sprintf("run-%04d", i)
		if r.ID != want {
			t.Errorf("runs[%d].ID = %q, want %q", i, r.ID, want)
		}
	}
}

func TestListRuns_NoArbitraryUpperBoundOnExplicitLimit(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 1500, 4)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", 1200)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1200 {
		t.Errorf("len(runs) = %d, want 1200", len(runs))
	}
}

// An explicit limit is applied in the query, ahead of the authz filter, so a
// principal whose grants cover only some agent types sees fewer than limit rows.
// This is why no limit is applied by default; issue #209 tracks pushing the
// visibility filter into the query so a bound can return an exact count.
func TestListRuns_ExplicitLimitAppliesBeforeTheAuthzFilter(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 60, 2)

	// Permit only at-0's runs. Slugs come from seedRun's "stub-<id>" default,
	// and filterRuns builds the candidate path as ["runs", slug].
	svc.run.authz = NewAuthz(&pathAllowEvaluator{segment: "stub-at-0"})

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) == 0 || len(runs) >= 10 {
		t.Errorf("len(runs) = %d, want between 1 and 9", len(runs))
	}
	for _, r := range runs {
		if r.AgentTypeID != "at-0" {
			t.Errorf("run %s has agent type %q, want at-0", r.ID, r.AgentTypeID)
		}
	}
}

// Unbounded is only safe because the authz filter no longer costs one policy
// evaluation per row (policy.engine.Query memoises on the rego input), so a
// large history must still resolve to one evaluation per distinct agent type.
func TestListRuns_UnboundedCostsOneEvaluationPerAgentType(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 800, 5)

	counting := &countingQueryEvaluator{}
	svc.run.authz = NewAuthz(counting)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 800 {
		t.Errorf("len(runs) = %d, want 800", len(runs))
	}
	if counting.calls != 1 {
		t.Errorf("policy Query calls = %d, want 1 batched call", counting.calls)
	}
	if counting.candidates != 800 {
		t.Errorf("candidates passed = %d, want 800 in the single call", counting.candidates)
	}
}

// countingQueryEvaluator records how the authz filter batches its candidates.
type countingQueryEvaluator struct {
	allowAllEvaluator
	calls      int
	candidates int
}

func (c *countingQueryEvaluator) Query(_ context.Context, in policy.QueryInput) ([]string, error) {
	c.calls++
	c.candidates += len(in.Candidates)
	out := make([]string, 0, len(in.Candidates))
	for _, cand := range in.Candidates {
		out = append(out, cand.ID)
	}
	return out, nil
}
