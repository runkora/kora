package agents

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/policy"
)

// Visibility rules are applied inside the repository query (RunListFilter),
// so a page's LIMIT never returns fewer rows than fit because some were
// dropped afterward. See #209.

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

// denyAllEvaluator grants nothing, for the fail-closed test.
type denyAllEvaluator struct{ allowAllEvaluator }

func (denyAllEvaluator) Query(context.Context, policy.QueryInput) ([]string, error) {
	return nil, nil
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

func TestListRuns_DefaultPageSize(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 80, 3)

	runs, hasMore, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", nil, 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != defaultRunsLimit {
		t.Errorf("len(runs) = %d, want the default page size %d", len(runs), defaultRunsLimit)
	}
	if !hasMore {
		t.Error("hasMore = false, want true: 80 seeded rows exceed the default page")
	}
}

func TestListRuns_ExplicitLimitIsHonoured(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 40, 2)

	runs, hasMore, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", nil, 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 10 {
		t.Fatalf("len(runs) = %d, want 10", len(runs))
	}
	if !hasMore {
		t.Error("hasMore = false, want true")
	}
	// run-0000 is newest; an explicit limit keeps the newest, not an arbitrary slice.
	for i, r := range runs {
		want := fmt.Sprintf("run-%04d", i)
		if r.ID != want {
			t.Errorf("runs[%d].ID = %q, want %q", i, r.ID, want)
		}
	}
}

func TestListRuns_LimitAboveCeilingIsClamped(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", maxRunsLimit+50, 4)

	// The HTTP handler rejects an over-ceiling limit with a 400; any other
	// caller of the service gets it capped by runsPageSize.
	runs, hasMore, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", nil, maxRunsLimit+50)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != maxRunsLimit {
		t.Errorf("len(runs) = %d, want the ceiling %d", len(runs), maxRunsLimit)
	}
	if !hasMore {
		t.Error("hasMore = false, want true")
	}
}

// This is the fix for #209: pushing the visibility filter into the query
// means an explicit limit returns exactly that many rows when enough are
// readable, rather than a page the authz filter then thins out unpredictably.
func TestListRuns_ExactPageWithNarrowedVisibility(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 60, 2)

	// Permit only at-0's runs. Slugs come from seedRun's "stub-<id>" default;
	// readableAgentTypeIDsForRuns builds the candidate path as ["runs", slug].
	svc.run.authz = NewAuthz(&pathAllowEvaluator{segment: "stub-at-0"})

	runs, hasMore, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", nil, 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 10 {
		t.Errorf("len(runs) = %d, want exactly 10: the visibility filter is applied in the query, so a page never comes back short", len(runs))
	}
	if !hasMore {
		t.Error("hasMore = false, want true: at-0 has 30 runs, more remain after this page")
	}
	for _, r := range runs {
		if r.AgentTypeID != "at-0" {
			t.Errorf("run %s has agent type %q, want at-0", r.ID, r.AgentTypeID)
		}
	}
}

// A principal with no readable agent type must see nothing, not everything —
// the acceptance criterion #209 states explicitly.
func TestListRuns_EmptyReadableSetFailsClosed(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 20, 2)
	svc.run.authz = NewAuthz(&denyAllEvaluator{})

	runs, hasMore, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", nil, 0)
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

// Walking the cursor must reach every row exactly once, in the same order a
// single unbounded read would produce, with hasMore turning false on the last
// page.
func TestListRuns_CursorPaginationCoversEveryRowOnce(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	const total = 137
	seedRunsAcrossTypes(t, repo, "t-1", total, 3)
	ctx := withTenantCtx(context.Background(), "t-1")

	var seen []string
	var cursor *runCursor
	for page := 0; ; page++ {
		if page > total {
			t.Fatalf("did not terminate after %d pages", page)
		}
		runs, hasMore, err := svc.run.ListRuns(ctx, "", cursor, 25)
		if err != nil {
			t.Fatalf("ListRuns page %d: %v", page, err)
		}
		for _, r := range runs {
			seen = append(seen, r.ID)
		}
		if !hasMore {
			if len(runs) == 0 && page == 0 {
				t.Fatal("first page was empty")
			}
			break
		}
		last := runs[len(runs)-1]
		cursor = &runCursor{CreatedAt: last.CreatedAt, ID: last.ID}
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
	for i := range total {
		want := fmt.Sprintf("run-%04d", i)
		if _, ok := seenSet[want]; !ok {
			t.Errorf("run %s never appeared in any page", want)
		}
	}
}

// The readable-agent-type resolution costs one evaluation per distinct agent
// type in the tenant, not one per run: it runs once, over the tenant's agent
// types, before the runs query — never per row.
func TestListRuns_VisibilityResolutionCostsOneEvaluationPerAgentType(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 800, 5)

	counting := &countingQueryEvaluator{}
	svc.run.authz = NewAuthz(counting)

	runs, hasMore, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", nil, 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != defaultRunsLimit {
		t.Errorf("len(runs) = %d, want the default page size %d", len(runs), defaultRunsLimit)
	}
	if !hasMore {
		t.Error("hasMore = false, want true")
	}
	if counting.calls != 1 {
		t.Errorf("policy Query calls = %d, want 1 batched call", counting.calls)
	}
	if counting.candidates != 5 {
		t.Errorf("candidates passed = %d, want 5 (one per distinct agent type)", counting.candidates)
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
