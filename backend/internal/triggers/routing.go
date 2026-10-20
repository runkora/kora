package triggers

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Confidence presets a routed trigger stores. The thresholds behind them live in
// code (trigger routing ADR, Decision 8).
const (
	ConfidenceStrict   = "strict"
	ConfidenceBalanced = "balanced"
	ConfidenceLoose    = "loose"
)

// confidenceThresholds is the probability the chosen option must reach under each preset.
var confidenceThresholds = map[string]float64{
	ConfidenceStrict:   0.85,
	ConfidenceBalanced: 0.70,
	ConfidenceLoose:    0.50,
}

// Routing bounds (ADR Decisions 6 and 7). maxJoinKeyBytes matches the length
// checks on trigger_routes and trigger_routing_decisions.
const (
	minRouteCandidates = 2
	maxRouteCandidates = 20
	maxJoinableRuns    = 20
	maxJoinKeyBytes    = 256
)

// Routing outcomes, stored in trigger_routing_decisions.outcome.
const (
	RouteOutcomeNewRun      = "new_run"
	RouteOutcomeJoinedRun   = "joined_run"
	RouteOutcomeFallbackRun = "fallback_run"
	RouteOutcomeNoRun       = "no_run"
)

// Why a decision did not take the model's answer as given.
const (
	FallbackBelowThreshold = "below_threshold"
	FallbackDecisionFailed = "decision_failed"
	// FallbackRunClosed records a lost race: the chosen run stopped taking
	// messages, so the delivery started a new run of that run's agent.
	FallbackRunClosed = "run_closed"
)

// RoutingConfig is a routed trigger's routing object, on the API and as stored.
type RoutingConfig struct {
	// Candidates are the agent type ids a delivery may start or join.
	Candidates []string `json:"candidates"`
	// Model is the decision model's catalog name, e.g. "liquid/d1".
	Model      string `json:"model"`
	Confidence string `json:"confidence"`
	// FallbackAgentTypeID is the candidate that takes a delivery routing is
	// unsure about. Nil starts no run.
	FallbackAgentTypeID *string `json:"fallback_agent_type_id"`
	// JoinRuns lets a delivery join an open run. Omitted on a request, it is on.
	JoinRuns *bool `json:"join_runs"`
	// JoinKey is a template in the prompt_template syntax. A delivery whose key
	// matches an earlier decision of the same trigger joins that decision's run.
	JoinKey string `json:"join_key,omitempty"`

	// candidateSlugs maps each candidate to the slug its run scopes are keyed
	// on. Filled when the trigger is created or read, never from a request.
	candidateSlugs map[string]string
}

// joins reports whether deliveries may join open runs.
func (r *RoutingConfig) joins() bool {
	return r.JoinRuns == nil || *r.JoinRuns
}

// threshold is the probability the chosen option must reach.
func (r *RoutingConfig) threshold() float64 {
	if t, ok := confidenceThresholds[r.Confidence]; ok {
		return t
	}
	return confidenceThresholds[ConfidenceBalanced]
}

// joinKeyTemplate is the key template for referencedKeys, or "" when there is none.
func (r *RoutingConfig) joinKeyTemplate() string {
	if r == nil {
		return ""
	}
	return r.JoinKey
}

// isCandidate reports whether an agent type is one of the candidates.
func (r *RoutingConfig) isCandidate(agentTypeID string) bool {
	return slices.Contains(r.Candidates, agentTypeID)
}

// RoutingDecision is what a routed trigger decided for one delivery: the row in
// trigger_routing_decisions and the routing object on a delivery run.
type RoutingDecision struct {
	DeliveryID string `json:"-"`
	TriggerID  string `json:"-"`
	TenantID   string `json:"-"`
	// QuestionVersion is stored for audit and never returned (PRD PD6).
	QuestionVersion string `json:"-"`

	Outcome    string  `json:"outcome"`
	KeyMatched bool    `json:"key_matched"`
	JoinKey    *string `json:"join_key"`
	Model      *string `json:"model"`
	// AgentTypeID is the agent that received the delivery.
	AgentTypeID *string `json:"agent_type_id"`
	// RunID is the run started or joined. The delivery run row carries it on the API.
	RunID       *string  `json:"-"`
	Probability *float64 `json:"probability"`
	Threshold   *float64 `json:"threshold"`
	// Confidence is the decision model's confidence in the deciding answer.
	Confidence     *float64      `json:"-"`
	Scores         RoutingScores `json:"scores"`
	FallbackReason *string       `json:"fallback_reason"`
	Error          *string       `json:"error"`
	CreatedAt      time.Time     `json:"-"`

	// decideErr is the failed decision behind Error, for failure accounting.
	decideErr error
}

