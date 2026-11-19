package agents

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

func answers(names ...string) []v1alpha1.GraderAnswer {
	out := make([]v1alpha1.GraderAnswer, len(names))
	for i, n := range names {
		out[i] = v1alpha1.GraderAnswer{Name: n, Description: strings.ToUpper(n)}
	}
	return out
}

func choice(name string, options, accept, skipOn []string) v1alpha1.GraderQuestion {
	return v1alpha1.GraderQuestion{Name: name, Type: "choice", Question: "Which?", Options: answers(options...), Accept: accept, SkipOn: skipOn}
}

func scale(name string, levels, accept, skipOn []string) v1alpha1.GraderQuestion {
	return v1alpha1.GraderQuestion{Name: name, Type: "scale", Question: "How many?", Levels: answers(levels...), Accept: accept, SkipOn: skipOn}
}

func boolean(name string) v1alpha1.GraderQuestion {
	return v1alpha1.GraderQuestion{Name: name, Type: "boolean", Question: "Did it?", Expect: new(true)}
}

func graderSpec(questions ...v1alpha1.GraderQuestion) v1alpha1.GraderSpec {
	return v1alpha1.GraderSpec{
		Agent: "reviewer", Description: "Checks reviews.", Model: v1alpha1.ModelRef{Family: "typesafe", Name: "jev"},
		Threshold: v1alpha1.Threshold{Pass: 0.7, Fail: 0.3}, Questions: questions,
	}
}

func TestNormalizeGrader(t *testing.T) {
	l := []string{"no-pr", "one", "two"}
	cases := []struct {
		name    string
		q       []v1alpha1.GraderQuestion
		wantErr string
	}{
		{"valid", []v1alpha1.GraderQuestion{boolean("a"), choice("b", []string{"x", "y"}, []string{"x"}, nil), scale("c", l, []string{"one"}, []string{"no-pr"})}, ""},
		{"duplicate question", []v1alpha1.GraderQuestion{boolean("a"), boolean("a")}, `question name "a" is used twice`},
		{"duplicate option", []v1alpha1.GraderQuestion{choice("b", []string{"x", "x"}, []string{"x"}, nil)}, `option name "x" is used twice`},
		{"duplicate level", []v1alpha1.GraderQuestion{scale("c", []string{"no-pr", "no-pr"}, []string{"no-pr"}, nil)}, `level name "no-pr" is used twice`},
		{"accept names no option", []v1alpha1.GraderQuestion{choice("b", []string{"x"}, []string{"z"}, nil)}, `accept names "z", which is not one of the options`},
		{"skipOn names no level", []v1alpha1.GraderQuestion{scale("c", l, []string{"one"}, []string{"zero"})}, `skipOn names "zero", which is not one of the levels`},
		{"accept may name none", []v1alpha1.GraderQuestion{choice("b", []string{"x", "y"}, []string{"none"}, nil)}, ""},
		{"accept and skipOn", []v1alpha1.GraderQuestion{choice("b", []string{"x", "y"}, []string{"x"}, []string{"x"})}, `"x" is in both accept and skipOn`},
		{"skipOn none", []v1alpha1.GraderQuestion{choice("b", []string{"x"}, []string{"x"}, []string{"none"})}, ""},
		{"skipOn all but one", []v1alpha1.GraderQuestion{choice("b", []string{"x", "y"}, []string{"x"}, []string{"y", "none"})}, ""},
		{"scale without skipOn", []v1alpha1.GraderQuestion{scale("c", l, []string{"one"}, nil)}, "a scale needs a level"},
		{"accept every option and none", []v1alpha1.GraderQuestion{choice("b", []string{"x", "y"}, []string{"x", "y", "none"}, nil)}, "accept lists every option, including none"},
		{"accept every declared option", []v1alpha1.GraderQuestion{choice("b", []string{"x", "y"}, []string{"x", "y"}, nil)}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeGrader(graderSpec(tc.q...))
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("err = %v, want none", err)
			case tc.wantErr != "" && (err == nil || !errors.Is(err, ErrInvalidGrader) || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("err = %v, want ErrInvalidGrader with %q", err, tc.wantErr)
			}
		})
	}
}

