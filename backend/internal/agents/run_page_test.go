package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
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

// countingQueryEvaluator allows every candidate and records how they were batched.
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

// seedRunsAcrossTypes seeds n runs for one tenant over typeCount agent types.
// Runs come in threes that share a created_at, so pages cross ties.
func seedRunsAcrossTypes(t *testing.T, repo *mockRepository, tenant string, n, typeCount int) {
	t.Helper()
	base := time.Now().UTC().Truncate(time.Microsecond)
	for i := range n {
		seedRun(t, repo, Run{
			ID:          fmt.Sprintf("run-%04d", i),
			TenantID:    tenant,
			AgentTypeID: fmt.Sprintf("at-%d", i%typeCount),
			State:       RunStateActive,
			CreatedAt:   base.Add(-time.Duration(i-i%3) * time.Second),
		})
	}
}

// walkRuns follows next cursors from the first page and returns every run in order.
func walkRuns(t *testing.T, ctx context.Context, svc *RunService, agentTypeID string, p RunListQuery) (runs []Run, pages int) {
	t.Helper()
	for {
		page, err := svc.ListRuns(ctx, agentTypeID, p)
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		pages++
		runs = append(runs, page.Runs...)
		if page.NextCursor == "" {
			return runs, pages
		}
		if len(page.Runs) != p.Limit {
			t.Fatalf("page %d holds %d runs and has a next cursor, want a full page of %d", pages, len(page.Runs), p.Limit)
		}
		if p.After, err = decodeRunCursor(page.NextCursor); err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		if pages > 100 {
			t.Fatal("pagination did not terminate")
		}
	}
}

func assertNewestFirstNoDuplicates(t *testing.T, runs []Run) {
	t.Helper()
	seen := map[string]bool{}
	for i, r := range runs {
		if seen[r.ID] {
			t.Fatalf("run %s appears twice", r.ID)
		}
		seen[r.ID] = true
		if i > 0 {
			prev := runs[i-1]
			if r.CreatedAt.After(prev.CreatedAt) || (r.CreatedAt.Equal(prev.CreatedAt) && r.ID > prev.ID) {
				t.Fatalf("run %s is out of (created_at, id) DESC order after %s", r.ID, prev.ID)
			}
		}
	}
}

func TestListRuns_PagesCoverEveryRunOnce(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 130, 3)

	runs, pages := walkRuns(t, withTenantCtx(context.Background(), "t-1"), svc.run, "", RunListQuery{Limit: 50})
	if pages != 3 || len(runs) != 130 {
		t.Fatalf("got %d runs over %d pages, want 130 over 3", len(runs), pages)
	}
	assertNewestFirstNoDuplicates(t, runs)
}

// The read grant is part of the query, so a caller who may read one agent's
// runs gets full pages of them, not pages thinned after the limit.
func TestListRuns_GrantFilterKeepsPagesFull(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 90, 3)
	svc.run.authz = NewAuthz(&pathAllowEvaluator{segment: "stub-at-0"})
	ctx := withTenantCtx(context.Background(), "t-1")

	runs, _ := walkRuns(t, ctx, svc.run, "", RunListQuery{Limit: 10})
	if len(runs) != 30 {
		t.Fatalf("got %d runs, want the 30 runs of at-0", len(runs))
	}
	for _, r := range runs {
		if r.AgentTypeID != "at-0" {
			t.Errorf("run %s of %s is outside the grant", r.ID, r.AgentTypeID)
		}
	}
	assertNewestFirstNoDuplicates(t, runs)

	page, err := svc.run.ListRuns(ctx, "at-1", RunListQuery{Limit: 10})
	if err != nil || len(page.Runs) != 0 || page.NextCursor != "" {
		t.Errorf("an agent outside the grant gave %d runs, cursor %q, %v; want none", len(page.Runs), page.NextCursor, err)
	}
}

func TestListRuns_AgentTypeAndStateFilters(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 30, 3)
	repo.runs["run-0003"].State = RunStateFailed
	repo.runs["run-0004"].State = RunStateFailed
	repo.runs["run-0006"].State = RunStateFailed
	ctx := withTenantCtx(context.Background(), "t-1")

	runs, _ := walkRuns(t, ctx, svc.run, "at-0", RunListQuery{States: []RunState{RunStateFailed}, Limit: 1})
	var ids []string
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	if !reflect.DeepEqual(ids, []string{"run-0003", "run-0006"}) {
		t.Errorf("failed runs of at-0 = %v, want [run-0003 run-0006]", ids)
	}
}

// One policy query per page, over the tenant's agent types rather than its runs.
func TestListRuns_OnePolicyQueryOverAgentTypes(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 400, 5)
	counting := &countingQueryEvaluator{}
	svc.run.authz = NewAuthz(counting)

	if _, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), "", RunListQuery{Limit: 200}); err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if counting.calls != 1 || counting.candidates != 5 {
		t.Errorf("policy queries = %d over %d candidates, want 1 over the 5 agent types", counting.calls, counting.candidates)
	}
}

