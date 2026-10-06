package agents

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

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
		wantCapped   bool
		wantOverflow int64
	}{
		{"uncapped uses the longest run", FinishedRuns{Completed: 3, Failed: 1, P99Ms: 800, LongestMs: 900}, 900, false, 0},
		{"capped uses p99 and keeps the overflow", FinishedRuns{Completed: 25, Failed: 5, P99Ms: 800, LongestMs: 90000}, 800, true, 2},
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
			if got.Period != "30d" || got.Total != 40 || got.Triggered != 15 || got.Manual != 25 {
				t.Errorf("counts = %+v, want 40 runs, 15 triggered, 25 manual", got)
			}
			if got.Completed+got.Failed != d.Finished || d.RangeMs != tc.wantRange || d.Capped != tc.wantCapped || d.Overflow != tc.wantOverflow {
				t.Errorf("durations = %+v, want range %d capped %v overflow %d", d, tc.wantRange, tc.wantCapped, tc.wantOverflow)
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
