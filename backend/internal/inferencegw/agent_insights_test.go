package inferencegw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// ── visibility cache ──────────────────────────────────────────────────────────

type countingSource struct {
	calls atomic.Int32
	byTok map[string]agentsclient.Visibility
	err   error
	gate  chan struct{}
}

func (s *countingSource) VisibleAgents(_ context.Context, bearer string) (agentsclient.Visibility, error) {
	s.calls.Add(1)
	if s.gate != nil {
		<-s.gate
	}
	if s.err != nil {
		return agentsclient.Visibility{}, s.err
	}
	return s.byTok[bearer], nil
}

func TestVisibilityCache_ReusesAnswerUntilTTL(t *testing.T) {
	src := &countingSource{byTok: map[string]agentsclient.Visibility{"tok": {AllRows: []string{"coder"}}}}
	c := newVisibilityCache(src, 5*time.Second)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	for range 3 {
		v, err := c.Visible(context.Background(), "t1", "tok")
		if err != nil || !slices.Equal(v.AllRows, []string{"coder"}) {
			t.Fatalf("Visible = %+v, %v", v, err)
		}
	}
	if got := src.calls.Load(); got != 1 {
		t.Fatalf("calls inside the TTL = %d, want 1", got)
	}

	now = now.Add(5 * time.Second)
	if _, err := c.Visible(context.Background(), "t1", "tok"); err != nil {
		t.Fatal(err)
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("calls after the TTL = %d, want 2", got)
	}
}

// A revoked grant must show up as soon as the entry expires, and never leak to
// another token in the meantime.
func TestVisibilityCache_KeysOnTenantAndToken(t *testing.T) {
	src := &countingSource{byTok: map[string]agentsclient.Visibility{
		"alice": {AllRows: []string{"coder"}, OwnRows: []string{"notes"}},
		"bob":   {AllRows: []string{"coder"}},
	}}
	c := newVisibilityCache(src, time.Minute)

	a, _ := c.Visible(context.Background(), "t1", "alice")
	b, _ := c.Visible(context.Background(), "t1", "bob")
	if len(a.OwnRows) != 1 || len(b.OwnRows) != 0 {
		t.Fatalf("alice own %v, bob own %v: one token's answer served to another", a.OwnRows, b.OwnRows)
	}

	if _, err := c.Visible(context.Background(), "t2", "alice"); err != nil {
		t.Fatal(err)
	}
	if got := src.calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (one per tenant and token)", got)
	}
}

func TestVisibilityCache_DoesNotCacheErrors(t *testing.T) {
	boom := errors.New("agents down")
	src := &countingSource{err: boom}
	c := newVisibilityCache(src, time.Minute)

	for range 2 {
		if _, err := c.Visible(context.Background(), "t1", "tok"); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2: a failure must not be reused", got)
	}
}

func TestVisibilityCache_SharesOneInFlightCall(t *testing.T) {
	src := &countingSource{byTok: map[string]agentsclient.Visibility{"tok": {AllRows: []string{"coder"}}}, gate: make(chan struct{})}
	c := newVisibilityCache(src, time.Minute)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			_, _ = c.Visible(context.Background(), "t1", "tok")
		})
	}
	// Wait for the first call to reach the source, then release it.
	for src.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(src.gate)
	wg.Wait()
	if got := src.calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 for concurrent askers", got)
	}
}

func TestVisibilityCache_BoundsEntries(t *testing.T) {
	src := &countingSource{byTok: map[string]agentsclient.Visibility{}}
	c := newVisibilityCache(src, time.Hour)
	for i := range maxVisibilityEntries + 10 {
		if _, err := c.Visible(context.Background(), "t1", "tok-"+string(rune('a'+i%26))+time.Duration(i).String()); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.entries) > maxVisibilityEntries {
		t.Errorf("entries = %d, want at most %d", len(c.entries), maxVisibilityEntries)
	}
}

