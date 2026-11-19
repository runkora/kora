package grading

import (
	"errors"
	"math"
	"testing"
)

func boolQ(name string, expect bool) Question {
	return Question{Name: name, Type: Boolean, Expect: expect}
}

// orgQ is a choice with a none option and none in skipOn.
func orgQ() Question {
	return Question{
		Name:    "organization",
		Type:    Choice,
		Options: []string{"sandbox", "core", "other", "none"},
		Accept:  []string{"sandbox", "core"},
		SkipOn:  []string{"none"},
	}
}

// roundsQ is the spec's scale: no-pr is skipped, four-or-more fails.
func roundsQ() Question {
	return Question{
		Name:    "rounds",
		Type:    Scale,
		Options: []string{"no-pr", "one", "two", "three", "four-or-more"},
		Accept:  []string{"one", "two", "three"},
		SkipOn:  []string{"no-pr"},
	}
}

//go:fix inline
func TestBand(t *testing.T) {
	tests := []struct {
		p    float64
		want Verdict
	}{
		{1, Pass},
		{0.7, Pass},
		{0.6999999999, Pass},
		{0.69, Uncertain},
		{0.5, Uncertain},
		{0.31, Uncertain},
		{0.3, Fail},
		{0.3000000001, Fail},
		{0, Fail},
	}
	for _, tc := range tests {
		if got := band(tc.p, DefaultThreshold); got != tc.want {
			t.Errorf("band(%v) = %s, want %s", tc.p, got, tc.want)
		}
	}
}

func TestScoreQuestion(t *testing.T) {
	strict := &Threshold{Pass: 0.9, Fail: 0.1}
	noSkip := orgQ()
	noSkip.SkipOn = nil
	tests := []struct {
		name    string
		q       Question
		t       *Threshold
		a       Answer
		wantP   *float64
		verdict Verdict
	}{
		{"boolean expect true passes on yes", boolQ("b", true), nil, Answer{P: 0.8}, new(0.8), Pass},
		{"boolean expect true fails on no", boolQ("b", true), nil, Answer{P: 0.2}, new(0.2), Fail},
		{"boolean expect false passes on no", boolQ("b", false), nil, Answer{P: 0.2}, new(0.8), Pass},
		{"boolean expect false fails on yes", boolQ("b", false), nil, Answer{P: 0.9}, new(0.1), Fail},
		{"boolean expect false at 1 minus pass", boolQ("b", false), nil, Answer{P: 0.3}, new(0.7), Pass},
		{"boolean between is uncertain", boolQ("b", true), nil, Answer{P: 0.5}, new(0.5), Uncertain},
		{"question threshold overrides", boolQ("b", true), strict, Answer{P: 0.8}, new(0.8), Uncertain},
		{"question threshold fail bound", boolQ("b", true), strict, Answer{P: 0.1}, new(0.1), Fail},

		{"choice accepted mass over scored mass", orgQ(), nil,
			Answer{Dist: map[string]float64{"sandbox": 0.4, "core": 0.4, "other": 0.1, "none": 0.1}}, new(0.8 / 0.9), Pass},
		{"choice rejected option fails", orgQ(), nil,
			Answer{Dist: map[string]float64{"sandbox": 0.1, "other": 0.85, "none": 0.05}}, new(0.1 / 0.95), Fail},
		{"choice skip at pass is skipped", orgQ(), nil,
			Answer{Dist: map[string]float64{"sandbox": 0.3, "none": 0.7}}, nil, Skipped},
		{"choice skip between is uncertain", orgQ(), nil,
			Answer{Dist: map[string]float64{"sandbox": 0.5, "none": 0.5}}, new(float64(1)), Uncertain},
		{"choice skip at fail is scored", orgQ(), nil,
			Answer{Dist: map[string]float64{"sandbox": 0.7, "none": 0.3}}, new(float64(1)), Pass},
		{"choice without skipOn scores none as scored", noSkip, nil,
			Answer{Dist: map[string]float64{"sandbox": 0.5, "none": 0.5}}, new(0.5), Uncertain},
		{"skip check uses the question band", func() Question { q := orgQ(); q.Threshold = strict; return q }(), strict,
			Answer{Dist: map[string]float64{"sandbox": 0.2, "none": 0.8}}, new(float64(1)), Uncertain},

		{"scale accepted levels pass", roundsQ(), nil,
			Answer{Dist: map[string]float64{"one": 0.5, "two": 0.3, "four-or-more": 0.2}}, new(0.8), Pass},
		{"scale four or more fails", roundsQ(), nil,
			Answer{Dist: map[string]float64{"three": 0.2, "four-or-more": 0.8}}, new(0.2), Fail},
		{"scale no pr is skipped", roundsQ(), nil,
			Answer{Dist: map[string]float64{"no-pr": 0.95, "one": 0.05}}, nil, Skipped},
		{"scale skip mass sums over every skipOn name",
			func() Question { q := roundsQ(); q.SkipOn = []string{"no-pr", "four-or-more"}; return q }(), nil,
			Answer{Dist: map[string]float64{"no-pr": 0.4, "four-or-more": 0.3, "one": 0.3}}, nil, Skipped},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			th := DefaultThreshold
			if tc.t != nil {
				th = *tc.t
			}
			r, err := scoreQuestion(tc.q, th, tc.a)
			if err != nil {
				t.Fatal(err)
			}
			if r.Verdict != tc.verdict {
				t.Errorf("verdict %s, want %s", r.Verdict, tc.verdict)
			}
			switch {
			case tc.wantP == nil && r.P != nil:
				t.Errorf("p %v, want empty", *r.P)
			case tc.wantP != nil && (r.P == nil || math.Abs(*r.P-*tc.wantP) > 1e-12):
				t.Errorf("p %v, want %v", r.P, *tc.wantP)
			}
		})
	}
}

