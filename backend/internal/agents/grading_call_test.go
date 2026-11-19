package agents

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/runkora/kora/backend/internal/agents/grading"
	inference "github.com/runkora/kora/backend/internal/platform/clients/inference"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

// testGraderVersion is a normalized version with one question of each type.
func testGraderVersion(t *testing.T) graderVersion {
	t.Helper()
	spec, err := normalizeGrader(v1alpha1.GraderSpec{
		Agent: "github-self-service", Description: "Checks new repository requests.",
		Model:       v1alpha1.ModelRef{Family: "typesafe", Name: "jev-1-14"},
		AppliesWhen: "Did the requester ask for a new repository?",
		Questions: []v1alpha1.GraderQuestion{
			{Name: "opened-pr", Type: "boolean", Question: "Did the agent open a pull request?", Expect: new(true)},
			{Name: "pushed-main", Type: "boolean", Question: "Did the agent push to main?", Expect: new(false),
				Threshold: &v1alpha1.Threshold{Pass: 0.9, Fail: 0.1}},
			{Name: "organization", Type: "choice", Question: "Which organization?",
				Options: []v1alpha1.GraderAnswer{{Name: "sandbox", Description: "clear-route-sandbox"}, {Name: "core", Description: "clear-route"}},
				Accept:  []string{"sandbox", "core"}},
			{Name: "rounds", Type: "scale", Question: "How many question rounds?",
				Levels: []v1alpha1.GraderAnswer{{Name: "no-pr", Description: "No pull request was opened"}, {Name: "one", Description: "One round"}, {Name: "two", Description: "Two rounds"}},
				Accept: []string{"one"}, SkipOn: []string{"no-pr"}},
		},
	})
	if err != nil {
		t.Fatalf("normalizeGrader: %v", err)
	}
	_, raw, err := versionOf(spec)
	if err != nil {
		t.Fatalf("versionOf: %v", err)
	}
	v, err := parseGraderVersion(raw)
	if err != nil {
		t.Fatalf("parseGraderVersion: %v", err)
	}
	return v
}

// question is the named question of v, for a test to change.
func (v graderVersion) question(t *testing.T, name string) *v1alpha1.GraderQuestion {
	t.Helper()
	q := v.find(name)
	if q == nil {
		t.Fatalf("no question %q", name)
	}
	return q
}

