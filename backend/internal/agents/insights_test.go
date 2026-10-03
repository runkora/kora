package agents

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// fakeInsightsStore returns canned refs and records the scope it was asked to count.
type fakeInsightsStore struct {
	refs     []AgentTypeRef
	refsErr  error
	outcomes RunOutcomes
	scope    RunOutcomeScope
	counted  bool
}

func (f *fakeInsightsStore) ListAgentTypeRefs(context.Context, string) ([]AgentTypeRef, error) {
	return f.refs, f.refsErr
}

func (f *fakeInsightsStore) CountRunOutcomes(_ context.Context, scope RunOutcomeScope) (RunOutcomes, error) {
	f.counted = true
	f.scope = scope
	return f.outcomes, nil
}

// readOnlyEvaluator allows read on the listed slugs per resource and nothing else.
type readOnlyEvaluator struct {
	allowAllEvaluator
	allowed map[string][]string // resource -> slugs
}

func (e readOnlyEvaluator) Query(_ context.Context, in policy.QueryInput) ([]string, error) {
	var out []string
	for _, c := range in.Candidates {
		if len(c.PathSegments) == 2 && slices.Contains(e.allowed[c.PathSegments[0]], c.PathSegments[1]) {
			out = append(out, c.ID)
		}
	}
	return out, nil
}

var insightsRefs = []AgentTypeRef{
	{ID: "id-coder", Slug: "coder"},
	{ID: "id-notes", Slug: "notes", Delegated: true},
	{ID: "id-secret", Slug: "secret"},
}

func newInsights(store *fakeInsightsStore, allowed map[string][]string) *InsightsService {
	return NewInsightsService(store, NewAuthz(readOnlyEvaluator{allowed: allowed}), fakeBudgets{}, func(string) string { return "ns" })
}

func TestVisibleAgents_PartitionsDelegatedAgents(t *testing.T) {
	store := &fakeInsightsStore{refs: insightsRefs}
	svc := newInsights(store, map[string][]string{"types": {"coder", "notes"}})

	got, err := svc.VisibleAgents(withRunCallerCtx(context.Background(), "t1", "alice", ""))
	if err != nil {
		t.Fatalf("VisibleAgents: %v", err)
	}
	if !slices.Equal(got.AllRows, []string{"coder"}) || !slices.Equal(got.OwnRows, []string{"notes"}) {
		t.Errorf("visibility = %+v, want all=[coder] own=[notes]", got)
	}
}

func TestVisibleAgents_AdminSeesDelegatedInFull(t *testing.T) {
	store := &fakeInsightsStore{refs: insightsRefs}
	svc := newInsights(store, map[string][]string{"types": {"coder", "notes"}})

	got, err := svc.VisibleAgents(withRunCallerCtx(context.Background(), "t1", "root", rolePlatformAdmin))
	if err != nil {
		t.Fatalf("VisibleAgents: %v", err)
	}
	if !slices.Equal(got.AllRows, []string{"coder", "notes"}) || len(got.OwnRows) != 0 {
		t.Errorf("visibility = %+v, want both agents in full", got)
	}
}

func TestVisibleAgents_NoGrantsSeesNothing(t *testing.T) {
	store := &fakeInsightsStore{refs: insightsRefs}
	svc := newInsights(store, nil)

	got, err := svc.VisibleAgents(withRunCallerCtx(context.Background(), "t1", "alice", ""))
	if err != nil {
		t.Fatalf("VisibleAgents: %v", err)
	}
	if len(got.AllRows) != 0 || len(got.OwnRows) != 0 {
		t.Errorf("visibility = %+v, want empty", got)
	}
}

func TestVisibleAgents_StoreError(t *testing.T) {
	boom := errors.New("db down")
	svc := newInsights(&fakeInsightsStore{refsErr: boom}, nil)
	if _, err := svc.VisibleAgents(withRunCallerCtx(context.Background(), "t1", "alice", "")); !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrapped %v", err, boom)
	}
}

func TestRunOutcomes_ScopesTheCount(t *testing.T) {
	store := &fakeInsightsStore{refs: insightsRefs, outcomes: RunOutcomes{Completed: 4, Failed: 1}}
	// The caller may read runs of coder and notes, not secret.
	svc := newInsights(store, map[string][]string{"runs": {"coder", "notes"}})

	got, err := svc.RunOutcomes(withRunCallerCtx(context.Background(), "t1", "alice", ""), usagewindow.Period30d)
	if err != nil {
		t.Fatalf("RunOutcomes: %v", err)
	}
	if got.Completed != 4 || got.Failed != 1 {
		t.Errorf("outcomes = %+v", got)
	}
	if !slices.Equal(store.scope.AllRows, []string{"id-coder"}) || !slices.Equal(store.scope.OwnRows, []string{"id-notes"}) {
		t.Errorf("scope rows = all %v own %v", store.scope.AllRows, store.scope.OwnRows)
	}
	if store.scope.Subject != "alice" || store.scope.TenantID != "t1" {
		t.Errorf("scope identity = %q in %q", store.scope.Subject, store.scope.TenantID)
	}
	if age := time.Since(store.scope.Since); age < 29*24*time.Hour || age > 30*24*time.Hour {
		t.Errorf("since is %s ago, want a 30d window", age)
	}
}

