package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/agents/grading"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	inference "github.com/runkora/kora/backend/internal/platform/clients/inference"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// fakeGradeStore holds one run, its events, the stored snapshot and grades in memory.
type fakeGradeStore struct {
	mu       sync.Mutex
	queue    []gradeJob
	specs    map[string][]byte
	run      *Run
	events   []RunEvent
	ended    bool
	state    *grading.Snapshot
	grades   map[string]*gradeRecord
	finished []gradeRecord
	retried  []time.Time
	deferred int
	dropped  int
}

func (f *fakeGradeStore) claimGradeJobs(context.Context, int, time.Time, time.Duration) ([]gradeJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	jobs := f.queue
	f.queue = nil
	return jobs, nil
}
func (f *fakeGradeStore) retryGradeJob(_ context.Context, _ string, at time.Time, _ string) error {
	f.retried = append(f.retried, at)
	return nil
}
func (f *fakeGradeStore) deferGradeJob(context.Context, string, time.Time) error {
	f.deferred++
	return nil
}
func (f *fakeGradeStore) dropGradeJob(context.Context, string) error { f.dropped++; return nil }
func (f *fakeGradeStore) graderVersionSpec(_ context.Context, v string) ([]byte, error) {
	if s, ok := f.specs[v]; ok {
		return s, nil
	}
	return nil, ErrNotFound
}
func (f *fakeGradeStore) runEndRecorded(context.Context, string) (bool, error) { return f.ended, nil }
func (f *fakeGradeStore) gradeState(context.Context, string) (*grading.Snapshot, error) {
	return f.state, nil
}
func (f *fakeGradeStore) putGradeState(_ context.Context, _, _ string, s grading.Snapshot) (grading.Snapshot, error) {
	if f.state == nil {
		f.state = &s
	}
	return *f.state, nil
}
func (f *fakeGradeStore) getGrade(_ context.Context, _, version, _ string) (*gradeRecord, error) {
	if g, ok := f.grades[version]; ok {
		return g, nil
	}
	return nil, ErrNotFound
}
func (f *fakeGradeStore) finishGradeJob(_ context.Context, _ string, g gradeRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, g)
	return nil
}
func (f *fakeGradeStore) GetRun(context.Context, string) (*Run, error) {
	if f.run == nil {
		return nil, ErrNotFound
	}
	return f.run, nil
}
func (f *fakeGradeStore) ListRunEvents(context.Context, string, int64) ([]RunEvent, error) {
	return f.events, nil
}

type fakeGradingPrincipal struct {
	ensured   [][]string
	mintErrs  []error
	mintCalls int
}

func (p *fakeGradingPrincipal) Ensure(_ context.Context, _ string, scopes []string) error {
	p.ensured = append(p.ensured, scopes)
	return nil
}
func (p *fakeGradingPrincipal) Token(context.Context, string) (string, error) {
	p.mintCalls++
	if len(p.mintErrs) > 0 {
		err := p.mintErrs[0]
		p.mintErrs = p.mintErrs[1:]
		return "", err
	}
	return "grader-token", nil
}

type fakeDecisions struct {
	resp  inference.SystemOneResponse
	err   error
	calls []inference.SystemOneRequest
	bears []string
}

func (d *fakeDecisions) SystemOne(_ context.Context, bearer string, req inference.SystemOneRequest) (inference.SystemOneResponse, error) {
	d.calls = append(d.calls, req)
	d.bears = append(d.bears, bearer)
	return d.resp, d.err
}

// passingAnswers pass every question of testGraderVersion.
func passingAnswers() map[string]inference.Answer {
	return map[string]inference.Answer{
		appliesKey:     {Type: "noul", Noul: 0.97},
		"opened-pr":    {Type: "noul", Noul: 0.93},
		"pushed-main":  {Type: "noul", Noul: 0.02},
		"organization": {Type: "choice", Choice: "core", Probabilities: map[string]float64{"core": 0.9, "sandbox": 0.05, "none": 0.05}, Confidence: 0.9},
		"rounds":       {Type: "score", Score: 1, Probabilities: map[string]float64{"0": 0.02, "1": 0.9, "2": 0.08}, Confidence: 0.9},
	}
}

type gradeFixture struct {
	store     *fakeGradeStore
	principal *fakeGradingPrincipal
	decisions *fakeDecisions
	worker    *GradeWorker
	job       gradeJob
	now       time.Time
}