// skipOn may not list every answer. Separate cases keep accept off the skipped names.
func TestNormalizeGraderSkipOnEveryAnswer(t *testing.T) {
	for _, q := range []v1alpha1.GraderQuestion{
		{Name: "c", Type: "scale", Levels: answers("a", "b"), SkipOn: []string{"a", "b"}},
		{Name: "b", Type: "choice", Options: answers("x"), SkipOn: []string{"x", "none"}},
	} {
		if _, err := normalizeGrader(graderSpec(q)); err == nil || !strings.Contains(err.Error(), "skipOn lists every") {
			t.Errorf("%s: err = %v, want skipOn lists every", q.Type, err)
		}
	}
}

func TestNormalizeGraderAddsNone(t *testing.T) {
	in := graderSpec(choice("b", []string{"x"}, []string{"x"}, nil), choice("own", []string{"x", "none"}, []string{"x"}, nil))
	in.Threshold = v1alpha1.Threshold{}
	out, err := normalizeGrader(in)
	if err != nil {
		t.Fatal(err)
	}
	names := func(q v1alpha1.GraderQuestion) []string {
		var n []string
		for _, o := range q.Options {
			n = append(n, o.Name)
		}
		return n
	}
	if got := names(out.Questions[0]); !slices.Equal(got, []string{"x", "none"}) {
		t.Errorf("options = %v, want none added", got)
	}
	if got := names(out.Questions[1]); !slices.Equal(got, []string{"x", "none"}) {
		t.Errorf("options = %v, want the declared none kept once", got)
	}
	if len(in.Questions[0].Options) != 1 {
		t.Error("the input spec was changed")
	}
	if out.Threshold != defaultThreshold {
		t.Errorf("threshold = %+v, want the default", out.Threshold)
	}
}

func TestParseMinPassRate(t *testing.T) {
	for in, want := range map[string]int{"0%": 0, "90%": 90, "100%": 100} {
		if got, err := parseMinPassRate(in); err != nil || *got != want {
			t.Errorf("%q = %v, %v", in, got, err)
		}
	}
	if got, err := parseMinPassRate(""); got != nil || err != nil {
		t.Errorf("empty = %v, %v", got, err)
	}
	for _, in := range []string{"90", "101%", "-1%", "x%"} {
		if _, err := parseMinPassRate(in); !errors.Is(err, ErrInvalidGrader) {
			t.Errorf("%q: err = %v", in, err)
		}
	}
}

