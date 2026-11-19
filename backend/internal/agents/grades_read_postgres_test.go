package agents

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/agents/grading"
)

// writeGrade inserts a grade with its answers, question to verdict, and returns the run id.
func (p *gradingPG) writeGrade(t *testing.T, g *Grader, version, runID string, started, graded time.Time, status, verdict string, answers map[string]string) string {
	t.Helper()
	ctx := context.Background()
	if runID == "" {
		runID = uuid.NewString()
	}
	id := uuid.NewString()
	var v *string
	if verdict != "" {
		v = &verdict
	}
	if _, err := p.r.pool.Exec(ctx, `INSERT INTO grades (id, tenant_id, grader_id, version, run_id, agent_type_id, run_outcome, run_started_at, graded_at, status, verdict)
		VALUES ($1, $2, $3, $4, $5, $6, 'completed', $7, $8, $9, $10)`, id, p.tenant, g.ID, version, runID, p.agent.ID, started, graded, status, v); err != nil {
		t.Fatalf("insert grade: %v", err)
	}
	for q, av := range answers {
		if _, err := p.r.pool.Exec(ctx, `INSERT INTO grade_answers (grade_id, question, raw, p, verdict) VALUES ($1, $2, '{"type":"noul","noul":0.5}', 0.5, $3)`, id, q, av); err != nil {
			t.Fatalf("insert answer: %v", err)
		}
	}
	return runID
}

func (p *gradingPG) setMinPassRate(t *testing.T, g *Grader, percent int) {
	t.Helper()
	if _, err := p.r.pool.Exec(context.Background(), `UPDATE graders SET min_pass_rate = $2 WHERE id = $1`, g.ID, percent); err != nil {
		t.Fatal(err)
	}
}

// summary reads the named Grader's summary from agentGraders.
func (p *gradingPG) summary(t *testing.T, name string) GraderSummary {
	t.Helper()
	gs, err := p.r.agentGraders(context.Background(), p.tenant, p.agent.ID)
	if err != nil {
		t.Fatalf("agentGraders: %v", err)
	}
	for _, g := range gs {
		if g.Name == name {
			return summaryOf(g)
		}
	}
	t.Fatalf("no grader %s", name)
	return GraderSummary{}
}

// The window is the current version's latest 30 graded runs. Under 15 the status is not
// enough runs. Other statuses and other versions do not count.
func TestPostgres_GraderWindow(t *testing.T) {
	p := newGradingPG(t)
	g, v := p.grader, p.grader.CurrentVersion
	p.setMinPassRate(t, g, 90)
	base := time.Now().UTC().Add(-100 * time.Hour)
	at := func(i int) time.Time { return base.Add(time.Duration(i) * time.Hour) }
	grade := func(i int, verdict string) { p.writeGrade(t, g, v, "", at(i), at(i), "graded", verdict, nil) }

	for i := 1; i <= 14; i++ {
		grade(i, "pass")
	}
	// Neither counts in the window: another version's grade and a run that was too large.
	p.writeGrade(t, g, "test-other-"+uuid.NewString(), "", at(50), at(50), "graded", "fail", nil)
	p.writeGrade(t, g, v, "", at(51), at(51), "too_large", "", nil)
	p.writeGrade(t, g, v, "", at(52), at(52), "not_applicable", "", nil)
	check := func(step string, status grading.PassRateStatus, passed, graded int) {
		t.Helper()
		s := p.summary(t, g.Name)
		if s.Status != status || s.Passed != passed || s.Graded != graded || s.TooLarge != 1 || s.MinPassRate == nil || *s.MinPassRate != 90 {
			t.Errorf("%s: %+v, want %s %d/%d with 1 too large", step, s, status, passed, graded)
		}
	}
	check("14 graded", grading.NotEnoughRuns, 14, 14)
	grade(15, "fail")
	check("15 graded", grading.Passing, 14, 15)
	grade(16, "fail")
	grade(17, "uncertain")
	check("17 graded", grading.Failing, 14, 17)
	for i := 18; i <= 35; i++ {
		grade(i, "pass")
	}
	// The window drops runs 1 to 5: 9 + 18 passes of 30 is exactly 90%.
	check("35 graded", grading.Passing, 27, 30)
}

