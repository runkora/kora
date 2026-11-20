package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/agents/grading"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// fakeGradeReader returns fixed rows and records whether it was read.
type fakeGradeReader struct {
	grades  []storedGrade
	graders []storedGrader
	read    bool
}

func (f *fakeGradeReader) runGrades(context.Context, string, string, string) ([]storedGrade, error) {
	f.read = true
	return f.grades, nil
}

func (f *fakeGradeReader) agentGraders(context.Context, string, string) ([]storedGrader, error) {
	f.read = true
	return f.graders, nil
}

func (f *fakeGradeReader) agentGrader(_ context.Context, _, _, name string) (*storedGrader, error) {
	f.read = true
	for i := range f.graders {
		if f.graders[i].Name == name {
			return &f.graders[i], nil
		}
	}
	return nil, ErrNotFound
}

// patCtx is a PAT caller, which reads only what its scopes allow.
func patCtx(tenantID string) context.Context {
	return token.WithClaims(context.Background(), &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "bob"},
		Kora:             token.KoraClaims{TenantID: tenantID, PrincipalType: token.PrincipalTypePAT, PatID: "pat-1"},
	})
}

func testVersionSpec(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(testGraderVersion(t))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The Grader reads follow the run read check. The agent-type reads of a delegated agent also
// need agents:delegated:read. Every deny reads as not found.
func TestGradeReads_Authz(t *testing.T) {
	tests := []struct {
		name    string
		granted []string
		agent   string
		allowed bool
	}{
		{"no run scope", []string{"agents:types:read"}, "at-auto", false},
		{"another agent's run scope", []string{"agents:runs:other:read"}, "at-auto", false},
		{"the agent's run read", []string{"agents:runs:helper:read"}, "at-auto", true},
		{"the parent run read", []string{"agents:runs:read"}, "at-auto", true},
		{"a delegated agent without delegated read", []string{"agents:runs:reviewer:read"}, "at-del", false},
		{"a delegated agent with delegated read", []string{"agents:runs:reviewer:read", "agents:delegated:read"}, "at-del", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{granted: tt.granted})
			seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "helper"})
			seedAgentType(t, repo, AgentType{ID: "at-del", TenantID: "t1", Slug: "reviewer", IdentityMode: IdentityModeDelegated})
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: tt.agent, State: RunStateCompleted, CreatedBy: "carol"})
			reader := &fakeGradeReader{graders: []storedGrader{{Name: "g", Spec: testVersionSpec(t)}}}
			svc.run.SetGradeReader(reader)
			ctx := patCtx("t1")

			checks := map[string]error{}
			_, checks["run grades"] = svc.run.RunGrades(ctx, "run-1")
			_, checks["graders"] = svc.run.AgentGraders(ctx, tt.agent)
			_, checks["grader"] = svc.run.AgentGrader(ctx, tt.agent, "g")
			for read, err := range checks {
				if tt.allowed && err != nil {
					t.Errorf("%s = %v, want allowed", read, err)
				}
				if !tt.allowed && !errors.Is(err, ErrNotFound) {
					t.Errorf("%s = %v, want ErrNotFound", read, err)
				}
			}
			if !tt.allowed && reader.read {
				t.Error("a denied read reached storage")
			}
		})
	}
}

// A service built without a grade reader reads every Grader as not found instead of panicking.
func TestGradeReads_NoReader(t *testing.T) {
	svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{granted: []string{"agents:runs:read"}})
	seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "helper"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-auto", State: RunStateCompleted})
	ctx := patCtx("t1")
	if _, err := svc.run.AgentGraders(ctx, "at-auto"); !errors.Is(err, ErrNotFound) {
		t.Errorf("AgentGraders = %v, want ErrNotFound", err)
	}
	if _, err := svc.run.AgentGrader(ctx, "at-auto", "g"); !errors.Is(err, ErrNotFound) {
		t.Errorf("AgentGrader = %v, want ErrNotFound", err)
	}
	if _, err := svc.run.RunGrades(ctx, "run-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RunGrades = %v, want ErrNotFound", err)
	}
}

// An agent of another tenant reads as not found.
func TestGradeReads_OtherTenant(t *testing.T) {
	svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{granted: []string{"agents:runs:read"}})
	seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t2", Slug: "helper"})
	svc.run.SetGradeReader(&fakeGradeReader{})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t2", AgentTypeID: "at-auto", State: RunStateCompleted})
	ctx := patCtx("t1")
	if _, err := svc.run.AgentGraders(ctx, "at-auto"); !errors.Is(err, ErrNotFound) {
		t.Errorf("AgentGraders = %v, want ErrNotFound", err)
	}
	if _, err := svc.run.AgentGrader(ctx, "at-auto", "g"); !errors.Is(err, ErrNotFound) {
		t.Errorf("AgentGrader = %v, want ErrNotFound", err)
	}
	if _, err := svc.run.RunGrades(ctx, "run-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RunGrades = %v, want ErrNotFound", err)
	}
}