func TestGraderVersion(t *testing.T) {
	base := graderSpec(boolean("a"), choice("b", []string{"x"}, []string{"x"}, nil))
	version := func(s v1alpha1.GraderSpec) string {
		t.Helper()
		n, err := normalizeGrader(s)
		if err != nil {
			t.Fatal(err)
		}
		v, _, err := versionOf(n)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v := version(base)
	if len(v) != 64 || version(base) != v {
		t.Fatalf("version %q is not a stable sha256", v)
	}
	same := base
	same.Description, same.MinPassRate, same.Agent = "Other words.", "50%", "other"
	unset := base
	unset.Threshold = v1alpha1.Threshold{}
	reordered := graderSpec(boolean("a"), choice("b", []string{"x", "y"}, []string{"y", "x"}, nil))
	sorted := graderSpec(boolean("a"), choice("b", []string{"x", "y"}, []string{"x", "y"}, nil))
	if version(reordered) != version(sorted) {
		t.Error("reordering accept changed the version")
	}
	skipA := graderSpec(scale("s", []string{"n", "m", "y"}, []string{"y"}, []string{"n", "m"}))
	skipB := graderSpec(scale("s", []string{"n", "m", "y"}, []string{"y"}, []string{"m", "n"}))
	if version(skipA) != version(skipB) {
		t.Error("reordering skipOn changed the version")
	}
	questionsReversed := graderSpec(choice("b", []string{"x"}, []string{"x"}, nil), boolean("a"))
	if version(questionsReversed) != v {
		t.Error("reordering questions changed the version")
	}
	equalBand := graderSpec(boolean("a"), choice("b", []string{"x"}, []string{"x"}, nil))
	equalBand.Questions[0].Threshold = &v1alpha1.Threshold{Pass: 0.7, Fail: 0.3}
	for name, s := range map[string]v1alpha1.GraderSpec{
		"description, minPassRate and agent": same, "an unset threshold": unset, "a question threshold equal to the Grader's": equalBand,
	} {
		if version(s) != v {
			t.Errorf("%s changed the version", name)
		}
	}

	changed := map[string]func(*v1alpha1.GraderSpec){
		"model":       func(s *v1alpha1.GraderSpec) { s.Model.Name = "jev-2" },
		"threshold":   func(s *v1alpha1.GraderSpec) { s.Threshold.Pass = 0.8 },
		"appliesWhen": func(s *v1alpha1.GraderSpec) { s.AppliesWhen = "Was it a review?" },
		"question":    func(s *v1alpha1.GraderSpec) { s.Questions[0].Question = "Did it, really?" },
		"expect":      func(s *v1alpha1.GraderSpec) { s.Questions[0].Expect = new(false) },
		"question threshold": func(s *v1alpha1.GraderSpec) {
			s.Questions[0].Threshold = &v1alpha1.Threshold{Pass: 0.9, Fail: 0.1}
		},
	}
	for name, change := range changed {
		s := graderSpec(boolean("a"), choice("b", []string{"x"}, []string{"x"}, nil))
		change(&s)
		if version(s) == v {
			t.Errorf("a %s change kept the version", name)
		}
	}

	n, _ := normalizeGrader(base)
	_, raw, _ := versionOf(n)
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["description"]; ok || len(stored) != 3 {
		t.Errorf("version spec = %s, want model, threshold and questions only", raw)
	}
}

// fakeGraderStore holds Graders by name and counts writes.
type fakeGraderStore struct {
	agents   map[string]*AgentType // by slug
	graders  map[string]Grader
	versions map[string][]byte
	writes   int
}

func newFakeGraderStore() *fakeGraderStore {
	return &fakeGraderStore{
		agents:   map[string]*AgentType{"reviewer": {ID: "agent-1", TenantID: applierTenant, Slug: "reviewer"}},
		graders:  map[string]Grader{},
		versions: map[string][]byte{},
	}
}

func (f *fakeGraderStore) GetAgentTypeBySlug(_ context.Context, _, slug string) (*AgentType, error) {
	if at, ok := f.agents[slug]; ok {
		return at, nil
	}
	return nil, ErrNotFound
}

func (f *fakeGraderStore) GetGraderByName(_ context.Context, _, name string) (*Grader, error) {
	if g, ok := f.graders[name]; ok {
		return &g, nil
	}
	return nil, ErrNotFound
}

func (f *fakeGraderStore) PutGrader(_ context.Context, g Grader, spec []byte) (*Grader, error) {
	f.writes++
	if old, ok := f.graders[g.Name]; ok {
		g.ID = old.ID
	} else {
		g.ID = "grader-" + g.Name
	}
	f.graders[g.Name] = g
	f.versions[g.CurrentVersion] = spec
	return &g, nil
}

func (f *fakeGraderStore) DeleteGrader(_ context.Context, _, name string) error {
	delete(f.graders, name)
	return nil
}

func (f *fakeGraderStore) ListDeclaredGraders(context.Context, string) ([]declarative.Declared, error) {
	var out []declarative.Declared
	for _, g := range f.graders {
		out = append(out, declarative.Declared{Name: g.Name, UID: g.ResourceUID})
	}
	return out, nil
}

func TestGraderApplier(t *testing.T) {
	ctx := context.Background()
	store := newFakeGraderStore()
	a := NewGraderApplier(store)
	refs := map[string]string{declarative.RefAgent("reviewer"): "agent-1"}
	spec := graderSpec(boolean("a"), choice("b", []string{"x"}, []string{"x"}, nil))
	spec.MinPassRate = "90%"

	res, err := a.Apply(ctx, item(t, "reviews", "uid-1", 1, spec, refs))
	if err != nil || res.Outcome != declarative.OutcomeProjected || !res.Ready || res.ID != "grader-reviews" {
		t.Fatalf("create = %+v, %v", res, err)
	}
	g := store.graders["reviews"]
	if g.AgentTypeID != "agent-1" || g.Description != "Checks reviews." || *g.MinPassRate != 90 || g.ResourceUID != "uid-1" || g.ResourceGeneration != 1 {
		t.Errorf("row = %+v", g)
	}
	if !strings.Contains(string(store.versions[g.CurrentVersion]), `"name":"none"`) {
		t.Errorf("version spec = %s, want the none option", store.versions[g.CurrentVersion])
	}

	if _, err := a.Apply(ctx, item(t, "reviews", "uid-1", 1, spec, refs)); err != nil || store.writes != 1 {
		t.Errorf("an unchanged generation was written again: %d writes, %v", store.writes, err)
	}

	// A description change is a write with the same version.
	spec.Description = "Checks pull request reviews."
	if _, err := a.Apply(ctx, item(t, "reviews", "uid-1", 2, spec, refs)); err != nil || store.graders["reviews"].CurrentVersion != g.CurrentVersion || store.writes != 2 {
		t.Errorf("description change: %v, row %+v", err, store.graders["reviews"])
	}

	// A recreated Agent has a new id. The Grader follows it at the same generation.
	store.agents["reviewer"] = &AgentType{ID: "agent-2", TenantID: applierTenant, Slug: "reviewer"}
	if res, _ := a.Apply(ctx, item(t, "reviews", "uid-1", 2, spec, refs)); res.Outcome != declarative.OutcomePending {
		t.Errorf("a stale agent ref = %+v, want Pending", res)
	}
	refs[declarative.RefAgent("reviewer")] = "agent-2"
	if res, err := a.Apply(ctx, item(t, "reviews", "uid-1", 2, spec, refs)); err != nil || res.ID != "grader-reviews" || store.graders["reviews"].AgentTypeID != "agent-2" {
		t.Errorf("recreated agent = %+v, %v, row %+v", res, err, store.graders["reviews"])
	}

	if got, _ := a.List(ctx, applierTenant); !slices.Equal(got, []declarative.Declared{{Name: "reviews", UID: "uid-1"}}) {
		t.Errorf("list = %+v", got)
	}
	if err := a.Delete(ctx, applierTenant, "reviews"); err != nil || len(store.graders) != 0 {
		t.Errorf("delete: %v, graders %v", err, store.graders)
	}
}

func TestGraderApplierRefusals(t *testing.T) {
	ctx := context.Background()
	store := newFakeGraderStore()
	a := NewGraderApplier(store)
	refs := map[string]string{declarative.RefAgent("reviewer"): "agent-1"}

	invalid := graderSpec(boolean("a"), boolean("a"))
	if res, _ := a.Apply(ctx, item(t, "g", "u", 1, invalid, refs)); res.Outcome != declarative.OutcomeInvalid || !strings.Contains(res.Message, "used twice") {
		t.Errorf("invalid = %+v", res)
	}
	rate := graderSpec(boolean("a"))
	rate.MinPassRate = "120%"
	if res, _ := a.Apply(ctx, item(t, "g", "u", 1, rate, refs)); res.Outcome != declarative.OutcomeInvalid {
		t.Errorf("minPassRate = %+v", res)
	}
	if res, _ := a.Apply(ctx, declarative.Item{TenantID: applierTenant, Name: "g", UID: "u", Spec: json.RawMessage(`[]`)}); res.Outcome != declarative.OutcomeInvalid {
		t.Errorf("undecodable spec = %+v", res)
	}
	unknown := graderSpec(boolean("a"))
	unknown.Agent = "ghost"
	if res, _ := a.Apply(ctx, item(t, "g", "u", 1, unknown, map[string]string{declarative.RefAgent("ghost"): "x"})); res.Outcome != declarative.OutcomePending {
		t.Errorf("unknown agent = %+v", res)
	}
	if res, _ := a.Apply(ctx, item(t, "g", "u", 1, graderSpec(boolean("a")), nil)); res.Outcome != declarative.OutcomePending {
		t.Errorf("no ref = %+v", res)
	}
	if store.writes != 0 {
		t.Errorf("%d writes from refused specs", store.writes)
	}
}