// ── service ───────────────────────────────────────────────────────────────────

type fakeAgentUsageStore struct {
	mu      sync.Mutex
	scopes  []AgentUsageScope
	usage   []AgentTypeUsage
	models  []ModelSpend
	buckets SpendBuckets
	budgets []AgentBudgetSpend
	err     error
	queries atomic.Int32
}

func (f *fakeAgentUsageStore) note(scope AgentUsageScope) {
	f.queries.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes = append(f.scopes, scope)
}

func (f *fakeAgentUsageStore) AgentUsage(_ context.Context, s AgentUsageScope, _ usagewindow.Window) ([]AgentTypeUsage, error) {
	f.note(s)
	return f.usage, f.err
}

func (f *fakeAgentUsageStore) AgentSpendByModel(_ context.Context, s AgentUsageScope, _ usagewindow.Window) ([]ModelSpend, error) {
	f.note(s)
	return f.models, f.err
}

func (f *fakeAgentUsageStore) AgentSpendBuckets(_ context.Context, s AgentUsageScope, _ usagewindow.Window) (SpendBuckets, error) {
	f.note(s)
	return f.buckets, f.err
}

func (f *fakeAgentUsageStore) AgentBudgetSpend(_ context.Context, tenantID string, slugs []string, _, _ time.Time) ([]AgentBudgetSpend, error) {
	f.queries.Add(1)
	f.mu.Lock()
	f.scopes = append(f.scopes, AgentUsageScope{TenantID: tenantID, AllRows: slugs})
	f.mu.Unlock()
	return f.budgets, f.err
}

func newInsightsService(store agentUsageStore, vis agentsclient.Visibility, visErr error) *AgentInsightsService {
	src := &countingSource{byTok: map[string]agentsclient.Visibility{"tok": vis}, err: visErr}
	s := NewAgentInsightsService(store, src)
	s.now = func() time.Time { return time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC) } // a Saturday
	return s
}

func TestAgentInsights_ScopesQueriesToVisibleAgents(t *testing.T) {
	store := &fakeAgentUsageStore{
		usage:   []AgentTypeUsage{{AgentSlug: "coder", EstimatedCostUSD: 1.5}, {AgentSlug: "notes", EstimatedCostUSD: 0.5}},
		models:  []ModelSpend{{ModelID: "m1", CostUSD: 2}},
		buckets: SpendBuckets{PreviousUSD: 1, Buckets: []float64{0, 0, 0, 0, 0, 1, 1, 0}},
	}
	svc := newInsightsService(store, agentsclient.Visibility{AllRows: []string{"coder"}, OwnRows: []string{"notes"}}, nil)

	out, err := svc.Insights(context.Background(), "t1", "alice", "tok", usagewindow.Period7d)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if store.queries.Load() != 3 {
		t.Errorf("queries = %d, want 3 (usage, models, buckets)", store.queries.Load())
	}
	for _, s := range store.scopes {
		if s.TenantID != "t1" || s.Subject != "alice" || !slices.Equal(s.AllRows, []string{"coder"}) || !slices.Equal(s.OwnRows, []string{"notes"}) {
			t.Errorf("scope = %+v", s)
		}
	}
	if out.TotalCostUSD != 2.0 || out.PreviousTotalUSD != 1 {
		t.Errorf("total %v previous %v", out.TotalCostUSD, out.PreviousTotalUSD)
	}
	// 14:00 on 3 Oct: the trailing 7 days start 26 Sep 14:00, so the origin is
	// 26 Sep 00:00 and the buckets run to the one holding now.
	if len(out.Trend) != 8 || out.Trend[5].CostUSD != 1 || out.BucketSeconds != 86400 {
		t.Errorf("trend = %+v bucket %d", out.Trend, out.BucketSeconds)
	}
	if want := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC); !out.Trend[0].Start.Equal(want) || !out.End.Equal(out.Start.AddDate(0, 0, 7)) {
		t.Errorf("window = %s to %s, first bucket %s", out.Start, out.End, out.Trend[0].Start)
	}
}