func TestScoreQuestionBadAnswer(t *testing.T) {
	tests := []struct {
		name string
		q    Question
		a    Answer
	}{
		{"unknown type", Question{Name: "x", Type: "number"}, Answer{P: 1}},
		{"empty distribution", roundsQ(), Answer{}},
		{"names outside options only", roundsQ(), Answer{Dist: map[string]float64{"many": 1}}},
		{"a name outside options", orgQ(), Answer{Dist: map[string]float64{"core": 0.2, "bogus": 0.8}}},
		{"boolean above 1", boolQ("b", false), Answer{P: 1.4}},
		{"boolean below 0", boolQ("b", true), Answer{P: -0.1}},
		{"boolean NaN", boolQ("b", true), Answer{P: math.NaN()}},
		{"negative probability", roundsQ(), Answer{Dist: map[string]float64{"one": 1, "no-pr": -0.5}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := scoreQuestion(tc.q, DefaultThreshold, tc.a); !errors.Is(err, ErrBadAnswer) {
				t.Errorf("err %v, want ErrBadAnswer", err)
			}
		})
	}
}

func TestScore(t *testing.T) {
	spec := GraderSpec{
		AppliesWhen: "Did the requester ask for a new repository?",
		Questions:   []Question{boolQ("opened-pr", true), boolQ("pushed-main", false), roundsQ()},
	}
	skippedRounds := Answer{Dist: map[string]float64{"no-pr": 1}}
	passRounds := Answer{Dist: map[string]float64{"two": 1}}
	tests := []struct {
		name    string
		spec    GraderSpec
		applies *float64
		answers map[string]Answer
		status  Status
		verdict Verdict
		scored  int
	}{
		{"every question passes", spec, new(0.9),
			map[string]Answer{"opened-pr": {P: 0.9}, "pushed-main": {P: 0.1}, "rounds": passRounds}, Graded, Pass, 3},
		{"a skipped question does not stop a pass", spec, new(0.9),
			map[string]Answer{"opened-pr": {P: 0.9}, "pushed-main": {P: 0.1}, "rounds": skippedRounds}, Graded, Pass, 3},
		{"fail beats uncertain", spec, new(0.9),
			map[string]Answer{"opened-pr": {P: 0.1}, "pushed-main": {P: 0.5}, "rounds": passRounds}, Graded, Fail, 3},
		{"uncertain beats pass", spec, new(0.9),
			map[string]Answer{"opened-pr": {P: 0.9}, "pushed-main": {P: 0.5}, "rounds": passRounds}, Graded, Uncertain, 3},
		{"applies at pass is graded", spec, new(0.7),
			map[string]Answer{"opened-pr": {P: 0.9}, "pushed-main": {P: 0.1}, "rounds": passRounds}, Graded, Pass, 3},
		{"applies at fail is not applicable", spec, new(0.3), nil, NotApplicable, "", 0},
		{"applies between is applies_uncertain", spec, new(0.5), nil, AppliesUncertain, "", 0},
		{"applies uses the grader threshold", GraderSpec{Threshold: &Threshold{Pass: 0.95, Fail: 0.05}, AppliesWhen: "a?", Questions: spec.Questions},
			new(0.9), nil, AppliesUncertain, "", 0},
		{"no appliesWhen grades every run", GraderSpec{Questions: []Question{boolQ("opened-pr", true)}}, nil,
			map[string]Answer{"opened-pr": {P: 0.9}}, Graded, Pass, 1},
		{"a skipped question and a failed one fail", GraderSpec{Questions: []Question{roundsQ(), boolQ("opened-pr", true)}}, nil,
			map[string]Answer{"rounds": skippedRounds, "opened-pr": {P: 0.1}}, Graded, Fail, 2},
		{"skip check uses the question threshold", GraderSpec{Questions: []Question{
			func() Question { q := roundsQ(); q.Threshold = &Threshold{Pass: 0.9, Fail: 0.1}; return q }(),
		}}, nil, map[string]Answer{"rounds": {Dist: map[string]float64{"no-pr": 0.8, "two": 0.2}}}, Graded, Uncertain, 1},
		{"every question skipped is not applicable", GraderSpec{Questions: []Question{roundsQ()}}, nil,
			map[string]Answer{"rounds": skippedRounds}, NotApplicable, "", 1},
		{"grader threshold is the question default", GraderSpec{Threshold: &Threshold{Pass: 0.95, Fail: 0.05}, Questions: []Question{boolQ("opened-pr", true)}}, nil,
			map[string]Answer{"opened-pr": {P: 0.9}}, Graded, Uncertain, 1},
		{"question threshold overrides the grader", GraderSpec{
			Threshold: &Threshold{Pass: 0.95, Fail: 0.05},
			Questions: []Question{{Name: "q", Type: Boolean, Expect: true, Threshold: &Threshold{Pass: 0.8, Fail: 0.2}}},
		}, nil, map[string]Answer{"q": {P: 0.9}}, Graded, Pass, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := Score(tc.spec, tc.applies, tc.answers)
			if err != nil {
				t.Fatal(err)
			}
			if g.Status != tc.status || g.Verdict != tc.verdict || len(g.Questions) != tc.scored {
				t.Errorf("got %s %q with %d questions, want %s %q with %d", g.Status, g.Verdict, len(g.Questions), tc.status, tc.verdict, tc.scored)
			}
			for i, q := range g.Questions {
				if q.Question != tc.spec.Questions[i].Name {
					t.Errorf("question %d is %s, want spec order", i, q.Question)
				}
			}
		})
	}
}

func TestScoreErrors(t *testing.T) {
	spec := GraderSpec{AppliesWhen: "a?", Questions: []Question{boolQ("q", true)}}
	if _, err := Score(spec, nil, map[string]Answer{"q": {P: 1}}); !errors.Is(err, ErrMissingAnswer) {
		t.Errorf("no appliesWhen answer: err %v, want ErrMissingAnswer", err)
	}
	if _, err := Score(spec, new(float64(1)), nil); !errors.Is(err, ErrMissingAnswer) {
		t.Errorf("no question answer: err %v, want ErrMissingAnswer", err)
	}
	bad := GraderSpec{Questions: []Question{roundsQ()}}
	if _, err := Score(bad, nil, map[string]Answer{"rounds": {}}); !errors.Is(err, ErrBadAnswer) {
		t.Errorf("bad answer: err %v, want ErrBadAnswer", err)
	}
}