// Without minPassRate the status is no_minimum at any count. Graders sort failing first.
func TestPostgres_GraderSummariesSort(t *testing.T) {
	p := newGradingPG(t)
	now := time.Now().UTC()
	failing := p.grader
	p.setMinPassRate(t, failing, 90)
	for i := range 20 {
		verdict := "pass"
		if i%2 == 0 {
			verdict = "fail"
		}
		p.writeGrade(t, failing, failing.CurrentVersion, "", now.Add(time.Duration(-i)*time.Minute), now, "graded", verdict, nil)
	}
	none := p.putGrader(t, "a-no-minimum", testGraderVersion(t))
	for i := range 3 {
		p.writeGrade(t, none, none.CurrentVersion, "", now.Add(time.Duration(-i)*time.Minute), now, "graded", "fail", nil)
	}
	empty := p.putGrader(t, "b-empty", testGraderVersion(t))
	p.setMinPassRate(t, empty, 50)

	stored, err := p.r.agentGraders(context.Background(), p.tenant, p.agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	out := make([]GraderSummary, len(stored))
	for i, s := range stored {
		out[i] = summaryOf(s)
	}
	slices.SortStableFunc(out, compareGraders)
	for _, s := range out {
		got = append(got, s.Name+":"+string(s.Status))
	}
	want := []string{failing.Name + ":failing", "a-no-minimum:no_minimum", "b-empty:not_enough_runs"}
	if !slices.Equal(got, want) {
		t.Errorf("graders = %v, want %v", got, want)
	}
}

// A run's grade per Grader is the current version's, and otherwise the one graded last.
func TestPostgres_RunGradesNewestVersion(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	now := time.Now().UTC()
	runID := p.run(t, RunStateCompleted)
	first := p.grader
	oldVersion := first.CurrentVersion
	first = p.putGrader(t, first.Name, testGraderVersion(t))
	// The old version graded the run after the current one did. The current one still wins.
	p.writeGrade(t, first, first.CurrentVersion, runID, now, now.Add(-time.Hour), "graded", "pass", map[string]string{"opened-pr": "pass"})
	p.writeGrade(t, first, oldVersion, runID, now, now, "graded", "fail", map[string]string{"opened-pr": "fail"})

	// A Grader whose current version has not graded the run shows its latest graded version.
	second := p.putGrader(t, "second", testGraderVersion(t))
	olderVersion := second.CurrentVersion
	second = p.putGrader(t, "second", testGraderVersion(t))
	newerVersion := second.CurrentVersion
	second = p.putGrader(t, "second", testGraderVersion(t))
	p.writeGrade(t, second, olderVersion, runID, now, now.Add(-2*time.Hour), "graded", "fail", nil)
	p.writeGrade(t, second, newerVersion, runID, now, now.Add(-time.Hour), "not_applicable", "", nil)

	// A Grader without a grade for the run has no entry.
	p.putGrader(t, "third", testGraderVersion(t))

	got, err := p.r.runGrades(ctx, p.tenant, p.agent.ID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("grades = %+v, want 2", got)
	}
	if g := got[0]; g.Grader != first.Name || g.Version != first.CurrentVersion || *g.Verdict != "pass" || len(g.Answers) != 1 || g.Answers[0].Verdict != "pass" {
		t.Errorf("first = %+v", g)
	}
	if g := got[1]; g.Grader != "second" || g.Version != newerVersion || g.Status != "not_applicable" {
		t.Errorf("second = %+v", g)
	}
	if rg, err := runGradeOf(got[0]); err != nil || rg.Answers[0].Text == "" {
		t.Errorf("runGradeOf = %+v, %v", rg, err)
	}
}

// The detail reads the window's fail and uncertain answers with each run's summary. A
// Grader of another agent is not found.
func TestPostgres_AgentGraderDetail(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	now := time.Now().UTC()
	g := p.grader
	runID := p.run(t, RunStateCompleted)
	if err := p.r.SetRunSummary(ctx, runID, "Create a repository"); err != nil {
		t.Fatal(err)
	}
	p.writeGrade(t, g, g.CurrentVersion, runID, now, now, "graded", "fail", map[string]string{"opened-pr": "fail", "pushed-main": "pass", "rounds": "skipped"})
	p.writeGrade(t, g, g.CurrentVersion, "", now.Add(-time.Hour), now, "graded", "uncertain", map[string]string{"opened-pr": "uncertain"})

	stored, err := p.r.agentGrader(ctx, p.tenant, p.agent.ID, g.Name)
	if err != nil {
		t.Fatal(err)
	}
	d, err := detailOf(*stored)
	if err != nil {
		t.Fatal(err)
	}
	q := d.Questions[0]
	if q.Name != "opened-pr" || q.Failed != 1 || q.Uncertain != 1 || len(q.Runs) != 2 || q.Runs[0].RunID != runID ||
		q.Runs[0].Title == nil || *q.Runs[0].Title != "Create a repository" || q.Runs[1].Title != nil {
		t.Errorf("opened-pr = %+v", q)
	}
	for _, other := range d.Questions[1:] {
		if other.Failed+other.Uncertain != 0 {
			t.Errorf("%s = %+v, want no flagged runs", other.Name, other)
		}
	}

	if _, err := p.r.agentGrader(ctx, p.tenant, p.agent.ID, "missing"); err != ErrNotFound {
		t.Errorf("unknown grader = %v, want ErrNotFound", err)
	}
	if _, err := p.r.agentGrader(ctx, p.tenant, uuid.NewString(), g.Name); err != ErrNotFound {
		t.Errorf("another agent's grader = %v, want ErrNotFound", err)
	}
}

// The runs filter matches the current version's run verdict, or one question's answer verdict.
func TestPostgres_ListRunsGradeFilter(t *testing.T) {
	p := newGradingPG(t)
	ctx := context.Background()
	now := time.Now().UTC()
	g := p.grader
	failed, passed, oldOnly := p.run(t, RunStateCompleted), p.run(t, RunStateCompleted), p.run(t, RunStateCompleted)
	p.run(t, RunStateCompleted)
	p.writeGrade(t, g, g.CurrentVersion, failed, now, now, "graded", "fail", map[string]string{"opened-pr": "fail", "rounds": "skipped"})
	p.writeGrade(t, g, g.CurrentVersion, passed, now, now, "graded", "pass", map[string]string{"opened-pr": "pass"})
	p.writeGrade(t, g, "test-old-"+uuid.NewString(), oldOnly, now, now, "graded", "fail", map[string]string{"opened-pr": "fail"})

	for _, tc := range []struct {
		name   string
		filter GradeFilter
		want   []string
	}{
		{"any grade", GradeFilter{Grader: g.Name}, []string{failed, passed}},
		{"run verdict", GradeFilter{Grader: g.Name, Verdict: "fail"}, []string{failed}},
		{"answer verdict", GradeFilter{Grader: g.Name, Question: "opened-pr", Verdict: "pass"}, []string{passed}},
		{"skipped answer", GradeFilter{Grader: g.Name, Question: "rounds", Verdict: "skipped"}, []string{failed}},
		{"answered question", GradeFilter{Grader: g.Name, Question: "rounds"}, []string{failed}},
		{"unknown grader", GradeFilter{Grader: "missing"}, nil},
		{"with the other filters", GradeFilter{Grader: g.Name, Verdict: "fail"}, []string{failed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := RunListQuery{Scope: RunScope{AgentTypeIDs: []string{p.agent.ID}, Vis: RunVisibility{SeeAll: true}}, Grade: &tc.filter, Limit: 10}
			if tc.name == "with the other filters" {
				// The filter's placeholders follow the scope's and come before the time bounds, cursor and limit.
				q.States, q.CreatedAfter, q.CreatedBefore = []RunState{RunStateCompleted}, now.Add(-time.Hour), now.Add(time.Hour)
				q.After = &runCursor{CreatedAt: now.Add(time.Hour), ID: "z"}
			}
			runs, err := p.r.ListRuns(ctx, p.tenant, q)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, r := range runs {
				got = append(got, r.ID)
			}
			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("runs = %v, want %v", got, want)
			}
		})
	}
}
