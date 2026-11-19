package agents

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/agents/grading"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// The Grader read API (docs/adr/2026-11-19-graders.md, Decision 10).

// gradeReader is the storage the Grader reads use.
type gradeReader interface {
	// runGrades returns the run's grade per Grader of the agent, on the Grader's current version
	// when it has one and otherwise on the newest version that graded the run.
	runGrades(ctx context.Context, tenantID, agentTypeID, runID string) ([]storedGrade, error)
	// agentGraders returns each Grader of the agent with its window.
	agentGraders(ctx context.Context, tenantID, agentTypeID string) ([]storedGrader, error)
	// agentGrader returns one Grader of the agent with its window and the window's failed and
	// uncertain answers. A Grader of another agent is ErrNotFound.
	agentGrader(ctx context.Context, tenantID, agentTypeID, name string) (*storedGrader, error)
}

// SetGradeReader installs the storage the Grader reads use. When unset, every Grader read is ErrNotFound.
func (s *RunService) SetGradeReader(r gradeReader) { s.grades = r }

// storedGrade is one grades row with its Grader, its version spec and its answers.
type storedGrade struct {
	Grader      string
	Description string
	Version     string
	Spec        []byte
	Status      string
	Verdict     *string
	AppliesP    *float64
	StateCap    *int
	InputTokens *int64
	ModelServed *string
	CostUSD     *float64
	GradedAt    time.Time
	Answers     []gradeAnswer
}

// storedGrader is a Grader with its current version spec and the current version's latest
// graded runs, newest first. Answers holds only fail and uncertain answers.
type storedGrader struct {
	Name        string
	Description string
	MinPassRate *int
	Version     string
	Spec        []byte
	TooLarge    int
	// Window is the newest graded runs, at most grading.PassRateWindow of them.
	Window []windowRun
}

// windowRun is one graded run of a Grader's window.
type windowRun struct {
	RunID     string
	Title     *string
	StartedAt time.Time
	Verdict   grading.Verdict
	Answers   map[string]string
	// Raw is each stored answer as returned, keyed by question. It is read only for the Grader detail.
	Raw map[string]json.RawMessage
}

