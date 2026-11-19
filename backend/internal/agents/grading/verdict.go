package grading

import (
	"errors"
	"fmt"
	"slices"
)

// Threshold is a pass/fail band on a probability.
type Threshold struct {
	Pass float64
	Fail float64
}

// DefaultThreshold is the band when a Grader sets none.
var DefaultThreshold = Threshold{Pass: 0.7, Fail: 0.3}

// QuestionType is how a question is answered.
type QuestionType string

const (
	Boolean QuestionType = "boolean"
	Choice  QuestionType = "choice"
	Scale   QuestionType = "scale"
)

// Question is one Grader question. Options names a choice's options, including
// the added none, or a scale's levels. Its order fixes the order p is summed in.
type Question struct {
	Name      string
	Type      QuestionType
	Expect    bool
	Options   []string
	Accept    []string
	SkipOn    []string
	Threshold *Threshold
}

// GraderSpec holds the Grader fields that grading reads.
type GraderSpec struct {
	Threshold   Threshold
	AppliesWhen string
	Questions   []Question
}

// Answer is the model's answer to one question. P is a boolean's probability
// of yes. Dist is a choice or scale's probability per option or level name.
type Answer struct {
	P    float64
	Dist map[string]float64
}

// Verdict is the outcome of one question or of a graded run.
type Verdict string

const (
	Pass      Verdict = "pass"
	Fail      Verdict = "fail"
	Uncertain Verdict = "uncertain"
	Skipped   Verdict = "skipped"
)

// Status is a grade's status. The pipeline adds too_large and error.
type Status string

const (
	Graded           Status = "graded"
	NotApplicable    Status = "not_applicable"
	AppliesUncertain Status = "applies_uncertain"
)

// QuestionResult is one question's p and verdict. P is nil when skipped.
type QuestionResult struct {
	Question string
	P        *float64
	Verdict  Verdict
}

// Grade is a run's result for one Grader. Verdict is set when Status is
// Graded. Questions is empty when the run was not graded on appliesWhen.
type Grade struct {
	Status    Status
	Verdict   Verdict
	Questions []QuestionResult
}

var (
	// ErrMissingAnswer reports a question or appliesWhen with no answer.
	ErrMissingAnswer = errors.New("grading: missing answer")
	// ErrBadAnswer reports an answer that cannot give a p.
	ErrBadAnswer = errors.New("grading: bad answer")
)

// epsilon absorbs float error when an answer sums several probabilities,
// so a sum that should equal a bound counts as on it.
const epsilon = 1e-9

// band places p on t: Pass at or above pass, Fail at or below fail.
func band(p float64, t Threshold) Verdict {
	switch {
	case p >= t.Pass-epsilon:
		return Pass
	case p <= t.Fail+epsilon:
		return Fail
	}
	return Uncertain
}

// Score grades a run from its answers. applies is the appliesWhen answer and
// is ignored when the spec has no appliesWhen.
func Score(spec GraderSpec, applies *float64, answers map[string]Answer) (Grade, error) {
	t := spec.Threshold
	if spec.AppliesWhen != "" {
		if applies == nil {
			return Grade{}, fmt.Errorf("appliesWhen: %w", ErrMissingAnswer)
		}
		switch band(*applies, t) {
		case Fail:
			return Grade{Status: NotApplicable}, nil
		case Uncertain:
			return Grade{Status: AppliesUncertain}, nil
		}
	}

	g := Grade{Status: Graded, Questions: make([]QuestionResult, 0, len(spec.Questions))}
	for _, q := range spec.Questions {
		a, ok := answers[q.Name]
		if !ok {
			return Grade{}, fmt.Errorf("question %s: %w", q.Name, ErrMissingAnswer)
		}
		qt := t
		if q.Threshold != nil {
			qt = *q.Threshold
		}
		r, err := scoreQuestion(q, qt, a)
		if err != nil {
			return Grade{}, fmt.Errorf("question %s: %w", q.Name, err)
		}
		g.Questions = append(g.Questions, r)
	}
	g.Verdict = rollUp(g.Questions)
	if g.Verdict == Skipped {
		g.Status, g.Verdict = NotApplicable, ""
	}
	return g, nil
}

// rollUp is the run verdict: fail beats uncertain beats pass. It is Skipped
// when every question was skipped.
func rollUp(results []QuestionResult) Verdict {
	v := Skipped
	for _, r := range results {
		switch {
		case r.Verdict == Fail:
			return Fail
		case r.Verdict == Uncertain:
			v = Uncertain
		case r.Verdict == Pass && v == Skipped:
			v = Pass
		}
	}
	return v
}

func scoreQuestion(q Question, t Threshold, a Answer) (QuestionResult, error) {
	r := QuestionResult{Question: q.Name}
	if q.Type == Boolean {
		if !isProbability(a.P) {
			return r, fmt.Errorf("%w: p %v", ErrBadAnswer, a.P)
		}
		p := a.P
		if !q.Expect {
			p = 1 - p
		}
		r.P, r.Verdict = &p, band(p, t)
		return r, nil
	}
	if q.Type != Choice && q.Type != Scale {
		return r, fmt.Errorf("%w: type %q", ErrBadAnswer, q.Type)
	}
	for name, v := range a.Dist {
		if !slices.Contains(q.Options, name) || !isProbability(v) {
			return r, fmt.Errorf("%w: %s at %v", ErrBadAnswer, name, v)
		}
	}

	skip := sum(a.Dist, q.SkipOn)
	skipVerdict := Fail
	if len(q.SkipOn) > 0 {
		skipVerdict = band(skip, t)
	}
	if skipVerdict == Pass {
		r.Verdict = Skipped
		return r, nil
	}
	// The denominator is every name not in skipOn.
	scored := 0.0
	for _, name := range q.Options {
		if !slices.Contains(q.SkipOn, name) {
			scored += a.Dist[name]
		}
	}
	if scored <= 0 {
		return r, fmt.Errorf("%w: no probability outside skipOn", ErrBadAnswer)
	}
	p := sum(a.Dist, q.Accept) / scored
	r.P = &p
	if skipVerdict == Uncertain {
		r.Verdict = Uncertain
		return r, nil
	}
	r.Verdict = band(p, t)
	return r, nil
}

func isProbability(v float64) bool { return v >= 0 && v <= 1 }

func sum(dist map[string]float64, names []string) float64 {
	s := 0.0
	for _, n := range names {
		s += dist[n]
	}
	return s
}