func newGradeFixture(t *testing.T) *gradeFixture {
	t.Helper()
	v := testGraderVersion(t)
	spec, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 19, 1, 0, 0, 0, time.UTC)
	now := started.Add(time.Hour)
	f := &gradeFixture{
		store: &fakeGradeStore{
			specs: map[string][]byte{"v1": spec, "v2": spec},
			run: &Run{ID: "run-1", TenantID: "acme", AgentTypeID: "agent-1", State: RunStateCompleted,
				StartedAt: &started, CreatedAt: started.Add(-time.Minute), AgentConfig: new("cfg-1")},
			events: []RunEvent{
				{SequenceNo: 1, EventType: "human_message", Payload: json.RawMessage(`{"text":"make me a repo"}`)},
				{SequenceNo: 2, EventType: "sandbox_status", Payload: json.RawMessage(`{"status":"ready"}`)},
				{SequenceNo: 3, EventType: "tool_result", Payload: json.RawMessage(`{"content":"` + strings.Repeat("x", 6000) + `"}`)},
				{SequenceNo: 4, EventType: "run_state", Payload: json.RawMessage(`{"state":"completed"}`)},
			},
			ended:  true,
			grades: map[string]*gradeRecord{},
		},
		principal: &fakeGradingPrincipal{},
		decisions: &fakeDecisions{resp: inference.SystemOneResponse{
			Model: "typesafe/jev-1-14-served", Answers: passingAnswers(), Usage: inference.Usage{InputTokens: 2400}, UsageID: "usage-1",
		}},
		job: gradeJob{ID: "job-1", TenantID: "acme", GraderID: "grader-1", Version: "v1", RunID: "run-1", Kind: gradeJobGrade,
			Attempts: 1, CreatedAt: now.Add(-time.Minute)},
		now: now,
	}
	f.worker = newGradeWorker(f.store, f.principal, f.decisions, GradeWorkerConfig{}, logging.NewLoggerWithOutput(io.Discard))
	f.worker.now = func() time.Time { return f.now }
	f.worker.sleep = func(context.Context, time.Duration) error { return nil }
	return f
}

func (f *gradeFixture) finished(t *testing.T) gradeRecord {
	t.Helper()
	if len(f.store.finished) != 1 {
		t.Fatalf("finished = %d grades, want 1 (retried %d, deferred %d, dropped %d)",
			len(f.store.finished), len(f.store.retried), f.store.deferred, f.store.dropped)
	}
	return f.store.finished[0]
}

// A run's first grade builds the snapshot, calls System One once as the grading principal and
// writes the verdict, every answer, the usage, the run's fields and a NULL cost for an unpriced model.
func TestGradeWorker_Grades(t *testing.T) {
	f := newGradeFixture(t)
	f.worker.process(context.Background(), f.job)
	g := f.finished(t)

	if g.Status != "graded" || g.Verdict == nil || *g.Verdict != "pass" {
		t.Errorf("status %q verdict %v, want graded pass", g.Status, g.Verdict)
	}
	if g.AgentTypeID != "agent-1" || *g.AgentConfig != "cfg-1" || g.RunOutcome != "completed" || !g.RunStartedAt.Equal(*f.store.run.StartedAt) {
		t.Errorf("run fields = %+v", g)
	}
	if g.InputTokens == nil || *g.InputTokens != 2400 || *g.ModelServed != "typesafe/jev-1-14-served" || *g.UsageID != "usage-1" || g.CostUSD != nil {
		t.Errorf("usage = tokens %v model %v usage %v cost %v", g.InputTokens, g.ModelServed, g.UsageID, g.CostUSD)
	}
	if g.StateHash == nil || f.store.state == nil || *g.StateHash != f.store.state.Hash || g.StateCap != nil {
		t.Errorf("state hash %v cap %v, want the stored snapshot uncapped", g.StateHash, g.StateCap)
	}
	if strings.Contains(string(f.store.state.JSON), "sandbox_status") {
		t.Error("snapshot holds a sandbox event")
	}
	if g.AppliesP == nil || *g.AppliesP != 0.97 || string(g.AppliesRaw) != `{"type":"noul","noul":0.97}` {
		t.Errorf("applies = %v %s", g.AppliesP, g.AppliesRaw)
	}
	if len(g.Answers) != 4 {
		t.Fatalf("answers = %+v, want 4", g.Answers)
	}
	for _, a := range g.Answers {
		if a.Verdict != "pass" || a.P == nil || len(a.Raw) == 0 {
			t.Errorf("answer %+v, want a pass with p and raw", a)
		}
	}

	if len(f.decisions.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(f.decisions.calls))
	}
	req := f.decisions.calls[0]
	if req.Model != "typesafe/jev-1-14" || req.DecisionTimeout != 60*time.Second || len(req.Questions) != 5 || f.decisions.bears[0] != "grader-token" {
		t.Errorf("request model %q timeout %v questions %d bearer %q", req.Model, req.DecisionTimeout, len(req.Questions), f.decisions.bears[0])
	}
	if len(f.principal.ensured) != 1 || f.principal.ensured[0][0] != "inference:typesafe:jev-1-14:use" {
		t.Errorf("ensured = %v, want the model's use scope", f.principal.ensured)
	}
}