func TestAgentInsights_NothingVisibleRunsNoQuery(t *testing.T) {
	store := &fakeAgentUsageStore{}
	svc := newInsightsService(store, agentsclient.Visibility{}, nil)

	out, err := svc.Insights(context.Background(), "t1", "alice", "tok", usagewindow.Period24h)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if store.queries.Load() != 0 {
		t.Errorf("queries = %d, want 0", store.queries.Load())
	}
	if out.TotalCostUSD != 0 || len(out.Usage) != 0 || len(out.Models) != 0 || len(out.Trend) != 9 {
		t.Errorf("out = %+v", out)
	}
	raw, _ := json.Marshal(out)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	if generic["usage"] == nil || generic["models"] == nil {
		t.Errorf("usage and models must encode as arrays, got %s", raw)
	}
}

func TestAgentInsights_VisibilityFailureFailsClosed(t *testing.T) {
	store := &fakeAgentUsageStore{usage: []AgentTypeUsage{{AgentSlug: "coder", EstimatedCostUSD: 9}}}
	svc := newInsightsService(store, agentsclient.Visibility{}, errors.New("agents down"))

	if _, err := svc.Insights(context.Background(), "t1", "alice", "tok", usagewindow.Period7d); !errors.Is(err, ErrAgentVisibilityUnavailable) {
		t.Errorf("Insights err = %v, want ErrAgentVisibilityUnavailable", err)
	}
	if _, err := svc.Budgets(context.Background(), "t1", "alice", "tok"); !errors.Is(err, ErrAgentVisibilityUnavailable) {
		t.Errorf("Budgets err = %v, want ErrAgentVisibilityUnavailable", err)
	}
	if store.queries.Load() != 0 {
		t.Errorf("queries = %d: usage must not be read without a visibility answer", store.queries.Load())
	}
}

func TestAgentInsights_StoreErrorSurfaces(t *testing.T) {
	boom := errors.New("db down")
	svc := newInsightsService(&fakeAgentUsageStore{err: boom}, agentsclient.Visibility{AllRows: []string{"coder"}}, nil)
	if _, err := svc.Insights(context.Background(), "t1", "alice", "tok", usagewindow.Period7d); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

func TestAgentBudgets_LeavesOutOwnRowAgents(t *testing.T) {
	store := &fakeAgentUsageStore{budgets: []AgentBudgetSpend{
		{AgentSlug: "triage", WeekToDateUSD: 1, MonthToDateUSD: 4},
		{AgentSlug: "coder", WeekToDateUSD: 2, MonthToDateUSD: 5},
	}}
	svc := newInsightsService(store, agentsclient.Visibility{AllRows: []string{"coder", "triage"}, OwnRows: []string{"notes"}}, nil)

	out, err := svc.Budgets(context.Background(), "t1", "alice", "tok")
	if err != nil {
		t.Fatalf("Budgets: %v", err)
	}
	if len(store.scopes) != 1 || !slices.Equal(store.scopes[0].AllRows, []string{"coder", "triage"}) {
		t.Errorf("queried slugs = %+v, want the AllRows agents only", store.scopes)
	}
	if out.Agents[0].AgentSlug != "coder" || out.Agents[1].AgentSlug != "triage" {
		t.Errorf("agents = %+v, want sorted by slug", out.Agents)
	}
	// 3 Oct 2026 is a Saturday: the ISO week began Monday 28 Sep, the month on 1 Oct.
	if want := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC); !out.WeekStart.Equal(want) {
		t.Errorf("week start = %s, want %s", out.WeekStart, want)
	}
	if want := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC); !out.MonthStart.Equal(want) {
		t.Errorf("month start = %s, want %s", out.MonthStart, want)
	}
}