// RunGrade is a run's grade on one Grader. Fields the pipeline did not record are null.
type RunGrade struct {
	Grader      string   `json:"grader"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Model       string   `json:"model"`
	Status      string   `json:"status"`
	Verdict     *string  `json:"verdict"`
	AppliesP    *float64 `json:"applies_p"`
	// StateCap is the tool_result cap in characters. 0 is no bodies. Null is not capped.
	StateCap    *int    `json:"state_cap"`
	InputTokens *int64  `json:"input_tokens"`
	ModelServed *string `json:"model_served"`
	// CostUSD is this grade's own cost. It is null when the model has no price.
	CostUSD  *float64      `json:"cost_usd"`
	GradedAt time.Time     `json:"graded_at"`
	Answers  []GradeAnswer `json:"answers"`
}

// GradeAnswer is one question's answer with the question it answers.
type GradeAnswer struct {
	Question string `json:"question"`
	Type     string `json:"type"`
	Text     string `json:"text"`
	// P is null when the question was skipped.
	P       *float64 `json:"p"`
	Verdict string   `json:"verdict"`
	// Probabilities are a choice or scale answer's probability per option or level name.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// GraderSummary is a Grader's standing over its current version's latest graded runs.
type GraderSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// MinPassRate is a percent. Null means the Grader has no minimum.
	MinPassRate *int                   `json:"min_pass_rate"`
	Version     string                 `json:"version"`
	Status      grading.PassRateStatus `json:"status"`
	Passed      int                    `json:"passed"`
	Graded      int                    `json:"graded"`
	// TooLarge counts the current version's runs whose state did not fit the model.
	TooLarge int `json:"too_large"`
}

// GraderDetail is a Grader's summary, its version spec and its questions' failed and
// uncertain runs over the same window.
type GraderDetail struct {
	GraderSummary
	Model       string           `json:"model"`
	Threshold   ThresholdView    `json:"threshold"`
	AppliesWhen string           `json:"applies_when,omitempty"`
	Questions   []GraderQuestion `json:"questions"`
}

// ThresholdView is a pass/fail band.
type ThresholdView struct {
	Pass float64 `json:"pass"`
	Fail float64 `json:"fail"`
}

// GraderQuestion is one question of the version spec with its window counts. Questions come
// most failed and uncertain first.
type GraderQuestion struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Question  string         `json:"question"`
	Expect    *bool          `json:"expect,omitempty"`
	Options   []AnswerOption `json:"options,omitempty"`
	Levels    []AnswerOption `json:"levels,omitempty"`
	Accept    []string       `json:"accept,omitempty"`
	SkipOn    []string       `json:"skip_on,omitempty"`
	Threshold *ThresholdView `json:"threshold,omitempty"`
	// Answers are every answer the question can get, in spec order, with the window's count of each.
	Answers   []AnswerCount `json:"answers"`
	Failed    int           `json:"failed"`
	Uncertain int           `json:"uncertain"`
	// Runs are the window's runs that failed or were uncertain on this question, newest first.
	Runs []FlaggedRun `json:"runs"`
}

// AnswerOption is a choice option or a scale level.
type AnswerOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AnswerCount is one answer a question can get. Count is how many of the window's runs had it as
// the most likely answer, from the stored probabilities. A run with no stored answer is not counted.
type AnswerCount struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Passes      bool   `json:"passes"`
	Skips       bool   `json:"skips"`
	Count       int    `json:"count"`
}

// FlaggedRun is a run whose answer to one question was fail or uncertain.
type FlaggedRun struct {
	RunID string `json:"run_id"`
	// Title is the run's summary, null until one is written.
	Title     *string   `json:"title"`
	StartedAt time.Time `json:"started_at"`
	Verdict   string    `json:"verdict"`
}

// RunGrades returns the run's grades under the run read check of GET /api/v1/runs/{id}. A run
// that has not ended has none.
func (s *RunService) RunGrades(ctx context.Context, runID string) ([]RunGrade, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.RunGrades", trace.WithAttributes(observability.RunIDKey.String(runID)))
	defer span.End()
	if s.grades == nil {
		return nil, ErrNotFound
	}
	run, at, err := s.loadRunForAction(ctx, runID, "read")
	if err != nil {
		return nil, err
	}
	out := []RunGrade{}
	if !run.State.Terminal() {
		return out, nil
	}
	stored, err := s.grades.runGrades(ctx, run.TenantID, at.ID, run.ID)
	if err != nil {
		observability.RecordFailure(span, err, errSpanReadGrades)
		return nil, err
	}
	for _, g := range stored {
		rg, err := runGradeOf(g)
		if err != nil {
			observability.RecordFailure(span, err, errSpanReadGrades)
			return nil, err
		}
		out = append(out, rg)
	}
	return out, nil
}

// AgentGraders returns the agent's Graders, failing first and then by pass rate, lowest first.
func (s *RunService) AgentGraders(ctx context.Context, agentTypeID string) ([]GraderSummary, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.AgentGraders", trace.WithAttributes(observability.AgentIDKey.String(agentTypeID)))
	defer span.End()
	if s.grades == nil {
		return nil, ErrNotFound
	}
	at, err := s.gradedAgent(ctx, agentTypeID)
	if err != nil {
		return nil, err
	}
	stored, err := s.grades.agentGraders(ctx, at.TenantID, at.ID)
	if err != nil {
		observability.RecordFailure(span, err, errSpanReadGrades)
		return nil, err
	}
	out := make([]GraderSummary, len(stored))
	for i, g := range stored {
		out[i] = summaryOf(g)
	}
	slices.SortStableFunc(out, compareGraders)
	return out, nil
}

// AgentGrader returns one Grader of the agent with its spec and its questions' flagged runs.
func (s *RunService) AgentGrader(ctx context.Context, agentTypeID, name string) (*GraderDetail, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.AgentGrader", trace.WithAttributes(observability.AgentIDKey.String(agentTypeID)))
	defer span.End()
	if s.grades == nil {
		return nil, ErrNotFound
	}
	at, err := s.gradedAgent(ctx, agentTypeID)
	if err != nil {
		return nil, err
	}
	g, err := s.grades.agentGrader(ctx, at.TenantID, at.ID, name)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			observability.RecordFailure(span, err, errSpanReadGrades)
		}
		return nil, err
	}
	d, err := detailOf(*g)
	if err != nil {
		observability.RecordFailure(span, err, errSpanReadGrades)
		return nil, err
	}
	return d, nil
}

// gradedAgent loads an agent whose Graders the caller may read: its runs' read check, and for a
// delegated agent agents:delegated:read, since the counts include runs other people started.
// A deny is ErrNotFound so the agent's existence does not leak.
func (s *RunService) gradedAgent(ctx context.Context, agentTypeID string) (*AgentType, error) {
	at, err := s.atRepo.GetAgentType(ctx, agentTypeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get agent type: %w", err)
	}
	if at.TenantID != tenantFromContext(ctx) {
		return nil, ErrNotFound
	}
	if err = s.authz.checkRun(ctx, *at, "read"); err != nil {
		if errors.Is(err, ErrForbidden) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if at.IdentityMode.OrDefault() == IdentityModeDelegated {
		seeAll, err := s.authz.canReadDelegatedRuns(ctx)
		if err != nil {
			return nil, err
		}
		if !seeAll {
			return nil, ErrNotFound
		}
	}
	return at, nil
}

// runGradeOf joins a stored grade's answers to the questions of the version that graded it.
func runGradeOf(g storedGrade) (RunGrade, error) {
	v, err := parseGraderVersion(g.Spec)
	if err != nil {
		return RunGrade{}, err
	}
	out := RunGrade{
		Grader: g.Grader, Description: g.Description, Version: g.Version, Model: v.modelName(), Status: g.Status,
		Verdict: g.Verdict, AppliesP: g.AppliesP, StateCap: g.StateCap, InputTokens: g.InputTokens,
		ModelServed: g.ModelServed, CostUSD: g.CostUSD, GradedAt: g.GradedAt, Answers: []GradeAnswer{},
	}
	for _, a := range g.Answers {
		q := v.find(a.Question)
		if q == nil {
			return RunGrade{}, fmt.Errorf("%w: grade of %s answers question %q, which version %s does not have", grading.ErrBadAnswer, g.Grader, a.Question, g.Version)
		}
		probs, err := answerProbabilities(*q, a.Raw)
		if err != nil {
			return RunGrade{}, err
		}
		out.Answers = append(out.Answers, GradeAnswer{Question: q.Name, Type: q.Type, Text: q.Question, P: a.P, Verdict: a.Verdict, Probabilities: probs})
	}
	return out, nil
}

// find returns the named question, or nil.
func (v graderVersion) find(name string) *v1alpha1.GraderQuestion {
	for i := range v.Questions {
		if v.Questions[i].Name == name {
			return &v.Questions[i]
		}
	}
	return nil
}

// answerProbabilities reads a stored choice or scale answer's distribution. A scale's is keyed
// by level index from 0 and comes back keyed by level name. A boolean has none.
func answerProbabilities(q v1alpha1.GraderQuestion, raw json.RawMessage) (map[string]float64, error) {
	if q.Type == string(grading.Boolean) {
		return nil, nil
	}
	var a struct {
		Probabilities map[string]float64 `json:"probabilities"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("decode answer to %s: %w", q.Name, err)
	}
	if q.Type == string(grading.Choice) {
		return a.Probabilities, nil
	}
	return levelDist(q, a.Probabilities)
}