// A run that has not ended has no grades and does not read them.
func TestRunGrades_NotEnded(t *testing.T) {
	svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{granted: []string{"agents:runs:read"}})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-auto", State: RunStateIdle})
	reader := &fakeGradeReader{}
	svc.run.SetGradeReader(reader)
	got, err := svc.run.RunGrades(patCtx("t1"), "run-1")
	if err != nil || got == nil || len(got) != 0 || reader.read {
		t.Errorf("RunGrades = %v, %v, read %v; want an empty list and no read", got, err, reader.read)
	}
}

// A grade's answers carry their question's text and type. A scale's distribution is keyed by level name.
func TestRunGradeOf(t *testing.T) {
	g := storedGrade{Grader: "new-repository", Version: "v1", Spec: testVersionSpec(t), Status: "graded", Verdict: new("pass"),
		CostUSD: nil, Answers: []gradeAnswer{
			{Question: "opened-pr", Raw: json.RawMessage(`{"type":"noul","noul":0.9}`), P: new(0.9), Verdict: "pass"},
			{Question: "organization", Raw: json.RawMessage(`{"type":"choice","choice":"core","probabilities":{"core":0.8,"sandbox":0.2},"confidence":0.8}`), P: new(1.0), Verdict: "pass"},
			{Question: "rounds", Raw: json.RawMessage(`{"type":"score","score":1,"probabilities":{"0":0.1,"1":0.6,"2":0.3},"confidence":0.6}`), P: new(0.67), Verdict: "uncertain"},
		}}
	got, err := runGradeOf(g)
	if err != nil {
		t.Fatalf("runGradeOf: %v", err)
	}
	if got.Model != "typesafe/jev-1-14" || got.CostUSD != nil || len(got.Answers) != 3 {
		t.Fatalf("grade = %+v", got)
	}
	if a := got.Answers[0]; a.Type != "boolean" || a.Text != "Did the agent open a pull request?" || a.Probabilities != nil {
		t.Errorf("boolean answer = %+v", a)
	}
	if a := got.Answers[1]; a.Probabilities["core"] != 0.8 {
		t.Errorf("choice answer = %+v", a)
	}
	if a := got.Answers[2]; a.Type != "scale" || a.Probabilities["one"] != 0.6 || a.Probabilities["no-pr"] != 0.1 || len(a.Probabilities) != 3 {
		t.Errorf("scale answer = %+v", a)
	}

	g.Answers = []gradeAnswer{{Question: "gone", Raw: json.RawMessage(`{}`), Verdict: "pass"}}
	if _, err := runGradeOf(g); err == nil {
		t.Error("an answer to a question the version lacks was accepted")
	}
}

// window builds a Grader window from verdicts, newest first.
func window(verdicts ...grading.Verdict) []windowRun {
	out := make([]windowRun, len(verdicts))
	for i, v := range verdicts {
		out[i] = windowRun{RunID: "run", Verdict: v}
	}
	return out
}

func repeat(v grading.Verdict, n int) []grading.Verdict {
	return slices.Repeat([]grading.Verdict{v}, n)
}

// Graders sort failing first, then by pass rate lowest first, with no graded runs last.
func TestAgentGraders_Sort(t *testing.T) {
	svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{granted: []string{"agents:runs:read"}})
	seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "helper"})
	minimum := new(90)
	svc.run.SetGradeReader(&fakeGradeReader{graders: []storedGrader{
		{Name: "a-none", Window: nil},
		{Name: "b-half", Window: window(append(repeat(grading.Pass, 10), repeat(grading.Fail, 10)...)...)},
		{Name: "c-failing-80", MinPassRate: minimum, Window: window(append(repeat(grading.Pass, 16), repeat(grading.Fail, 4)...)...)},
		{Name: "d-passing", MinPassRate: minimum, Window: window(repeat(grading.Pass, 20)...)},
		{Name: "e-failing-50", MinPassRate: minimum, Window: window(append(repeat(grading.Pass, 10), repeat(grading.Uncertain, 10)...)...)},
		{Name: "f-few", MinPassRate: minimum, Window: window(repeat(grading.Fail, 14)...)},
	}})
	got, err := svc.run.AgentGraders(patCtx("t1"), "at-auto")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range got {
		names = append(names, g.Name+":"+string(g.Status))
	}
	want := []string{"e-failing-50:failing", "c-failing-80:failing", "f-few:not_enough_runs", "b-half:no_minimum", "d-passing:passing", "a-none:no_minimum"}
	if !slices.Equal(names, want) {
		t.Errorf("order = %v, want %v", names, want)
	}
}

