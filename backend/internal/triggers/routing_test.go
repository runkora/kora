package triggers

import (
	"errors"
	"strings"
	"testing"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	inferenceclient "github.com/runkora/kora/backend/internal/platform/clients/inference"
)

func routingReq(mutate func(*RoutingConfig)) *RoutingConfig {
	r := &RoutingConfig{Candidates: []string{"at-a", "at-b", "at-c"}, Model: "liquid/d1"}
	if mutate != nil {
		mutate(r)
	}
	return r
}

func TestNormalizeRouting(t *testing.T) {
	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "at-" + strings.Repeat("x", i+1)
		}
		return out
	}
	tests := []struct {
		name    string
		in      *RoutingConfig
		wantErr string
	}{
		{name: "missing", in: nil, wantErr: "routing is required"},
		{name: "one candidate", in: routingReq(func(r *RoutingConfig) { r.Candidates = []string{"at-a"} }), wantErr: "between 2 and 20"},
		{name: "twenty one candidates", in: routingReq(func(r *RoutingConfig) { r.Candidates = ids(21) }), wantErr: "between 2 and 20"},
		{name: "twenty candidates", in: routingReq(func(r *RoutingConfig) { r.Candidates = ids(20) })},
		{name: "duplicate candidate", in: routingReq(func(r *RoutingConfig) { r.Candidates = []string{"at-a", "at-a"} }), wantErr: "more than once"},
		{name: "empty candidate", in: routingReq(func(r *RoutingConfig) { r.Candidates = []string{"at-a", " "} }), wantErr: "empty id"},
		{name: "fallback outside candidates", in: routingReq(func(r *RoutingConfig) { r.FallbackAgentTypeID = new("at-z") }), wantErr: "must be one of the candidates"},
		{name: "fallback candidate", in: routingReq(func(r *RoutingConfig) { r.FallbackAgentTypeID = new("at-b") })},
		{name: "bad confidence", in: routingReq(func(r *RoutingConfig) { r.Confidence = "certain" }), wantErr: "routing.confidence"},
		{name: "model without family", in: routingReq(func(r *RoutingConfig) { r.Model = "d1" }), wantErr: "routing.model"},
		{name: "join key too long", in: routingReq(func(r *RoutingConfig) { r.JoinKey = strings.Repeat("k", 257) }), wantErr: "256 bytes"},
		{name: "join key bad field", in: routingReq(func(r *RoutingConfig) { r.JoinKey = "{{ meta.nope }}" }), wantErr: "not a meta field"},
		{name: "join key", in: routingReq(func(r *RoutingConfig) { r.JoinKey = "{{ repository.full_name }}#{{ issue.number }}" })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeRouting(tt.in)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr) || !errors.Is(err, ErrValidation)):
				t.Fatalf("error = %v, want a validation error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeRouting_Defaults(t *testing.T) {
	r, err := normalizeRouting(routingReq(nil))
	if err != nil {
		t.Fatalf("normalizeRouting: %v", err)
	}
	if r.Confidence != ConfidenceBalanced || r.threshold() != 0.70 || !r.joins() || r.FallbackAgentTypeID != nil {
		t.Errorf("defaults = %+v", r)
	}
}

func TestConfidenceThresholds(t *testing.T) {
	for preset, want := range map[string]float64{ConfidenceStrict: 0.85, ConfidenceBalanced: 0.70, ConfidenceLoose: 0.50} {
		if got := (&RoutingConfig{Confidence: preset}).threshold(); got != want {
			t.Errorf("%s threshold = %v, want %v", preset, got, want)
		}
	}
}

// The model's use scope is the one the inference gateway checks for a catalog
// name: family and name from "<family>/<name>".
func TestModelUseScope(t *testing.T) {
	for model, want := range map[string]string{
		"liquid/d1":                    "inference:liquid:d1:use",
		"perplexity/pplx-decider-v1-2": "inference:perplexity:pplx-decider-v1-2:use",
	} {
		got, err := modelUseScope(model)
		if err != nil || got != want {
			t.Errorf("modelUseScope(%q) = %q, %v; want %q", model, got, err, want)
		}
	}
	for _, bad := range []string{"", "d1", "/d1", "liquid/", "liquid/d1:beta"} {
		if _, err := modelUseScope(bad); !errors.Is(err, ErrValidation) {
			t.Errorf("modelUseScope(%q) = %v, want a validation error", bad, err)
		}
	}
}

func TestRoutedGrantScopes(t *testing.T) {
	r := routingReq(func(r *RoutingConfig) { r.Candidates = []string{"at-a", "at-b"} })
	r.candidateSlugs = map[string]string{"at-a": "coder", "at-b": "reviewer"}
	got, err := routedGrantScopes(r)
	if err != nil {
		t.Fatalf("routedGrantScopes: %v", err)
	}
	want := []string{
		"agents:runs:coder:read", "agents:runs:coder:write",
		"agents:runs:reviewer:read", "agents:runs:reviewer:write",
		"inference:invoke", "inference:liquid:d1:use",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("scopes = %v, want %v", got, want)
	}
}

