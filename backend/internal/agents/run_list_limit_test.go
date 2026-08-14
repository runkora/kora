package agents

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/policy"
)

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

// GET /api/v1/runs backs a newest-first activity list and hands every returned
// row to filterRuns as a policy candidate, so the row count is bounded rather
// than following tenant history.

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

func TestListRuns_AppliesDefaultLimit(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", DefaultRunListLimit+50, 3)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != DefaultRunListLimit {
		t.Errorf("len(runs) = %d, want %d (unspecified limit must take the default, not the whole table)", len(runs), DefaultRunListLimit)
	}
}

func TestListRuns_ReturnsNewestFirstUnderTheLimit(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 40, 2)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 10 {
		t.Fatalf("len(runs) = %d, want 10", len(runs))
	}
	// run-0000 is newest; the cap must drop the oldest, not an arbitrary slice.
	for i, r := range runs {
		want := fmt.Sprintf("run-%04d", i)
		if r.ID != want {
			t.Errorf("runs[%d].ID = %q, want %q", i, r.ID, want)
		}
	}
}

func TestListRuns_LimitIsCappedAtMax(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", MaxRunListLimit+25, 4)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", MaxRunListLimit*10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != MaxRunListLimit {
		t.Errorf("len(runs) = %d, want %d (a caller must not be able to lift the cap)", len(runs), MaxRunListLimit)
	}
}

func TestListRuns_SmallerResultSetIsUnaffected(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 7, 2)

	runs, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 7 {
		t.Errorf("len(runs) = %d, want 7", len(runs))
	}
}

func TestClampRunListLimit(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"unspecified takes the default", 0, DefaultRunListLimit},
		{"negative takes the default", -5, DefaultRunListLimit},
		{"in range is honoured", 25, 25},
		{"the max itself is honoured", MaxRunListLimit, MaxRunListLimit},
		{"above the max is capped", MaxRunListLimit + 1, MaxRunListLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampRunListLimit(tt.in); got != tt.want {
				t.Errorf("clampRunListLimit(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// The limit is applied in the repository query, ahead of the authz filter, so
// a principal whose grants cover only some agent types sees fewer than limit
// rows. This is the documented trade-off; assert it so a later change to make
// the count exact is a deliberate one.
func TestListRuns_AuthzFilterAppliesAfterTheLimit(t *testing.T) {
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
		t.Errorf("len(runs) = %d, want between 1 and 9: the cap is applied before the authz filter", len(runs))
	}
	for _, r := range runs {
		if r.AgentTypeID != "at-0" {
			t.Errorf("run %s has agent type %q, want at-0", r.ID, r.AgentTypeID)
		}
	}
}