// A question's counts and runs cover the window's fail and uncertain answers, and questions
// sort by failed plus uncertain.
func TestDetailOf(t *testing.T) {
	at := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	w := []windowRun{
		{RunID: "r3", StartedAt: at, Verdict: grading.Fail, Answers: map[string]string{"rounds": "fail", "opened-pr": "uncertain"}},
		{RunID: "r2", StartedAt: at, Verdict: grading.Uncertain, Answers: map[string]string{"rounds": "uncertain"}},
		{RunID: "r1", StartedAt: at, Verdict: grading.Pass},
	}
	d, err := detailOf(storedGrader{Name: "g", Version: "v1", Spec: testVersionSpec(t), Window: w})
	if err != nil {
		t.Fatal(err)
	}
	if d.Graded != 3 || d.Passed != 1 || d.Status != grading.NoMinimum || d.Model != "typesafe/jev-1-14" || d.AppliesWhen == "" {
		t.Errorf("summary = %+v", d.GraderSummary)
	}
	var order []string
	for _, q := range d.Questions {
		order = append(order, q.Name)
	}
	if !slices.Equal(order, []string{"rounds", "opened-pr", "organization", "pushed-main"}) {
		t.Errorf("question order = %v", order)
	}
	r := d.Questions[0]
	if r.Failed != 1 || r.Uncertain != 1 || len(r.Runs) != 2 || r.Runs[0].RunID != "r3" || r.Runs[1].Verdict != "uncertain" {
		t.Errorf("rounds = %+v", r)
	}
	if len(r.Levels) != 3 || r.Levels[0].Description != "No pull request was opened" || !slices.Equal(r.SkipOn, []string{"no-pr"}) {
		t.Errorf("rounds spec = %+v", r)
	}
	if q := d.Questions[3]; q.Threshold == nil || q.Threshold.Pass != 0.9 || q.Expect == nil || *q.Expect || q.Runs == nil {
		t.Errorf("pushed-main = %+v", q)
	}
}

func TestHandleGradeReads(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: defaultTestTenant, AgentTypeID: "at-1", State: RunStateCompleted})
	c.runs.SetGradeReader(&fakeGradeReader{
		grades:  []storedGrade{{Grader: "g", Version: "v1", Spec: testVersionSpec(t), Status: "too_large"}},
		graders: []storedGrader{{Name: "g", Version: "v1", Spec: testVersionSpec(t)}},
	})
	get := func(h http.HandlerFunc, values map[string]string) *httptest.ResponseRecorder {
		req := withTenantRequest(httptest.NewRequest(http.MethodGet, "/", nil), defaultTestTenant)
		for k, v := range values {
			req.SetPathValue(k, v)
		}
		w := httptest.NewRecorder()
		h(w, req)
		return w
	}
	for _, tc := range []struct {
		name   string
		h      http.HandlerFunc
		values map[string]string
		code   int
		key    string
	}{
		{"run grades", c.handleRunGrades, map[string]string{"id": "run-1"}, http.StatusOK, "grades"},
		{"unknown run", c.handleRunGrades, map[string]string{"id": "missing"}, http.StatusNotFound, ""},
		{"graders", c.handleAgentGraders, map[string]string{"id": "at-1"}, http.StatusOK, "graders"},
		{"unknown agent", c.handleAgentGraders, map[string]string{"id": "missing"}, http.StatusNotFound, ""},
		{"grader", c.handleAgentGrader, map[string]string{"id": "at-1", "name": "g"}, http.StatusOK, "questions"},
		{"unknown grader", c.handleAgentGrader, map[string]string{"id": "at-1", "name": "nope"}, http.StatusNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := get(tc.h, tc.values)
			if w.Code != tc.code {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.code, w.Body.String())
			}
			if tc.key == "" {
				return
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body[tc.key] == nil {
				t.Errorf("body %s has no %s", w.Body.String(), tc.key)
			}
		})
	}
}

func TestHandleListRuns_GradeFilter(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRunsAcrossTypes(t, repo, "t-1", 3, 1)
	for _, q := range []string{"grader=g&verdict=passed", "question=q", "verdict=fail", "grader=g&verdict=skipped"} {
		if code, _ := listRuns(t, c, q); code != http.StatusBadRequest {
			t.Errorf("%s gave %d, want 400", q, code)
		}
	}
	for _, q := range []string{"grader=g", "grader=g&verdict=fail", "grader=g&question=q&verdict=skipped", "grader=g&question=q"} {
		if code, _ := listRuns(t, c, q); code != http.StatusOK {
			t.Errorf("%s gave %d, want 200", q, code)
		}
	}
}

// An answer counts under its most likely answer, a tie goes to the first in spec order, a run with no
// stored answer is not counted, and an unreadable answer is ErrBadAnswer.
func TestAnswerCounts(t *testing.T) {
	v := testGraderVersion(t)
	org := v.find("organization")
	window := []windowRun{
		{Raw: map[string]json.RawMessage{"organization": json.RawMessage(`{"probabilities":{"core":0.5,"sandbox":0.5}}`)}},
		{Raw: map[string]json.RawMessage{"organization": json.RawMessage(`{"probabilities":{"none":0.9,"core":0.1}}`)}},
		{},
	}
	got, err := answerCounts(*org, window)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range got {
		names = append(names, fmt.Sprintf("%s=%d", a.Name, a.Count))
	}
	if want := []string{"sandbox=1", "core=0", "none=1"}; !slices.Equal(names, want) {
		t.Errorf("counts = %v, want %v", names, want)
	}
	bad := []windowRun{{Raw: map[string]json.RawMessage{"organization": json.RawMessage(`{"probabilities":`)}}}
	if _, err := answerCounts(*org, bad); !errors.Is(err, grading.ErrBadAnswer) {
		t.Errorf("bad answer = %v, want ErrBadAnswer", err)
	}
}