func TestRenderJoinKey(t *testing.T) {
	fields := map[string]string{"repository.full_name": "acme/widgets", "issue.number": "7", "long": strings.Repeat("x", 257)}
	tests := []struct {
		name, tmpl, want string
		ok               bool
	}{
		{name: "no key", tmpl: ""},
		{name: "renders", tmpl: "{{ repository.full_name }}#{{ issue.number }}", want: "acme/widgets#7", ok: true},
		{name: "empty render", tmpl: " {{ missing }} "},
		{name: "too long", tmpl: "{{ long }}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := renderJoinKey(&RoutingConfig{JoinKey: tt.tmpl}, fields)
			if got != tt.want || ok != tt.ok {
				t.Errorf("renderJoinKey = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestBuildRoutingQuestions(t *testing.T) {
	agents := []agentsclient.AgentType{
		{ID: "at-a", Name: "Coder", Description: "Implements changes."},
		{ID: "at-b", Name: "Reviewer"},
	}
	runs := []agentsclient.Run{{ID: "run-9", AgentTypeID: "at-b", Summary: new("Reviewing PR 12")}}
	q := buildRoutingQuestions("liquid/d1", "the prompt", agents, runs)

	agentQ := q.request.Questions[questionAgent]
	if agentQ.Criteria["a1"] != "Coder: Implements changes." || agentQ.Criteria["a2"] != "Reviewer" || q.agents["a2"] != "at-b" {
		t.Errorf("agent question = %+v, ids %v", agentQ, q.agents)
	}
	runQ, ok := q.request.Questions[questionRun]
	if !ok || runQ.Criteria[runOptionNew] == "" || !strings.Contains(runQ.Criteria["r1"], "Reviewer") ||
		!strings.Contains(runQ.Criteria["r1"], "Reviewing PR 12") || q.runs["r1"].ID != "run-9" {
		t.Errorf("run question = %+v", runQ)
	}
	if !strings.Contains(agentQ.Instructions, "data") || q.request.State.(routingState).Message != "the prompt" {
		t.Errorf("instructions or state wrong: %q, %+v", agentQ.Instructions, q.request.State)
	}

	if _, ok := buildRoutingQuestions("liquid/d1", "p", agents, nil).request.Questions[questionRun]; ok {
		t.Error("the run question must be omitted when no run is joinable")
	}
}

// decisionAnswers builds an answer to the agent question and, when runP is
// set, the run question, from the probability of the first option.
func decisionAnswers(agentP float64, runChoice string, runP float64) inferenceclient.SystemOneResponse {
	resp := inferenceclient.SystemOneResponse{Answers: map[string]inferenceclient.Answer{
		questionAgent: {Choice: "a1", Probabilities: map[string]float64{"a1": agentP, "a2": 1 - agentP}, Confidence: agentP},
	}}
	if runChoice != "" {
		resp.Answers[questionRun] = inferenceclient.Answer{Choice: runChoice, Probabilities: map[string]float64{runChoice: runP}, Confidence: runP}
	}
	return resp
}

// ADR Decision 9: a run above the threshold, else an agent above it, else the fallback.
func TestDecideRoute_OutcomeOrder(t *testing.T) {
	agents := []agentsclient.AgentType{{ID: "at-a", Name: "A"}, {ID: "at-b", Name: "B"}}
	runs := []agentsclient.Run{{ID: "run-1", AgentTypeID: "at-b"}}
	q := buildRoutingQuestions("liquid/d1", "p", agents, runs)
	fallback := new("at-b")

	tests := []struct {
		name        string
		resp        inferenceclient.SystemOneResponse
		fallback    *string
		wantOutcome string
		wantAgent   string
		wantRun     string
		wantReason  string
	}{
		{name: "run above threshold", resp: decisionAnswers(0.9, "r1", 0.8), wantOutcome: RouteOutcomeJoinedRun, wantAgent: "at-b", wantRun: "run-1"},
		{name: "run below, agent above", resp: decisionAnswers(0.9, "r1", 0.6), wantOutcome: RouteOutcomeNewRun, wantAgent: "at-a"},
		{name: "new run chosen", resp: decisionAnswers(0.75, runOptionNew, 0.95), wantOutcome: RouteOutcomeNewRun, wantAgent: "at-a"},
		{name: "both below, fallback agent", resp: decisionAnswers(0.6, "r1", 0.5), fallback: fallback, wantOutcome: RouteOutcomeFallbackRun, wantAgent: "at-b", wantReason: FallbackBelowThreshold},
		{name: "both below, no fallback", resp: decisionAnswers(0.6, "r1", 0.5), wantOutcome: RouteOutcomeNoRun, wantReason: FallbackBelowThreshold},
		{name: "at the threshold", resp: decisionAnswers(0.7, "", 0), wantOutcome: RouteOutcomeNewRun, wantAgent: "at-a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decideRoute(q, tt.resp, 0.7, tt.fallback)
			if err != nil {
				t.Fatalf("decideRoute: %v", err)
			}
			if got.outcome != tt.wantOutcome || got.agentTypeID != tt.wantAgent || got.run.ID != tt.wantRun || got.fallbackReason != tt.wantReason {
				t.Errorf("choice = %+v, want %s agent=%q run=%q reason=%q", got, tt.wantOutcome, tt.wantAgent, tt.wantRun, tt.wantReason)
			}
			if got.scores.Agents["at-a"] == 0 {
				t.Errorf("scores are keyed by agent type id: %+v", got.scores)
			}
		})
	}
}

func TestDecideRoute_NoAgentAnswer(t *testing.T) {
	q := buildRoutingQuestions("liquid/d1", "p", []agentsclient.AgentType{{ID: "at-a"}, {ID: "at-b"}}, nil)
	resp := inferenceclient.SystemOneResponse{Answers: map[string]inferenceclient.Answer{questionAgent: {Choice: "a9"}}}
	if _, err := decideRoute(q, resp, 0.7, nil); !errors.Is(err, errNoAgentAnswer) {
		t.Errorf("err = %v, want errNoAgentAnswer", err)
	}
}