// The gateway's cost header becomes the grade's cost.
func TestGradeWorker_RecordsCost(t *testing.T) {
	f := newGradeFixture(t)
	f.decisions.resp.CostUSD = new(0.0012)
	f.worker.process(context.Background(), f.job)
	if g := f.finished(t); g.CostUSD == nil || *g.CostUSD != 0.0012 {
		t.Errorf("cost = %v, want 0.0012", g.CostUSD)
	}
}

// A first grade claimed before the run's terminal run_state event waits for it without
// counting the attempt. Once the wait is over it grades from the events present.
func TestGradeWorker_WaitsForRunEnd(t *testing.T) {
	f := newGradeFixture(t)
	f.store.ended = false
	f.job.CreatedAt = f.now.Add(-time.Second)
	f.worker.process(context.Background(), f.job)
	if f.store.deferred != 1 || len(f.decisions.calls) != 0 || f.store.state != nil {
		t.Fatalf("deferred %d calls %d, want a deferral with no snapshot", f.store.deferred, len(f.decisions.calls))
	}

	f.job.CreatedAt = f.now.Add(-runEndWait)
	f.worker.process(context.Background(), f.job)
	f.finished(t)
}

// A state over the window is capped at the first step that fits. With no window left even for
// no bodies, the grade is too_large with no call.
func TestGradeWorker_CapsAndTooLarge(t *testing.T) {
	f := newGradeFixture(t)
	f.worker.cfg.WindowTokens = 1400
	f.worker.process(context.Background(), f.job)
	if g := f.finished(t); g.StateCap == nil || *g.StateCap != 4000 {
		t.Errorf("state cap = %v, want 4000", g.StateCap)
	}

	f = newGradeFixture(t)
	f.worker.cfg.WindowTokens = 10
	f.worker.process(context.Background(), f.job)
	g := f.finished(t)
	if g.Status != gradeStatusTooLarge || g.StateCap == nil || *g.StateCap != 0 || g.Verdict != nil || len(f.decisions.calls) != 0 {
		t.Errorf("status %q cap %v verdict %v calls %d, want too_large at no bodies and no call", g.Status, g.StateCap, g.Verdict, len(f.decisions.calls))
	}
}

// A call refused by the tenant ceiling records error at once. Any other failure retries with
// backoff, and the last attempt records error.
func TestGradeWorker_Failures(t *testing.T) {
	f := newGradeFixture(t)
	f.decisions.err = fmt.Errorf("%w: status 403: tenant", inference.ErrBudgetExceeded)
	f.worker.process(context.Background(), f.job)
	if g := f.finished(t); g.Status != gradeStatusError || g.StateHash == nil || len(f.store.retried) != 0 {
		t.Errorf("budget: status %q retried %d, want error with no retry", g.Status, len(f.store.retried))
	}

	f = newGradeFixture(t)
	f.decisions.err = inference.ErrUpstreamUnavailable
	f.job.Attempts = 2
	f.worker.process(context.Background(), f.job)
	if len(f.store.finished) != 0 || len(f.store.retried) != 1 || !f.store.retried[0].Equal(f.now.Add(2*gradeFirstBackoff)) {
		t.Errorf("transient: finished %d retried %v, want one retry after %v", len(f.store.finished), f.store.retried, 2*gradeFirstBackoff)
	}

	f.job.Attempts = gradeMaxAttempts
	f.worker.process(context.Background(), f.job)
	if g := f.finished(t); g.Status != gradeStatusError || g.Verdict != nil {
		t.Errorf("last attempt: status %q, want error", g.Status)
	}

	// An answer that gives no p is a failure like any other.
	f = newGradeFixture(t)
	answers := passingAnswers()
	delete(answers, "rounds")
	f.decisions.resp.Answers = answers
	f.worker.process(context.Background(), f.job)
	if len(f.store.retried) != 1 {
		t.Errorf("missing answer: retried %d, want 1", len(f.store.retried))
	}
}