// RoutingScores holds the probability of every option, keyed by agent type id
// and by run id ("new" for a new run).
type RoutingScores struct {
	Agents map[string]float64 `json:"agents,omitempty"`
	Runs   map[string]float64 `json:"runs,omitempty"`
}

// normalizeRouting validates a routing object and returns what to store.
func normalizeRouting(r *RoutingConfig) (*RoutingConfig, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: routing is required when target_kind is %q", ErrValidation, TargetKindRouted)
	}
	candidates, err := normalizeCandidates(r.Candidates)
	if err != nil {
		return nil, err
	}
	out := &RoutingConfig{Candidates: candidates, Model: strings.TrimSpace(r.Model), Confidence: r.Confidence, JoinKey: r.JoinKey}

	if _, err := modelUseScope(out.Model); err != nil {
		return nil, err
	}

	if out.Confidence == "" {
		out.Confidence = ConfidenceBalanced
	}
	if _, ok := confidenceThresholds[out.Confidence]; !ok {
		return nil, fmt.Errorf("%w: routing.confidence must be one of %q, %q or %q", ErrValidation, ConfidenceStrict, ConfidenceBalanced, ConfidenceLoose)
	}

	if r.FallbackAgentTypeID != nil && strings.TrimSpace(*r.FallbackAgentTypeID) != "" {
		fallback := strings.TrimSpace(*r.FallbackAgentTypeID)
		if !out.isCandidate(fallback) {
			return nil, fmt.Errorf("%w: routing.fallback_agent_type_id must be one of the candidates", ErrValidation)
		}
		out.FallbackAgentTypeID = &fallback
	}

	joinRuns := r.JoinRuns == nil || *r.JoinRuns
	out.JoinRuns = &joinRuns

	if len(out.JoinKey) > maxJoinKeyBytes {
		return nil, fmt.Errorf("%w: routing.join_key must be %d bytes or fewer", ErrValidation, maxJoinKeyBytes)
	}
	if err := validateFieldKeys(out.JoinKey, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// normalizeCandidates trims the candidate ids and checks there are 2 to 20 distinct ones.
func normalizeCandidates(ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("%w: routing.candidates must not contain an empty id", ErrValidation)
		}
		if slices.Contains(out, id) {
			return nil, fmt.Errorf("%w: routing.candidates lists %q more than once", ErrValidation, id)
		}
		out = append(out, id)
	}
	if n := len(out); n < minRouteCandidates || n > maxRouteCandidates {
		return nil, fmt.Errorf("%w: routing.candidates must list between %d and %d agents", ErrValidation, minRouteCandidates, maxRouteCandidates)
	}
	return out, nil
}

// modelUseScope is the scope that lets a principal use a decision model. A
// catalog name is "<family>/<name>", and the inference gateway checks
// inference:<family>:<name>:use against it.
func modelUseScope(model string) (string, error) {
	family, name, ok := strings.Cut(model, "/")
	if !ok || family == "" || name == "" || strings.Contains(model, ":") {
		return "", fmt.Errorf("%w: routing.model must be a catalog model name such as \"liquid/d1\"", ErrValidation)
	}
	return "inference:" + family + ":" + name + ":use", nil
}

// routedGrantScopes are the scopes a routed trigger's principal holds: run
// scopes for every candidate, inference:invoke, and the model's use scope
// (ADR Decision 16).
func routedGrantScopes(r *RoutingConfig) ([]string, error) {
	modelScope, err := modelUseScope(r.Model)
	if err != nil {
		return nil, err
	}
	var scopes []string
	for _, id := range r.Candidates {
		scopes = append(scopes, triggerRunScopes(r.candidateSlugs[id])...)
	}
	return append(scopes, "inference:invoke", modelScope), nil
}

// routedCallerScopes are the grant scopes the caller is checked for with the
// evaluator: inference:invoke and the model's use scope. Each candidate's run
// scopes are covered by the caller's can_run on it (ADR Decision 17).
func routedCallerScopes(r *RoutingConfig) []string {
	modelScope, _ := modelUseScope(r.Model)
	return []string{"inference:invoke", modelScope}
}

// sameGrant reports whether two routing objects need the same grant.
func sameGrant(a, b *RoutingConfig) bool {
	return a.Model == b.Model && slices.Equal(slices.Sorted(slices.Values(a.Candidates)), slices.Sorted(slices.Values(b.Candidates)))
}

// renderJoinKey renders a routed trigger's join key for one delivery. ok is
// false when there is no key, it renders empty, or it renders over the limit;
// the model then decides.
func renderJoinKey(r *RoutingConfig, fields map[string]string) (string, bool) {
	if r.JoinKey == "" {
		return "", false
	}
	key := strings.TrimSpace(renderTemplate(r.JoinKey, fields))
	if key == "" || len(key) > maxJoinKeyBytes {
		return "", false
	}
	return key, true
}