// Each question type maps to its System One type: boolean to noul, choice to choice with each
// option's description including none, and scale to score with its levels lowest first.
func TestGraderVersion_DecisionQuestions(t *testing.T) {
	v := testGraderVersion(t)
	got := v.decisionQuestions()
	want := map[string]inference.Question{
		appliesKey:    {Type: inference.QuestionTypeNoul, Instructions: "Did the requester ask for a new repository?"},
		"opened-pr":   {Type: inference.QuestionTypeNoul, Instructions: "Did the agent open a pull request?"},
		"pushed-main": {Type: inference.QuestionTypeNoul, Instructions: "Did the agent push to main?"},
		"organization": {Type: inference.QuestionTypeChoice, Instructions: "Which organization?",
			Criteria: map[string]string{"sandbox": "clear-route-sandbox", "core": "clear-route", "none": noneOption.Description}},
		"rounds": {Type: inference.QuestionTypeScore, Instructions: "How many question rounds?",
			Levels: []string{"No pull request was opened", "One round", "Two rounds"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("questions = %+v\nwant %+v", got, want)
	}
	if v.modelName() != "typesafe/jev-1-14" {
		t.Errorf("model = %q", v.modelName())
	}

	// Without appliesWhen the request asks only the questions.
	v.AppliesWhen = ""
	if _, ok := v.decisionQuestions()[appliesKey]; ok {
		t.Error("a Grader with no appliesWhen asked it")
	}
}

// A score answer's probabilities are keyed by level index and read as level names.
func TestGraderVersion_ScoreInput(t *testing.T) {
	v := testGraderVersion(t)
	applies, got, err := v.scoreInput(map[string]inference.Answer{
		appliesKey:     {Noul: 0.95},
		"opened-pr":    {Noul: 0.9},
		"organization": {Choice: "core", Probabilities: map[string]float64{"core": 0.8, "none": 0.2}},
		"rounds":       {Score: 1.1, Probabilities: map[string]float64{"0": 0.05, "1": 0.8, "2": 0.15}},
	})
	if err != nil {
		t.Fatalf("scoreInput: %v", err)
	}
	if applies == nil || *applies != 0.95 {
		t.Errorf("applies = %v, want 0.95", applies)
	}
	want := map[string]grading.Answer{
		"opened-pr":    {P: 0.9},
		"organization": {Dist: map[string]float64{"core": 0.8, "none": 0.2}},
		"rounds":       {Dist: map[string]float64{"no-pr": 0.05, "one": 0.8, "two": 0.15}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("answers = %+v, want %+v", got, want)
	}

	for _, key := range []string{"3", "-1", "one"} {
		_, _, err := v.scoreInput(map[string]inference.Answer{"rounds": {Probabilities: map[string]float64{key: 1}}})
		if !errors.Is(err, grading.ErrBadAnswer) {
			t.Errorf("level key %q: err = %v, want ErrBadAnswer", key, err)
		}
	}
}

// A stored answer keeps the fields its type sets and reads back to the same grading input.
func TestStoredAnswer_RoundTrip(t *testing.T) {
	v := testGraderVersion(t)
	answers := map[string]inference.Answer{
		"opened-pr":    {Type: "noul", Noul: 0.12},
		"organization": {Type: "choice", Choice: "core", Probabilities: map[string]float64{"core": 0.7, "sandbox": 0.3}, Confidence: 0.7},
		"rounds":       {Type: "score", Score: 2.13, Legend: map[string]string{"0": "No pull request was opened"}, Probabilities: map[string]float64{"1": 0.4, "2": 0.6}, Confidence: 0.8},
	}
	back := map[string]inference.Answer{}
	for _, q := range v.Questions {
		a, ok := answers[q.Name]
		if !ok {
			continue
		}
		raw, err := storedAnswer(grading.QuestionType(q.Type), a)
		if err != nil {
			t.Fatalf("storedAnswer %s: %v", q.Name, err)
		}
		var got inference.Answer
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode %s: %v", q.Name, err)
		}
		back[q.Name] = got
	}
	if raw, _ := storedAnswer(grading.Boolean, answers["opened-pr"]); string(raw) != `{"type":"noul","noul":0.12}` {
		t.Errorf("boolean raw = %s", raw)
	}
	_, want, _ := v.scoreInput(answers)
	_, got, err := v.scoreInput(back)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("read back = %+v, %v, want %+v", got, err, want)
	}
}

// A threshold-only change gives the same thresholdless JSON. Any other change does not.
func TestRegradeKind(t *testing.T) {
	base := testGraderVersion(t)
	encode := func(v graderVersion) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	thresholds := base
	thresholds.Threshold = v1alpha1.Threshold{Pass: 0.8, Fail: 0.2}
	thresholds.Questions = append([]v1alpha1.GraderQuestion(nil), base.Questions...)
	thresholds.question(t, "opened-pr").Threshold = &v1alpha1.Threshold{Pass: 0.95, Fail: 0.05}
	question := base
	question.Questions = append([]v1alpha1.GraderQuestion(nil), base.Questions...)
	question.question(t, "opened-pr").Question = "Did the agent open a new pull request through its own tool calls?"
	model := base
	model.Model.Name = "jev-2"

	for name, tc := range map[string]struct {
		to   graderVersion
		want string
	}{
		"threshold": {thresholds, gradeJobRecompute},
		"question":  {question, gradeJobRegrade},
		"model":     {model, gradeJobRegrade},
	} {
		got, err := regradeKind(encode(base), encode(tc.to))
		if err != nil || got != tc.want {
			t.Errorf("%s: kind = %q, %v, want %q", name, got, err, tc.want)
		}
	}
}
