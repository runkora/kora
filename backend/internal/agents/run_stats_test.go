package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

func TestHistogramRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		finished int64
		want     int64
		capped   bool
	}{
		{"no runs", 0, 0, false},
		{"one below the threshold", cappedMinFinished - 1, 9000, false},
		{"exactly the threshold", cappedMinFinished, 4000, true},
		{"many runs", 500, 4000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, capped := histogramRange(tc.finished, 4000, 9000)
			if got != tc.want || capped != tc.capped {
				t.Errorf("range %d capped %v, want %d and %v", got, capped, tc.want, tc.capped)
			}
		})
	}
}

func TestHistogramRange_ZeroEdgeIsOneMillisecond(t *testing.T) {
	if got, capped := histogramRange(3, 0, 0); got != 1 || capped {
		t.Errorf("uncapped zero range = %d, %v; want 1, false", got, capped)
	}
	if got, capped := histogramRange(cappedMinFinished, 0, 0); got != 1 || !capped {
		t.Errorf("capped zero range = %d, %v; want 1, true", got, capped)
	}
}

func seedStatsAgent(t *testing.T, repo *mockRepository) {
	t.Helper()
	seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "helper", MaxConcurrentRunsPerPod: 5})
}

func TestAgentRunStats_Composes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		finished     FinishedRuns
		wantRange    int64
		wantFinished int64
		wantCapped   bool
		wantOverflow int64
	}{
		{"uncapped uses the longest run", FinishedRuns{RunOutcomes: RunOutcomes{Completed: 3, Failed: 1}, P99Ms: 800, LongestMs: 900}, 900, 4, false, 0},
		{"capped uses p99 and keeps the overflow", FinishedRuns{RunOutcomes: RunOutcomes{Completed: 25, Failed: 5}, P99Ms: 800, LongestMs: 90000}, 800, 30, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
			seedStatsAgent(t, repo)
			repo.statsTotal, repo.statsTriggered, repo.statsFinished = 40, 15, tc.finished
			repo.statsBuckets, repo.statsOverflow = []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, tc.wantOverflow

			ctx := withRunCallerCtx(context.Background(), "t1", "alice")
			got, err := f.run.AgentRunStats(ctx, "helper", usagewindow.Period30d, time.Now())
			if err != nil {
				t.Fatalf("AgentRunStats: %v", err)
			}
			d := got.Durations
			if got.Period != "30d" || got.Total != 40 || got.Triggered != 15 {
				t.Errorf("counts = %+v, want 40 runs, 15 triggered", got)
			}
			if got.Completed+got.Failed != tc.wantFinished || d.Finished != tc.wantFinished || d.Capped != tc.wantCapped || d.Overflow != tc.wantOverflow {
				t.Errorf("durations = %+v, want %d finished, capped %v, overflow %d", d, tc.wantFinished, tc.wantCapped, tc.wantOverflow)
			}
			if repo.statsBucketRange != tc.wantRange || len(d.Buckets) != histogramBuckets {
				t.Errorf("buckets asked for range %d, got %d buckets", repo.statsBucketRange, len(d.Buckets))
			}
		})
	}
}

func TestAgentRunStats_NoFinishedRunsHasEmptyBuckets(t *testing.T) {
	f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
	seedStatsAgent(t, repo)
	repo.statsBuckets = []int64{9, 9, 9, 9, 9, 9, 9, 9, 9, 9}

	ctx := withRunCallerCtx(context.Background(), "t1", "alice")
	got, err := f.run.AgentRunStats(ctx, "helper", usagewindow.Period7d, time.Now())
	if err != nil || !slices.Equal(got.Durations.Buckets, make([]int64, histogramBuckets)) || got.Durations.Capped {
		t.Errorf("stats %+v, %v; want ten zero buckets and not capped", got.Durations, err)
	}
}

func TestAgentRunStats_VisibilityAndUnknownAgent(t *testing.T) {
	for _, tc := range []struct {
		subject string
		want    RunVisibility
	}{
		{"alice", RunVisibility{Subject: "alice"}},
		{"auditor", RunVisibility{SeeAll: true, Subject: "auditor"}},
	} {
		t.Run(tc.subject, func(t *testing.T) {
			f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
			seedStatsAgent(t, repo)
			ctx := withRunCallerCtx(context.Background(), "t1", tc.subject)
			if _, err := f.run.AgentRunStats(ctx, "helper", usagewindow.Period7d, time.Now()); err != nil || repo.statsVis != tc.want || repo.statsAgentTypeID != "at-auto" {
				t.Errorf("visibility %+v for %q, %v; want %+v for at-auto", repo.statsVis, repo.statsAgentTypeID, err, tc.want)
			}
		})
	}

	f, _, _, _ := newTestServiceWithEval(t, auditorEval)
	ctx := withRunCallerCtx(context.Background(), "t1", "alice")
	if _, err := f.run.AgentRunStats(ctx, "missing", usagewindow.Period7d, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown agent gave %v, want ErrNotFound", err)
	}
}

func TestAgentRunStats_LookupFailureIsNotNotFound(t *testing.T) {
	f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
	boom := errors.New("connection reset")
	repo.getBySlugErr = boom
	ctx := withRunCallerCtx(context.Background(), "t1", "alice")
	_, err := f.run.AgentRunStats(ctx, "helper", usagewindow.Period7d, time.Now())
	if !errors.Is(err, boom) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want the lookup error and not ErrNotFound", err)
	}
}

// denyRunsEval allows everything except reading the runs of one agent.
type denyRunsEval struct{ allowAllEvaluator }

func (denyRunsEval) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	denied := len(in.PathSegments) == 2 && in.PathSegments[0] == "runs" && in.PathSegments[1] == "helper"
	return policy.Result{Allow: !denied}, nil
}

func TestAgentRunStats_DeniedCallerGetsNotFound(t *testing.T) {
	f, repo, _, _ := newTestServiceWithEval(t, denyRunsEval{})
	seedStatsAgent(t, repo)
	ctx := withRunCallerCtx(context.Background(), "t1", "alice")
	if _, err := f.run.AgentRunStats(ctx, "helper", usagewindow.Period7d, time.Now()); !errors.Is(err, ErrNotFound) || repo.statsAgentTypeID != "" {
		t.Errorf("err = %v, read agent type %q; want ErrNotFound and no read", err, repo.statsAgentTypeID)
	}
}

func TestHandleRunStats(t *testing.T) {
	for _, tc := range []struct {
		name, path, slug string
		lookupErr        error
		want             int
	}{
		{"a bad period", "/api/v1/run-stats/helper?period=1y", "helper", nil, http.StatusBadRequest},
		{"an unknown agent", "/api/v1/run-stats/missing", "missing", nil, http.StatusNotFound},
		{"a lookup failure", "/api/v1/run-stats/helper", "helper", errors.New("connection reset"), http.StatusInternalServerError},
		{"a known agent", "/api/v1/run-stats/helper?period=24h", "helper", nil, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, repo, _, _ := newTestControllerWithEval(t, auditorEval)
			seedStatsAgent(t, repo)
			repo.getBySlugErr = tc.lookupErr
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = req.WithContext(withRunCallerCtx(req.Context(), "t1", "alice"))
			req.SetPathValue("slug", tc.slug)
			w := httptest.NewRecorder()
			c.handleRunStats(w, req)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