// summaryOf reads a Grader's window with grading.Window.
func summaryOf(g storedGrader) GraderSummary {
	verdicts := make([]grading.Verdict, len(g.Window))
	for i, r := range g.Window {
		verdicts[i] = r.Verdict
	}
	var minRate *float64
	if g.MinPassRate != nil {
		minRate = new(float64(*g.MinPassRate) / 100)
	}
	w := grading.Window(minRate, verdicts)
	return GraderSummary{Name: g.Name, Description: g.Description, MinPassRate: g.MinPassRate, Version: g.Version,
		Status: w.Status, Passed: w.Passed, Graded: w.Graded, TooLarge: g.TooLarge}
}

// compareGraders puts failing Graders first, then orders by pass rate, lowest first. A Grader
// with no graded runs has no rate and comes after those that have one.
func compareGraders(a, b GraderSummary) int {
	if c := cmp.Compare(boolRank(a.Status != grading.Failing), boolRank(b.Status != grading.Failing)); c != 0 {
		return c
	}
	if c := cmp.Compare(boolRank(a.Graded == 0), boolRank(b.Graded == 0)); c != 0 {
		return c
	}
	// Cross-multiplied so equal rates compare equal without float error.
	if c := cmp.Compare(a.Passed*b.Graded, b.Passed*a.Graded); c != 0 {
		return c
	}
	return cmp.Compare(a.Name, b.Name)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// detailOf adds the version spec and per-question flagged runs to a Grader's summary.
func detailOf(g storedGrader) (*GraderDetail, error) {
	v, err := parseGraderVersion(g.Spec)
	if err != nil {
		return nil, err
	}
	d := &GraderDetail{
		GraderSummary: summaryOf(g), Model: v.modelName(),
		Threshold:   ThresholdView{Pass: v.Threshold.Pass, Fail: v.Threshold.Fail},
		AppliesWhen: v.AppliesWhen, Questions: make([]GraderQuestion, len(v.Questions)),
	}
	for i, q := range v.Questions {
		gq := GraderQuestion{Name: q.Name, Type: q.Type, Question: q.Question, Expect: q.Expect,
			Options: answerOptions(q.Options), Levels: answerOptions(q.Levels), Accept: q.Accept, SkipOn: q.SkipOn, Runs: []FlaggedRun{}}
		if q.Threshold != nil {
			gq.Threshold = &ThresholdView{Pass: q.Threshold.Pass, Fail: q.Threshold.Fail}
		}
		if gq.Answers, err = answerCounts(q, g.Window); err != nil {
			return nil, err
		}
		for _, r := range g.Window {
			verdict := r.Answers[q.Name]
			switch grading.Verdict(verdict) {
			case grading.Fail:
				gq.Failed++
			case grading.Uncertain:
				gq.Uncertain++
			default:
				continue
			}
			gq.Runs = append(gq.Runs, FlaggedRun{RunID: r.RunID, Title: r.Title, StartedAt: r.StartedAt, Verdict: verdict})
		}
		d.Questions[i] = gq
	}
	slices.SortStableFunc(d.Questions, func(a, b GraderQuestion) int {
		return cmp.Or(cmp.Compare(b.Failed+b.Uncertain, a.Failed+a.Uncertain), cmp.Compare(a.Name, b.Name))
	})
	return d, nil
}

// answerCounts lists q's answers in spec order and counts each window run under its most likely
// answer. A boolean's answers are yes and no, and yes is most likely at p 0.5 or above. A tie goes
// to the answer first in spec order.
func answerCounts(q v1alpha1.GraderQuestion, window []windowRun) ([]AnswerCount, error) {
	var out []AnswerCount
	if q.Type == string(grading.Boolean) {
		expect := q.Expect != nil && *q.Expect
		out = []AnswerCount{{Name: "yes", Passes: expect}, {Name: "no", Passes: !expect}}
	} else {
		for _, a := range answersOf(q) {
			out = append(out, AnswerCount{Name: a.Name, Description: a.Description,
				Passes: slices.Contains(q.Accept, a.Name), Skips: slices.Contains(q.SkipOn, a.Name)})
		}
	}
	for _, r := range window {
		raw, ok := r.Raw[q.Name]
		if !ok {
			continue
		}
		i, err := likeliest(q, raw, out)
		if err != nil {
			return nil, err
		}
		if i >= 0 {
			out[i].Count++
		}
	}
	return out, nil
}

// likeliest is the index in answers of a stored answer's most likely answer, or -1 when it has no probabilities.
func likeliest(q v1alpha1.GraderQuestion, raw json.RawMessage, answers []AnswerCount) (int, error) {
	var a struct {
		Noul          *float64           `json:"noul"`
		Probabilities map[string]float64 `json:"probabilities"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return -1, fmt.Errorf("%w: decode answer to %s: %w", grading.ErrBadAnswer, q.Name, err)
	}
	if q.Type == string(grading.Boolean) {
		switch {
		case a.Noul == nil:
			return -1, nil
		case *a.Noul >= 0.5:
			return 0, nil
		}
		return 1, nil
	}
	dist := a.Probabilities
	if q.Type == string(grading.Scale) {
		var err error
		if dist, err = levelDist(q, a.Probabilities); err != nil {
			return -1, err
		}
	}
	best := -1
	for i, ans := range answers {
		if p, ok := dist[ans.Name]; ok && (best < 0 || p > dist[answers[best].Name]) {
			best = i
		}
	}
	return best, nil
}

func answerOptions(in []v1alpha1.GraderAnswer) []AnswerOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]AnswerOption, len(in))
	for i, a := range in {
		out[i] = AnswerOption{Name: a.Name, Description: a.Description}
	}
	return out
}
