package agents

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/runkora/kora/backend/internal/agents/grading"
	inference "github.com/runkora/kora/backend/internal/platform/clients/inference"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

// The System One call of a Grader (docs/adr/2026-11-19-graders.md, Decision 6).

// appliesKey is appliesWhen's question id. A question name cannot hold an underscore, so it never collides.
const appliesKey = "applies_when"

// parseGraderVersion reads a stored grader_versions spec.
func parseGraderVersion(raw []byte) (graderVersion, error) {
	var v graderVersion
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("decode grader version: %w", err)
	}
	return v, nil
}

// thresholdless is the version's JSON with every threshold removed. Two versions with equal
// thresholdless JSON ask the same model the same questions, so their answers carry over.
func thresholdless(v graderVersion) ([]byte, error) {
	v.Threshold = v1alpha1.Threshold{}
	qs := make([]v1alpha1.GraderQuestion, len(v.Questions))
	for i, q := range v.Questions {
		q.Threshold = nil
		qs[i] = q
	}
	v.Questions = qs
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode thresholdless version: %w", err)
	}
	return b, nil
}

// modelName is the version's model as the catalog names it.
func (v graderVersion) modelName() string { return v.Model.Family + "/" + v.Model.Name }

// find returns the named question, or nil.
func (v graderVersion) find(name string) *v1alpha1.GraderQuestion {
	for i := range v.Questions {
		if v.Questions[i].Name == name {
			return &v.Questions[i]
		}
	}
	return nil
}

// gradingSpec is the version in the form grading.Score reads.
func (v graderVersion) gradingSpec() grading.GraderSpec {
	spec := grading.GraderSpec{
		Threshold:   grading.Threshold(v.Threshold),
		AppliesWhen: v.AppliesWhen,
		Questions:   make([]grading.Question, len(v.Questions)),
	}
	for i, q := range v.Questions {
		gq := grading.Question{Name: q.Name, Type: grading.QuestionType(q.Type), Accept: q.Accept, SkipOn: q.SkipOn}
		if q.Expect != nil {
			gq.Expect = *q.Expect
		}
		for _, a := range answersOf(q) {
			gq.Options = append(gq.Options, a.Name)
		}
		if q.Threshold != nil {
			gq.Threshold = new(grading.Threshold(*q.Threshold))
		}
		spec.Questions[i] = gq
	}
	return spec
}

// answersOf is a choice's options or a scale's levels.
func answersOf(q v1alpha1.GraderQuestion) []v1alpha1.GraderAnswer {
	if q.Type == string(grading.Scale) {
		return q.Levels
	}
	return q.Options
}

// decisionQuestions maps appliesWhen and every question to its System One question. A boolean
// is noul, a choice is choice with each option's description, and a scale is score with its
// level descriptions lowest first.
func (v graderVersion) decisionQuestions() map[string]inference.Question {
	out := make(map[string]inference.Question, len(v.Questions)+1)
	if v.AppliesWhen != "" {
		out[appliesKey] = inference.Question{Type: inference.QuestionTypeNoul, Instructions: v.AppliesWhen}
	}
	for _, q := range v.Questions {
		dq := inference.Question{Instructions: q.Question}
		switch grading.QuestionType(q.Type) {
		case grading.Boolean:
			dq.Type = inference.QuestionTypeNoul
		case grading.Choice:
			dq.Type, dq.Criteria = inference.QuestionTypeChoice, make(map[string]string, len(q.Options))
			for _, o := range q.Options {
				dq.Criteria[o.Name] = o.Description
			}
		case grading.Scale:
			dq.Type = inference.QuestionTypeScore
			for _, l := range q.Levels {
				dq.Levels = append(dq.Levels, l.Description)
			}
		}
		out[q.Name] = dq
	}
	return out
}

// storedAnswer is the part of a System One answer its question type sets. It is what
// grade_answers.raw holds, and a recompute reads it back as an inference.Answer.
func storedAnswer(qType grading.QuestionType, a inference.Answer) (json.RawMessage, error) {
	var v any
	switch qType {
	case grading.Boolean:
		v = struct {
			Type string  `json:"type"`
			Noul float64 `json:"noul"`
		}{inference.QuestionTypeNoul, a.Noul}
	case grading.Choice:
		v = struct {
			Type          string             `json:"type"`
			Choice        string             `json:"choice"`
			Probabilities map[string]float64 `json:"probabilities"`
			Confidence    float64            `json:"confidence"`
		}{inference.QuestionTypeChoice, a.Choice, a.Probabilities, a.Confidence}
	default:
		v = struct {
			Type          string             `json:"type"`
			Score         float64            `json:"score"`
			Legend        map[string]string  `json:"legend,omitempty"`
			Probabilities map[string]float64 `json:"probabilities"`
			Confidence    float64            `json:"confidence"`
		}{inference.QuestionTypeScore, a.Score, a.Legend, a.Probabilities, a.Confidence}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode answer: %w", err)
	}
	return b, nil
}

// scoreInput turns the model's answers into grading.Score's input. A score answer's
// probabilities are keyed by level index from 0 and become level names here.
func (v graderVersion) scoreInput(answers map[string]inference.Answer) (*float64, map[string]grading.Answer, error) {
	var applies *float64
	if a, ok := answers[appliesKey]; ok && v.AppliesWhen != "" {
		applies = new(a.Noul)
	}
	out := make(map[string]grading.Answer, len(v.Questions))
	for _, q := range v.Questions {
		a, ok := answers[q.Name]
		if !ok {
			continue
		}
		switch grading.QuestionType(q.Type) {
		case grading.Boolean:
			out[q.Name] = grading.Answer{P: a.Noul}
		case grading.Choice:
			out[q.Name] = grading.Answer{Dist: a.Probabilities}
		case grading.Scale:
			dist, err := levelDist(q, a.Probabilities)
			if err != nil {
				return nil, nil, err
			}
			out[q.Name] = grading.Answer{Dist: dist}
		}
	}
	return applies, out, nil
}

// levelDist rekeys a score answer's probabilities from level index, counted from 0, to level name.
func levelDist(q v1alpha1.GraderQuestion, probs map[string]float64) (map[string]float64, error) {
	dist := make(map[string]float64, len(probs))
	for k, p := range probs {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= len(q.Levels) {
			return nil, fmt.Errorf("question %s: %w: level %q", q.Name, grading.ErrBadAnswer, k)
		}
		dist[q.Levels[i].Name] = p
	}
	return dist, nil
}