func TestAgentBudgets_OnlyOwnRowsRunsNoQuery(t *testing.T) {
	store := &fakeAgentUsageStore{}
	svc := newInsightsService(store, agentsclient.Visibility{OwnRows: []string{"notes"}}, nil)
	out, err := svc.Budgets(context.Background(), "t1", "alice", "tok")
	if err != nil || len(out.Agents) != 0 || store.queries.Load() != 0 {
		t.Errorf("out %+v err %v queries %d", out, err, store.queries.Load())
	}
}

// ── scope clause ──────────────────────────────────────────────────────────────

func TestVisibilityClause(t *testing.T) {
	clause, args := AgentUsageScope{AllRows: []string{"a"}}.visibilityClause(4)
	if clause != "agent_slug = ANY($4)" || len(args) != 1 {
		t.Errorf("all rows only: %q %v", clause, args)
	}
	clause, args = AgentUsageScope{AllRows: []string{"a"}, OwnRows: []string{"b"}, Subject: "alice"}.visibilityClause(6)
	want := "(agent_slug = ANY($6) OR (agent_slug = ANY($7) AND delegator_id = $8))"
	if clause != want || len(args) != 3 || args[2] != "alice" {
		t.Errorf("with own rows: %q %v", clause, args)
	}
	_, args = AgentUsageScope{OwnRows: []string{"b"}, Subject: "alice"}.visibilityClause(4)
	if all, ok := args[0].([]string); !ok || all == nil {
		t.Errorf("a nil AllRows must encode as an empty array, got %#v", args[0])
	}
}

// ── handlers ──────────────────────────────────────────────────────────────────

func agentInsightsRequest(path string, claims *token.PlatformClaims) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer tok")
	if claims != nil {
		req = req.WithContext(token.WithClaims(req.Context(), claims))
	}
	return req
}