func TestRunOutcomes_NothingVisibleSkipsTheQuery(t *testing.T) {
	store := &fakeInsightsStore{refs: insightsRefs}
	svc := newInsights(store, nil)

	got, err := svc.RunOutcomes(withRunCallerCtx(context.Background(), "t1", "alice", ""), usagewindow.Period7d)
	if err != nil {
		t.Fatalf("RunOutcomes: %v", err)
	}
	if store.counted || got != (RunOutcomes{}) {
		t.Errorf("counted = %v, outcomes = %+v; want no query and zero counts", store.counted, got)
	}
}

func TestInsightsController_Handlers(t *testing.T) {
	store := &fakeInsightsStore{refs: insightsRefs, outcomes: RunOutcomes{Completed: 2}}
	svc := newInsights(store, map[string][]string{"types": {"coder"}, "runs": {"coder"}})
	c := NewInsightsController(svc, nil, nil, nil, "", logging.NewLoggerWithOutput(io.Discard))

	t.Run("visibility encodes empty lists as arrays", func(t *testing.T) {
		empty := NewInsightsController(newInsights(&fakeInsightsStore{}, nil), nil, nil, nil, "", logging.NewLoggerWithOutput(io.Discard))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-visibility", nil)
		req = req.WithContext(withRunCallerCtx(req.Context(), "t1", "alice", ""))
		rec := httptest.NewRecorder()
		empty.handleVisibility(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != "{\"all_rows\":[],\"own_rows\":[]}\n" {
			t.Errorf("status %d body %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("outcomes rejects a bad period", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/run-outcomes?period=1y", nil)
		req = req.WithContext(withRunCallerCtx(req.Context(), "t1", "alice", ""))
		rec := httptest.NewRecorder()
		c.handleRunOutcomes(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("outcomes defaults to 7d", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/run-outcomes", nil)
		req = req.WithContext(withRunCallerCtx(req.Context(), "t1", "alice", ""))
		rec := httptest.NewRecorder()
		c.handleRunOutcomes(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})
}

// fakeBudgets serves a budget per slug and records the namespace it was asked for.
type fakeBudgets struct {
	bySlug map[string]*AgentBudget
	failOn string
	mu     *sync.Mutex
	ns     *string
}

func (f fakeBudgets) GetAgentBudget(_ context.Context, at AgentType, namespace string) (*AgentBudget, error) {
	if f.ns != nil {
		f.mu.Lock()
		*f.ns = namespace
		f.mu.Unlock()
	}
	if at.Slug == f.failOn {
		return nil, errors.New("policy service down")
	}
	return f.bySlug[at.Slug], nil
}

func TestBudgetLimits(t *testing.T) {
	store := &fakeInsightsStore{refs: insightsRefs}
	var ns string
	budgets := fakeBudgets{mu: &sync.Mutex{}, ns: &ns, bySlug: map[string]*AgentBudget{
		"coder":  {LimitUSD: 50},
		"notes":  {LimitUSD: 10, ResetCadence: "weekly"},
		"secret": {LimitUSD: 99, ResetCadence: "weekly"},
	}}
	svc := NewInsightsService(store, NewAuthz(readOnlyEvaluator{allowed: map[string][]string{"types": {"coder", "notes"}}}), budgets, func(tenant string) string { return "ns-" + tenant })

	got, err := svc.BudgetLimits(withRunCallerCtx(context.Background(), "t1", "alice", ""))
	if err != nil {
		t.Fatalf("BudgetLimits: %v", err)
	}
	// notes is delegated and so seen only for alice's own runs: no ceiling shown.
	want := []AgentBudgetLimit{{AgentSlug: "coder", LimitUSD: 50, ResetCadence: "monthly"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("limits = %+v, want %+v", got, want)
	}
	if ns != "ns-t1" {
		t.Errorf("namespace = %q, want ns-t1", ns)
	}

	admin, err := svc.BudgetLimits(withRunCallerCtx(context.Background(), "t1", "root", rolePlatformAdmin))
	if err != nil || len(admin) != 2 || admin[1].ResetCadence != "weekly" {
		t.Errorf("admin limits = %+v, %v; want coder and weekly notes", admin, err)
	}
}

func TestBudgetLimits_LookupFailureFailsTheCall(t *testing.T) {
	store := &fakeInsightsStore{refs: insightsRefs}
	svc := NewInsightsService(store, NewAuthz(readOnlyEvaluator{allowed: map[string][]string{"types": {"coder"}}}), fakeBudgets{failOn: "coder"}, func(string) string { return "ns" })
	if _, err := svc.BudgetLimits(withRunCallerCtx(context.Background(), "t1", "alice", "")); err == nil {
		t.Error("a failed lookup must fail the call: a row without its limit would read as unbudgeted")
	}
}
