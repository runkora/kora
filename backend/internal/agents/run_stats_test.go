package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

func TestHistogramRange(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		finished, p99, longest int64
		want                   int64
		capped                 bool
	}{
		{"no runs", 0, 4000, 9000, 0, false},
		{"one below the threshold", cappedMinFinished - 1, 4000, 9000, 9000, false},
		{"exactly the threshold", cappedMinFinished, 4000, 9000, 4000, true},
		{"many runs", 500, 4000, 9000, 4000, true},
		{"an uncapped zero edge is one millisecond", 3, 0, 0, 1, false},
		{"a capped zero edge is one millisecond", cappedMinFinished, 0, 0, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, capped := histogramRange(tc.finished, tc.p99, tc.longest)
			if got != tc.want || capped != tc.capped {
				t.Errorf("range %d capped %v, want %d and %v", got, capped, tc.want, tc.capped)
			}
		})
	}
}

func seedStatsAgent(t *testing.T, repo *mockRepository) {
	t.Helper()
	seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "helper", MaxConcurrentRunsPerPod: 5})
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
			repo.statsResult = RunStats{Total: 3}
			ctx := withRunCallerCtx(context.Background(), "t1", tc.subject)
			got, err := f.run.AgentRunStats(ctx, "helper", usagewindow.Period7d, time.Now())
			if err != nil || got.Total != 3 || repo.statsVis != tc.want || repo.statsAgentTypeID != "at-auto" {
				t.Errorf("stats %+v, visibility %+v for %q, %v; want the repository result for at-auto under %+v", got, repo.statsVis, repo.statsAgentTypeID, err, tc.want)
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