func TestRunCursor_RoundTrip(t *testing.T) {
	want := runCursor{CreatedAt: time.Date(2026, 10, 7, 9, 30, 0, 123456000, time.UTC), ID: "run|with|bars"}
	got, err := decodeRunCursor(want.encode())
	if err != nil || !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Errorf("decode(encode(%+v)) = %+v, %v", want, got, err)
	}
	if got, err := decodeRunCursor(""); got != nil || err != nil {
		t.Errorf("an empty cursor = %+v, %v; want the first page", got, err)
	}
	for _, raw := range []string{
		"not base64!",
		base64.RawURLEncoding.EncodeToString([]byte("no-separator")),
		base64.RawURLEncoding.EncodeToString([]byte("yesterday|run-1")),
		base64.RawURLEncoding.EncodeToString([]byte("2026-10-07T09:30:00Z|")),
	} {
		if _, err := decodeRunCursor(raw); !errors.Is(err, ErrInvalidRunCursor) {
			t.Errorf("decode(%q) err = %v, want ErrInvalidRunCursor", raw, err)
		}
	}
}

func TestParseRunStates(t *testing.T) {
	got, err := parseRunStates("pending, active")
	if err != nil || !reflect.DeepEqual(got, []RunState{RunStatePending, RunStateActive}) {
		t.Errorf("parse = %v, %v", got, err)
	}
	if _, err := parseRunStates("active,sleeping"); !errors.Is(err, ErrInvalidRunState) {
		t.Errorf("an unknown state gave %v, want ErrInvalidRunState", err)
	}
}

// ── GET /api/v1/runs ──────────────────────────────────────────────────────────

type listRunsBody struct {
	Runs       []Run   `json:"runs"`
	NextCursor *string `json:"next_cursor"`
	Limit      int     `json:"limit"`
}

func listRuns(t *testing.T, c *Controller, query string) (int, listRunsBody) {
	t.Helper()
	w := getRequest(t, c.handleListRuns, query)
	var body listRunsBody
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
	}
	return w.Code, body
}

func TestHandleListRuns_Limits(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRunsAcrossTypes(t, repo, "t-1", 260, 2)
	for _, id := range []string{"run-0001", "run-0002", "run-0003"} {
		repo.runs[id].State = RunStateFailed
	}

	for _, tc := range []struct {
		name, query         string
		wantRuns, wantLimit int
		wantNext            bool
	}{
		{"absent takes the default", "", defaultRunsLimit, defaultRunsLimit, true},
		{"an explicit limit", "limit=7", 7, 7, true},
		{"the maximum", "limit=200", maxRunsLimit, maxRunsLimit, true},
		{"above the maximum is clamped", "limit=5000", maxRunsLimit, maxRunsLimit, true},
		{"the last page has a null cursor", "state=failed", 3, defaultRunsLimit, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := listRuns(t, c, tc.query)
			if code != http.StatusOK || len(body.Runs) != tc.wantRuns || body.Limit != tc.wantLimit || (body.NextCursor != nil) != tc.wantNext {
				t.Errorf("status %d, %d runs, limit %d, cursor %v; want 200, %d runs, limit %d, next cursor %v",
					code, len(body.Runs), body.Limit, body.NextCursor, tc.wantRuns, tc.wantLimit, tc.wantNext)
			}
		})
	}
}

func TestHandleListRuns_BadParamsReturn400(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRunsAcrossTypes(t, repo, "t-1", 3, 1)
	for _, q := range []string{"limit=0", "limit=-1", "limit=ten", "cursor=%21%21", "state=sleeping"} {
		if code, _ := listRuns(t, c, q); code != http.StatusBadRequest {
			t.Errorf("%s gave %d, want 400", q, code)
		}
	}
}

func TestHandleListRuns_RepeatedStateKeysCombine(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRunsAcrossTypes(t, repo, "t-1", 6, 1)
	repo.runs["run-0001"].State = RunStateFailed
	repo.runs["run-0002"].State = RunStatePending
	code, body := listRuns(t, c, "state=failed&state=pending")
	if code != http.StatusOK || len(body.Runs) != 2 {
		t.Errorf("status %d, %d runs; want 200 and the failed and pending runs", code, len(body.Runs))
	}
}

// ── Tenant run stats ──────────────────────────────────────────────────────────

func TestTenantRunStats_ScopeIsReadableAgentsUnderVisibility(t *testing.T) {
	f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
	seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "helper"})
	seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")
	seedAgentType(t, repo, AgentType{ID: "at-other", TenantID: "t2", Slug: "elsewhere"})
	repo.statsResult = RunStats{Total: 4}

	got, err := f.run.TenantRunStats(withRunCallerCtx(context.Background(), "t1", "alice"), usagewindow.Period7d, time.Now())
	if err != nil || got.Total != 4 {
		t.Fatalf("stats %+v, %v; want the repository result", got, err)
	}
	slices.Sort(repo.statsScope.AgentTypeIDs)
	want := RunScope{AgentTypeIDs: []string{"at-auto", "at-del"}, Vis: RunVisibility{Subject: "alice"}}
	if !reflect.DeepEqual(repo.statsScope, want) {
		t.Errorf("scope = %+v, want %+v", repo.statsScope, want)
	}
}

func TestHandleTenantRunStats(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		listErr    error
		want       int
	}{
		{"a bad period", "/api/v1/runs/stats?period=1y", nil, http.StatusBadRequest},
		{"the default period", "/api/v1/runs/stats", nil, http.StatusOK},
		{"an agent type lookup failure", "/api/v1/runs/stats?period=24h", errors.New("connection reset"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, repo, _, _ := newTestControllerWithEval(t, auditorEval)
			repo.listAgentTypesErr = tc.listErr
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = req.WithContext(withRunCallerCtx(req.Context(), "t1", "alice"))
			w := httptest.NewRecorder()
			c.handleTenantRunStats(w, req)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
