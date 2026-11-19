package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

// Grader is a declared Grader. Graders are written only through the operator, so every row is
// projected from a resource (docs/adr/2026-11-19-graders.md).
type Grader struct {
	ID          string
	TenantID    string
	Name        string
	AgentTypeID string
	// AgentSlug is read from the agent's row for events. Writes ignore it.
	AgentSlug   string
	Description string
	// MinPassRate is a percent. Nil means the Grader has no minimum.
	MinPassRate *int
	// CurrentVersion is the grader_versions row the Grader grades with.
	CurrentVersion     string
	ResourceUID        string
	ResourceGeneration int64
	Repository         string
}

// ErrInvalidGrader is wrapped by a Grader spec that fails the apply rules.
var ErrInvalidGrader = errors.New("invalid grader")

// noneOption is the option kora adds to every choice, so a run with no listed answer is answerable.
var noneOption = v1alpha1.GraderAnswer{Name: "none", Description: "None of the other options."}

// defaultThreshold is the CRD default. A spec sent without one hashes the same as one with it.
var defaultThreshold = v1alpha1.Threshold{Pass: 0.7, Fail: 0.3}

// graderVersion is the part of a Grader spec that changes its verdicts. Its JSON is the version
// hash input and the stored version spec. Description and minPassRate are left out.
type graderVersion struct {
	Model       v1alpha1.ModelRef         `json:"model"`
	Threshold   v1alpha1.Threshold        `json:"threshold"`
	AppliesWhen string                    `json:"appliesWhen,omitempty"`
	Questions   []v1alpha1.GraderQuestion `json:"questions"`
}

// versionOf returns the version hash of a normalized spec and the JSON it hashes. Struct fields
// marshal in declaration order, so equal specs give equal bytes.
func versionOf(spec v1alpha1.GraderSpec) (string, []byte, error) {
	raw, err := json.Marshal(graderVersion{Model: spec.Model, Threshold: spec.Threshold, AppliesWhen: spec.AppliesWhen, Questions: spec.Questions})
	if err != nil {
		return "", nil, fmt.Errorf("encode grader version: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}

// parseMinPassRate reads a percent such as "90%". An empty value is nil.
func parseMinPassRate(s string) (*int, error) {
	if s == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(strings.TrimSuffix(s, "%"))
	if err != nil || !strings.HasSuffix(s, "%") || n < 0 || n > 100 {
		return nil, fmt.Errorf("%w: minPassRate %q is not a percent from 0%% to 100%%", ErrInvalidGrader, s)
	}
	return &n, nil
}

// normalizeGrader checks the apply rules the CRD's CEL leaves to kora and puts the spec in the
// form the version hashes: each choice gets the none option, accept and skipOn are sorted sets,
// and a question threshold equal to the Grader's is dropped. The input spec is not changed.
func normalizeGrader(spec v1alpha1.GraderSpec) (v1alpha1.GraderSpec, error) {
	if spec.Threshold == (v1alpha1.Threshold{}) {
		spec.Threshold = defaultThreshold
	}
	questions := make([]v1alpha1.GraderQuestion, len(spec.Questions))
	seen := map[string]bool{}
	for i, q := range spec.Questions {
		if seen[q.Name] {
			return spec, fmt.Errorf("%w: question name %q is used twice", ErrInvalidGrader, q.Name)
		}
		seen[q.Name] = true
		nq, err := normalizeQuestion(q)
		if err != nil {
			return spec, fmt.Errorf("%w: question %q: %w", ErrInvalidGrader, q.Name, err)
		}
		// A question band equal to the Grader's judges the same, so it hashes as unset.
		if nq.Threshold != nil && *nq.Threshold == spec.Threshold {
			nq.Threshold = nil
		}
		questions[i] = nq
	}
	spec.Questions = questions
	return spec, nil
}

func normalizeQuestion(q v1alpha1.GraderQuestion) (v1alpha1.GraderQuestion, error) {
	if q.Type == "boolean" {
		return q, nil
	}
	answers, noun := q.Levels, "level"
	if q.Type == "choice" {
		answers, noun = q.Options, "option"
	}
	names := map[string]bool{}
	for _, a := range answers {
		if names[a.Name] {
			return q, fmt.Errorf("%s name %q is used twice", noun, a.Name)
		}
		names[a.Name] = true
	}
	if q.Type == "choice" && !names[noneOption.Name] {
		q.Options = append(slices.Clone(q.Options), noneOption)
		names[noneOption.Name] = true
	}
	accept, err := answerSet(q.Accept, names, "accept", noun)
	if err != nil {
		return q, err
	}
	skip, err := answerSet(q.SkipOn, names, "skipOn", noun)
	if err != nil {
		return q, err
	}
	if err := checkAnswerSets(q.Type, noun, len(names), accept, skip); err != nil {
		return q, err
	}
	// accept and skipOn are sets. Their order must not change the version.
	q.Accept, q.SkipOn = sortedNames(accept), sortedNames(skip)
	return q, nil
}

// sortedNames returns the names of set in order, or nil for an empty set.
func sortedNames(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(set))
}

// checkAnswerSets checks accept and skipOn against each other and against the count of answers.
func checkAnswerSets(qType, noun string, answers int, accept, skip map[string]bool) error {
	for name := range accept {
		if skip[name] {
			return fmt.Errorf("%q is in both accept and skipOn", name)
		}
	}
	switch {
	case len(skip) == answers:
		return fmt.Errorf("skipOn lists every %s", noun)
	case qType == "scale" && len(skip) == 0:
		return errors.New("a scale needs a level for runs where the question has no subject, listed in skipOn")
	case qType == "choice" && len(accept) == answers:
		return errors.New("accept lists every option, including none")
	}
	return nil
}

// answerSet returns the distinct names of list, each of which must name an option or level.
func answerSet(list []string, names map[string]bool, field, noun string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, name := range list {
		if !names[name] {
			return nil, fmt.Errorf("%s names %q, which is not one of the %ss", field, name, noun)
		}
		out[name] = true
	}
	return out, nil
}
