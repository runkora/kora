package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/agents/grading"
	inference "github.com/runkora/kora/backend/internal/platform/clients/inference"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// gradingPG is a tenant with one agent and a Grader on it, for the grading pipeline's Postgres tests.
type gradingPG struct {
	r      *PostgresRepository
	tenant string
	agent  AgentType
	grader *Grader
}

func newGradingPG(t *testing.T) *gradingPG {
	t.Helper()
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM grades WHERE tenant_id = $1`, tenant)
		_, _ = r.pool.Exec(ctx, `DELETE FROM grade_states WHERE tenant_id = $1`, tenant)
		_, _ = r.pool.Exec(ctx, `DELETE FROM grade_jobs WHERE tenant_id = $1`, tenant)
		_, _ = r.pool.Exec(ctx, `DELETE FROM graders WHERE tenant_id = $1`, tenant)
	})
	p := &gradingPG{r: r, tenant: tenant, agent: seedRunAgentType(t, r, tenant, IdentityModeAutonomous)}
	p.grader = p.putGrader(t, "new-repository", testGraderVersion(t))
	return p
}

// putGrader stores v as the named Grader's current version.
func (p *gradingPG) putGrader(t *testing.T, name string, v graderVersion) *Grader {
	t.Helper()
	spec, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	// The version is unique per test so the shared grader_versions table never mixes specs.
	version := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = p.r.pool.Exec(context.Background(), `DELETE FROM grader_versions WHERE version = $1`, version)
	})
	g, err := p.r.PutGrader(context.Background(), Grader{TenantID: p.tenant, Name: name, AgentTypeID: p.agent.ID, Description: "d",
		CurrentVersion: version, ResourceUID: "uid-" + name, ResourceGeneration: 1}, spec)
	if err != nil {
		t.Fatalf("PutGrader: %v", err)
	}
	return g
}

// run creates a run of the agent in state.
func (p *gradingPG) run(t *testing.T, state RunState) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	if _, err := p.r.CreateRun(ctx, Run{ID: id, TenantID: p.tenant, AgentTypeID: p.agent.ID, CreatedBy: "user-1"}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := p.r.pool.Exec(ctx, `UPDATE runs SET state = $2, started_at = now() WHERE id = $1`, id, string(state)); err != nil {
		t.Fatalf("set state: %v", err)
	}
	return id
}

// jobs returns the run's grade jobs as grader:kind.
func (p *gradingPG) jobs(t *testing.T, runID string) []string {
	t.Helper()
	rows, err := p.r.pool.Query(context.Background(), `SELECT grader_id || ':' || version || ':' || kind FROM grade_jobs WHERE run_id = $1 ORDER BY grader_id`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// Every run-end path queues one grade job per Grader of the run's agent in its transaction.
// A state change that does not end the run queues none. A terminate after the end adds none.
func TestPostgres_GradeJobsOnRunEnd(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	want := []string{p.grader.ID + ":" + p.grader.CurrentVersion + ":grade"}

	ends := map[string]func(id string) error{
		"completed":        func(id string) error { return p.r.SetRunState(ctx, id, RunStateActive, RunStateCompleted) },
		"budget_exhausted": func(id string) error { return p.r.SetRunState(ctx, id, RunStateActive, RunStateBudgetExhausted) },
		"failed":           func(id string) error { return p.r.SetRunState(ctx, id, RunStateActive, RunStateFailed) },
		"mark failed":      func(id string) error { return p.r.MarkRunFailed(ctx, id) },
		"terminated":       func(id string) error { _, err := p.r.TerminateRun(ctx, id); return err },
		"reaped": func(id string) error {
			_, err := p.r.pool.Exec(ctx, `UPDATE runs SET updated_at = now() - interval '2 hours' WHERE id = $1`, id)
			if err == nil {
				_, err = p.r.ReapStaleRuns(ctx, time.Hour)
			}
			return err
		},
	}
	for name, end := range ends {
		id := p.run(t, RunStateActive)
		if err := end(id); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := p.jobs(t, id); len(got) != 1 || got[0] != want[0] {
			t.Errorf("%s: jobs = %v, want %v", name, got, want)
		}
	}

	// Deleting the agent cancels its runs through endRuns.
	cancelled := p.run(t, RunStateIdle)
	if _, err := p.r.CancelRunsByAgentType(ctx, p.agent.ID); err != nil {
		t.Fatalf("CancelRunsByAgentType: %v", err)
	}
	if got := p.jobs(t, cancelled); len(got) != 1 {
		t.Errorf("cancelled: jobs = %v, want 1", got)
	}

	live := p.run(t, RunStateStarting)
	if err := p.r.SetRunState(ctx, live, RunStateStarting, RunStateActive); err != nil {
		t.Fatalf("SetRunState: %v", err)
	}
	if err := p.r.SetRunState(ctx, live, RunStateActive, RunStateIdle); err != nil {
		t.Fatalf("SetRunState: %v", err)
	}
	if got := p.jobs(t, live); len(got) != 0 {
		t.Errorf("live run: jobs = %v, want none", got)
	}

	// A second Grader on the agent gets its own job. The terminate after the end adds none.
	second := p.putGrader(t, "second", testGraderVersion(t))
	ended := p.run(t, RunStateActive)
	if err := p.r.SetRunState(ctx, ended, RunStateActive, RunStateCompleted); err != nil {
		t.Fatalf("SetRunState: %v", err)
	}
	if _, err := p.r.TerminateRun(ctx, ended); err != nil {
		t.Fatalf("TerminateRun: %v", err)
	}
	if got := p.jobs(t, ended); len(got) != 2 {
		t.Errorf("two graders: jobs = %v, want one each for %s and %s", got, p.grader.ID, second.ID)
	}
	// Queuing a run's end again keeps one job per Grader version.
	if err := p.r.enqueueGradeJobs(ctx, ended); err != nil {
		t.Fatalf("enqueueGradeJobs: %v", err)
	}
	if got := p.jobs(t, ended); len(got) != 2 {
		t.Errorf("enqueued twice: jobs = %v, want 2", got)
	}
}

// A claim takes each due job once, counts the attempt and holds it for the lease. A retried job
// is due at its backoff. A deferred one gets its attempt back.
func TestPostgres_ClaimGradeJobs(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	a, b := p.run(t, RunStateActive), p.run(t, RunStateActive)
	for _, id := range []string{a, b} {
		if err := p.r.SetRunState(ctx, id, RunStateActive, RunStateCompleted); err != nil {
			t.Fatal(err)
		}
	}
	// Claim far ahead so jobs other tests left due do not crowd ours out of the limit.
	now := time.Now().Add(time.Second)
	mine := func(jobs []gradeJob) []gradeJob {
		var out []gradeJob
		for _, j := range jobs {
			if j.TenantID == p.tenant {
				out = append(out, j)
			}
		}
		return out
	}
	first, err := p.r.claimGradeJobs(ctx, 100, now, gradeLease)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	got := mine(first)
	if len(got) != 2 || got[0].Attempts != 1 || got[0].Kind != gradeJobGrade || got[0].Version != p.grader.CurrentVersion {
		t.Fatalf("claimed = %+v, want both jobs on their first attempt", got)
	}
	again, err := p.r.claimGradeJobs(ctx, 100, now.Add(time.Minute), gradeLease)
	if err != nil || len(mine(again)) != 0 {
		t.Fatalf("second claim = %+v, %v, want none inside the lease", mine(again), err)
	}

	if err := p.r.retryGradeJob(ctx, got[0].ID, now.Add(time.Minute), "upstream unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := p.r.deferGradeJob(ctx, got[1].ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	later, err := p.r.claimGradeJobs(ctx, 100, now.Add(2*time.Minute), gradeLease)
	if err != nil {
		t.Fatal(err)
	}
	attempts := map[string]int{}
	for _, j := range mine(later) {
		attempts[j.ID] = j.Attempts
	}
	if attempts[got[0].ID] != 2 || attempts[got[1].ID] != 1 {
		t.Errorf("attempts = %v, want 2 for the retried job and 1 for the deferred one", attempts)
	}
	var lastError string
	if err := p.r.pool.QueryRow(ctx, `SELECT last_error FROM grade_jobs WHERE id = $1`, got[0].ID).Scan(&lastError); err != nil || lastError != "upstream unavailable" {
		t.Errorf("last_error = %q, %v", lastError, err)
	}
}

// A grade is one row per Grader version and run. Writing it again replaces it and its
// answers. It and its snapshot survive the Grader's and the run's deletes.
func TestPostgres_GradesUniqueAndKept(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	runID := p.run(t, RunStateActive)
	if err := p.r.SetRunState(ctx, runID, RunStateActive, RunStateCompleted); err != nil {
		t.Fatal(err)
	}
	snap, err := grading.BuildSnapshot([]grading.Event{{Seq: 1, Type: "human_message", Payload: json.RawMessage(`{"text":"hi"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := p.r.putGradeState(ctx, p.tenant, runID, snap)
	if err != nil || stored.Hash != snap.Hash {
		t.Fatalf("putGradeState = %+v, %v", stored, err)
	}
	// A second snapshot of the same run keeps the first.
	if again, _ := p.r.putGradeState(ctx, p.tenant, runID, grading.Snapshot{JSON: []byte(`[]`), Hash: "other"}); again.Hash != snap.Hash {
		t.Errorf("second snapshot hash = %q, want the first kept", again.Hash)
	}

	jobs, _ := p.r.claimGradeJobs(ctx, 100, time.Now().Add(time.Second), gradeLease)
	var job gradeJob
	for _, j := range jobs {
		if j.RunID == runID {
			job = j
		}
	}
	g := gradeRecord{TenantID: p.tenant, GraderID: p.grader.ID, Version: p.grader.CurrentVersion, RunID: runID, AgentTypeID: p.agent.ID,
		RunOutcome: "completed", RunStartedAt: time.Now().UTC(), StateHash: &snap.Hash, Status: "graded", Verdict: new("fail"),
		AppliesRaw: json.RawMessage(`{"type":"noul","noul":0.9}`), AppliesP: new(0.9),
		Answers: []gradeAnswer{
			{Question: "opened-pr", Raw: json.RawMessage(`{"type":"noul","noul":0.1}`), P: new(0.1), Verdict: "fail"},
			{Question: "rounds", Raw: json.RawMessage(`{"type":"score","score":0}`), Verdict: "skipped"},
		}}
	if err := p.r.finishGradeJob(ctx, job.ID, g); err != nil {
		t.Fatalf("finishGradeJob: %v", err)
	}
	// A finished job is gone, so a second worker holding it writes nothing.
	g.Verdict, g.Answers = new("pass"), g.Answers[:1]
	g.Answers[0].Verdict = "pass"
	if err := p.r.finishGradeJob(ctx, job.ID, g); !errors.Is(err, errGradeJobGone) {
		t.Fatalf("finishing a finished job = %v, want errGradeJobGone", err)
	}
	if got, _ := p.r.getGrade(ctx, p.grader.ID, p.grader.CurrentVersion, runID); *got.Verdict != "fail" {
		t.Errorf("verdict after a stale finish = %q, want the first write kept", *got.Verdict)
	}
	// A regrade of the same run and version replaces the grade and its answers.
	if err := p.r.finishGradeJob(ctx, p.queueJob(t, runID, p.grader.CurrentVersion), g); err != nil {
		t.Fatalf("finishGradeJob again: %v", err)
	}
	// A job moved to another version writes nothing for its old one.
	moved := p.queueJob(t, uuid.NewString(), "other-version")
	stale := g
	stale.RunID = "moved-run"
	if err := p.r.finishGradeJob(ctx, moved, stale); !errors.Is(err, errGradeJobGone) {
		t.Errorf("finishing a moved job = %v, want errGradeJobGone", err)
	}
	got, err := p.r.getGrade(ctx, p.grader.ID, p.grader.CurrentVersion, runID)
	if err != nil {
		t.Fatalf("getGrade: %v", err)
	}
	if *got.Verdict != "pass" || len(got.Answers) != 1 || got.Answers[0].Verdict != "pass" || got.CostUSD != nil || *got.AppliesP != 0.9 {
		t.Errorf("grade = %+v, want the second write with one answer and no cost", got)
	}
	if left := p.jobs(t, runID); len(left) != 0 {
		t.Errorf("jobs after finish = %v, want none", left)
	}

	if err := p.r.DeleteGrader(ctx, p.tenant, p.grader.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := p.r.pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	var grades, states int
	_ = p.r.pool.QueryRow(ctx, `SELECT count(*) FROM grades WHERE run_id = $1`, runID).Scan(&grades)
	_ = p.r.pool.QueryRow(ctx, `SELECT count(*) FROM grade_states WHERE run_id = $1`, runID).Scan(&states)
	if grades != 1 || states != 1 {
		t.Errorf("after deletes: grades %d states %d, want both kept", grades, states)
	}
}

// queueJob inserts a first-grade job for the run at version and returns its id.
func (p *gradingPG) queueJob(t *testing.T, runID, version string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := p.r.pool.Exec(context.Background(), `INSERT INTO grade_jobs (id, tenant_id, grader_id, version, run_id, kind, run_after)
		VALUES ($1, $2, $3, $4, $5, 'grade', now() + interval '1 hour')`, id, p.tenant, p.grader.ID, version, runID); err != nil {
		t.Fatalf("queue job: %v", err)
	}
	return id
}

// seedGrades writes n graded grades of the Grader's current version, one run each, an hour apart.
func (p *gradingPG) seedGrades(t *testing.T, n int) []string {
	t.Helper()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Duration(n) * time.Hour)
	ids := make([]string, n)
	for i := range n {
		ids[i] = uuid.NewString()
		g := gradeRecord{TenantID: p.tenant, GraderID: p.grader.ID, Version: p.grader.CurrentVersion, RunID: ids[i], AgentTypeID: p.agent.ID,
			RunOutcome: "completed", RunStartedAt: base.Add(time.Duration(i) * time.Hour), Status: "graded", Verdict: new("pass")}
		if err := p.r.finishGradeJob(ctx, p.queueJob(t, g.RunID, g.Version), g); err != nil {
			t.Fatalf("seed grade: %v", err)
		}
	}
	return ids
}

// A new version queues the previous version's latest 30 graded runs: a recompute for a
// threshold-only change and a regrade otherwise. A first grade still queued moves to the new version.
func TestPostgres_RegradeSelection(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	runs := p.seedGrades(t, 35)
	queued := p.run(t, RunStateActive)
	if err := p.r.SetRunState(ctx, queued, RunStateActive, RunStateCompleted); err != nil {
		t.Fatal(err)
	}
	v1 := p.grader.CurrentVersion

	v := testGraderVersion(t)
	v.Threshold = v1alpha1.Threshold{Pass: 0.8, Fail: 0.2}
	g := p.putGrader(t, p.grader.Name, v)
	count := func(version, kind string) int {
		var n int
		if err := p.r.pool.QueryRow(ctx, `SELECT count(*) FROM grade_jobs WHERE grader_id = $1 AND version = $2 AND kind = $3`,
			g.ID, version, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(g.CurrentVersion, gradeJobRecompute); n != 30 {
		t.Errorf("threshold change: %d recompute jobs, want 30", n)
	}
	var oldest int
	_ = p.r.pool.QueryRow(ctx, `SELECT count(*) FROM grade_jobs WHERE grader_id = $1 AND run_id = ANY($2)`, g.ID, runs[:5]).Scan(&oldest)
	if oldest != 0 {
		t.Errorf("%d jobs for the 5 oldest runs, want none", oldest)
	}
	var src string
	_ = p.r.pool.QueryRow(ctx, `SELECT DISTINCT source_version FROM grade_jobs WHERE grader_id = $1 AND kind = 'recompute'`, g.ID).Scan(&src)
	if src != v1 {
		t.Errorf("source version = %q, want %q", src, v1)
	}
	if got := p.jobs(t, queued); len(got) != 1 || got[0] != g.ID+":"+g.CurrentVersion+":grade" {
		t.Errorf("queued first grade = %v, want it moved to the new version", got)
	}

	// A question change queues regrades of the version's own grades. The 30 recomputes still
	// queued move to it and become regrades, since their source asked other questions.
	p.grader = g
	p.seedGrades(t, 3)
	v.question(t, "opened-pr").Question = "Did the agent open a new pull request through its own tool calls?"
	g = p.putGrader(t, p.grader.Name, v)
	if n := count(g.CurrentVersion, gradeJobRegrade); n != 33 {
		t.Errorf("question change: %d regrade jobs, want 3 new and 30 moved", n)
	}

	// A threshold-only change before those ran moves them on. They stay regrades and the
	// superseded version keeps none.
	queuedVersion := g.CurrentVersion
	v.Threshold = v1alpha1.Threshold{Pass: 0.75, Fail: 0.25}
	g = p.putGrader(t, p.grader.Name, v)
	if n := count(queuedVersion, gradeJobRegrade); n != 0 {
		t.Errorf("superseded version keeps %d regrade jobs, want 0", n)
	}
	if n := count(g.CurrentVersion, gradeJobRegrade); n != 33 {
		t.Errorf("newest version: %d regrade jobs, want the 33 moved", n)
	}
}

// A threshold-only change rescores the stored answers through the worker with no model call.
func TestPostgres_RecomputeFromAnswers(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	runID := p.run(t, RunStateActive)
	if _, err := p.r.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: "human_message", Payload: json.RawMessage(`{"text":"make a repo"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := p.r.SetRunState(ctx, runID, RunStateActive, RunStateCompleted); err != nil {
		t.Fatal(err)
	}
	if _, err := p.r.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: "run_state", Payload: json.RawMessage(`{"state":"completed"}`)}); err != nil {
		t.Fatal(err)
	}
	decisions := &fakeDecisions{resp: inference.SystemOneResponse{Model: "typesafe/jev-1-14", Answers: passingAnswers(), UsageID: "usage-1", CostUSD: new(0.001)}}
	w := newGradeWorker(p.r, &fakeGradingPrincipal{}, decisions, GradeWorkerConfig{}, logging.NewLoggerWithOutput(io.Discard))
	drain := func() {
		jobs, err := p.r.claimGradeJobs(ctx, 100, time.Now().Add(time.Second), gradeLease)
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range jobs {
			if j.TenantID == p.tenant {
				w.process(ctx, j)
			}
		}
	}
	drain()
	first, err := p.r.getGrade(ctx, p.grader.ID, p.grader.CurrentVersion, runID)
	if err != nil || *first.Verdict != "pass" || len(first.Answers) != 4 || *first.CostUSD != 0.001 {
		t.Fatalf("first grade = %+v, %v", first, err)
	}

	// pushed-main answered 0.02, so p 0.98. A 0.99 pass bound makes it uncertain.
	v := testGraderVersion(t)
	v.question(t, "pushed-main").Threshold = &v1alpha1.Threshold{Pass: 0.99, Fail: 0.01}
	g := p.putGrader(t, p.grader.Name, v)
	drain()
	if len(decisions.calls) != 1 {
		t.Errorf("calls = %d, want only the first grade's", len(decisions.calls))
	}
	got, err := p.r.getGrade(ctx, g.ID, g.CurrentVersion, runID)
	if err != nil {
		t.Fatalf("recomputed grade: %v", err)
	}
	if *got.Verdict != "uncertain" || *got.StateHash != *first.StateHash || len(got.Answers) != 4 || got.SourceVersion != p.grader.CurrentVersion {
		t.Errorf("recomputed = %+v, want uncertain from the same state, sourced from the first version", got)
	}
	if got.UsageID != nil || got.CostUSD == nil || *got.CostUSD != 0 {
		t.Errorf("recomputed usage %v cost %v, want no usage and cost 0", got.UsageID, got.CostUSD)
	}
	// The recompute adds nothing to the run's grading cost summed across versions.
	var total float64
	if err := p.r.pool.QueryRow(ctx, `SELECT sum(cost_usd) FROM grades WHERE run_id = $1`, runID).Scan(&total); err != nil || total != 0.001 {
		t.Errorf("cost across versions = %v, %v, want the first grade's 0.001", total, err)
	}
}

// The grading grant's scopes are the models of the tenant's Graders at their current versions.
func TestPostgres_GraderModelScopes(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	other := testGraderVersion(t)
	other.Model = v1alpha1.ModelRef{Family: "typesafe", Name: "jev-2"}
	p.putGrader(t, "other-model", other)
	p.putGrader(t, "same-model", testGraderVersion(t))

	got, err := p.r.graderModelScopes(ctx, p.tenant)
	want := []string{"inference:typesafe:jev-1-14:use", "inference:typesafe:jev-2:use"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("scopes = %v, %v, want %v", got, err, want)
	}
	if err := p.r.DeleteGrader(ctx, p.tenant, "other-model"); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.r.graderModelScopes(ctx, p.tenant); !slices.Equal(got, want[:1]) {
		t.Errorf("after delete = %v, want %v", got, want[:1])
	}
}