func TestHandleAgentInsights(t *testing.T) {
	store := &fakeAgentUsageStore{usage: []AgentTypeUsage{{AgentSlug: "coder", EstimatedCostUSD: 1}}}
	svc := newInsightsService(store, agentsclient.Visibility{AllRows: []string{"coder"}}, nil)
	claims := humanClaimsForHandler("t1", "alice")
	log := logging.NewLoggerWithOutput(io.Discard)

	t.Run("returns the rollup", func(t *testing.T) {
		c := (&Controller{logger: log}).WithAgentInsights(svc)
		w := httptest.NewRecorder()
		c.handleAgentInsights(w, agentInsightsRequest("/usage/agent-insights?period=30d", claims))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", w.Code, w.Body)
		}
		var out AgentInsights
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil || out.Period != "30d" || len(out.Trend) != 11 {
			t.Errorf("out = %+v, %v", out, err)
		}
	})

	t.Run("defaults to 7d", func(t *testing.T) {
		c := (&Controller{logger: log}).WithAgentInsights(svc)
		w := httptest.NewRecorder()
		c.handleAgentInsights(w, agentInsightsRequest("/usage/agent-insights", claims))
		var out AgentInsights
		_ = json.NewDecoder(w.Body).Decode(&out)
		if w.Code != http.StatusOK || out.Period != "7d" {
			t.Errorf("status %d period %q", w.Code, out.Period)
		}
	})

	t.Run("rejects an unknown period", func(t *testing.T) {
		c := (&Controller{logger: log}).WithAgentInsights(svc)
		w := httptest.NewRecorder()
		c.handleAgentInsights(w, agentInsightsRequest("/usage/agent-insights?period=1y", claims))
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("401 without claims", func(t *testing.T) {
		c := (&Controller{logger: log}).WithAgentInsights(svc)
		w := httptest.NewRecorder()
		c.handleAgentInsights(w, agentInsightsRequest("/usage/agent-insights", nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("503 when the service is not wired", func(t *testing.T) {
		c := &Controller{logger: log}
		w := httptest.NewRecorder()
		c.handleAgentInsights(w, agentInsightsRequest("/usage/agent-insights", claims))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
	})

	t.Run("503 when visibility is unknown", func(t *testing.T) {
		down := newInsightsService(store, agentsclient.Visibility{}, errors.New("agents down"))
		c := (&Controller{logger: log}).WithAgentInsights(down)
		w := httptest.NewRecorder()
		c.handleAgentInsights(w, agentInsightsRequest("/usage/agent-insights", claims))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
	})

	t.Run("budgets", func(t *testing.T) {
		c := (&Controller{logger: log}).WithAgentInsights(svc)
		w := httptest.NewRecorder()
		c.handleAgentBudgets(w, agentInsightsRequest("/usage/agent-budgets", claims))
		if w.Code != http.StatusOK {
			t.Errorf("status = %d: %s", w.Code, w.Body)
		}
	})
}

func TestAgentUsage_SameVisibilityAsInsights(t *testing.T) {
	store := &fakeAgentUsageStore{usage: []AgentTypeUsage{{AgentSlug: "notes", TotalTokens: 5, EstimatedCostUSD: 0.5}}}
	svc := newInsightsService(store, agentsclient.Visibility{AllRows: []string{"coder"}, OwnRows: []string{"notes"}}, nil)

	got, err := svc.AgentUsage(context.Background(), "t1", "alice", "tok", usagewindow.Period7d, "notes")
	if err != nil || got.TotalTokens != 5 {
		t.Fatalf("own-row agent = %+v, %v", got, err)
	}
	if len(store.scopes) != 1 || len(store.scopes[0].AllRows) != 0 || !slices.Equal(store.scopes[0].OwnRows, []string{"notes"}) || store.scopes[0].Subject != "alice" {
		t.Errorf("the query must be narrowed to the one agent under the caller's own-row rule: %+v", store.scopes)
	}

	store.scopes = nil
	got, err = svc.AgentUsage(context.Background(), "t1", "alice", "tok", usagewindow.Period7d, "secret")
	if err != nil || got.AgentSlug != "secret" || got.TotalTokens != 0 || len(store.scopes) != 0 {
		t.Errorf("an agent the caller cannot see = %+v, %v, queries %d; want zero and no query", got, err, len(store.scopes))
	}

	down := newInsightsService(store, agentsclient.Visibility{}, errors.New("agents down"))
	if _, err := down.AgentUsage(context.Background(), "t1", "alice", "tok", usagewindow.Period7d, "coder"); !errors.Is(err, ErrAgentVisibilityUnavailable) {
		t.Errorf("err = %v, want ErrAgentVisibilityUnavailable", err)
	}
}

func TestHandleAgentUsage_SlugValidation(t *testing.T) {
	svc := newInsightsService(&fakeAgentUsageStore{}, agentsclient.Visibility{AllRows: []string{"token-probe"}}, nil)
	c := (&Controller{logger: logging.NewLoggerWithOutput(io.Discard)}).WithAgentInsights(svc)
	for slug, want := range map[string]int{
		"token-probe": http.StatusOK,
		"":            http.StatusBadRequest,
		"probe%":      http.StatusBadRequest,
		"a/b":         http.StatusBadRequest,
	} {
		req := agentInsightsRequest("/usage/agent-type/x", humanClaimsForHandler("t1", "u1"))
		req.SetPathValue("slug", slug)
		w := httptest.NewRecorder()
		c.handleAgentUsage(w, req)
		if w.Code != want {
			t.Errorf("slug %q: status %d, want %d", slug, w.Code, want)
		}
	}
	req := agentInsightsRequest("/usage/agent-type/token-probe?period=1y", humanClaimsForHandler("t1", "u1"))
	req.SetPathValue("slug", "token-probe")
	w := httptest.NewRecorder()
	c.handleAgentUsage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad period: status %d, want 400", w.Code)
	}
}