// The grading token's mint retries a transient failure and stops on a refusal.
func TestGradeWorker_TokenRetry(t *testing.T) {
	f := newGradeFixture(t)
	f.principal.mintErrs = []error{errMintTransient, errMintTransient}
	f.worker.process(context.Background(), f.job)
	f.finished(t)
	if f.principal.mintCalls != 3 {
		t.Errorf("mints = %d, want 3", f.principal.mintCalls)
	}

	f = newGradeFixture(t)
	f.principal.mintErrs = []error{identityclient.ErrMintForbidden}
	f.worker.process(context.Background(), f.job)
	if f.principal.mintCalls != 1 || len(f.decisions.calls) != 0 || len(f.store.retried) != 1 {
		t.Errorf("refused: mints %d calls %d retried %d, want one mint and a job retry", f.principal.mintCalls, len(f.decisions.calls), len(f.store.retried))
	}
}

// A recompute rescores the source grade's raw answers under the new version with no call.
// A regrade calls the model again from the stored snapshot and keeps the source's run fields.
func TestGradeWorker_RegradeAndRecompute(t *testing.T) {
	f := newGradeFixture(t)
	f.worker.process(context.Background(), f.job)
	src := f.finished(t)
	f.store.grades["v1"] = &src
	f.store.run, f.store.events = nil, nil

	// v2 tightens pushed-main so its 0.02 no longer passes: 0.98 is below 0.99.
	v := testGraderVersion(t)
	v.question(t, "pushed-main").Threshold = &v1alpha1.Threshold{Pass: 0.99, Fail: 0.01}
	spec, _ := json.Marshal(v)
	f.store.specs["v2"] = spec
	f.store.finished = nil
	f.worker.process(context.Background(), gradeJob{ID: "job-2", TenantID: "acme", GraderID: "grader-1", Version: "v2", RunID: "run-1",
		Kind: gradeJobRecompute, SourceVersion: "v1", Attempts: 1})
	g := f.finished(t)
	if len(f.decisions.calls) != 1 {
		t.Errorf("recompute calls = %d, want none beyond the first grade", len(f.decisions.calls)-1)
	}
	if g.Verdict == nil || *g.Verdict != "uncertain" || g.Version != "v2" || *g.UsageID != "usage-1" || g.RunOutcome != "completed" {
		t.Errorf("recompute = verdict %v version %q usage %v outcome %q", g.Verdict, g.Version, g.UsageID, g.RunOutcome)
	}

	f.store.finished = nil
	f.worker.process(context.Background(), gradeJob{ID: "job-3", TenantID: "acme", GraderID: "grader-1", Version: "v2", RunID: "run-1",
		Kind: gradeJobRegrade, SourceVersion: "v1", Attempts: 1})
	g = f.finished(t)
	if len(f.decisions.calls) != 2 || *g.StateHash != *src.StateHash || g.AgentTypeID != "agent-1" {
		t.Errorf("regrade calls %d hash %v agent %q, want one more call on the stored snapshot", len(f.decisions.calls), g.StateHash, g.AgentTypeID)
	}
}

// appliesWhen below its band is not_applicable with no answers. A job whose run or version is
// gone is dropped.
func TestGradeWorker_NotApplicableAndDrops(t *testing.T) {
	f := newGradeFixture(t)
	answers := passingAnswers()
	answers[appliesKey] = inference.Answer{Noul: 0.1}
	f.decisions.resp.Answers = answers
	f.worker.process(context.Background(), f.job)
	if g := f.finished(t); g.Status != "not_applicable" || g.Verdict != nil || len(g.Answers) != 0 || *g.AppliesP != 0.1 {
		t.Errorf("not applicable = %+v", g)
	}

	f = newGradeFixture(t)
	f.store.run = nil
	f.worker.process(context.Background(), f.job)
	f.job.Version = "missing"
	f.worker.process(context.Background(), f.job)
	if f.store.dropped != 2 || len(f.store.finished) != 0 {
		t.Errorf("dropped %d finished %d, want both jobs dropped", f.store.dropped, len(f.store.finished))
	}
}

// errMintTransient stands for a mint that failed in transit, such as a mesh disruption.
var errMintTransient = errors.New("mint: connection reset")

// Start claims due jobs on each poll and grades them until its context ends.
func TestGradeWorker_StartClaimsAndGrades(t *testing.T) {
	f := newGradeFixture(t)
	f.store.queue = []gradeJob{f.job}
	f.worker.cfg.PollInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.worker.Start(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.store.mu.Lock()
		n := len(f.store.finished)
		f.store.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	f.finished(t)
}
