package policy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// Query hands the rego one candidate per row, and listing endpoints pass whole
// result sets. Candidate.ID never reaches the rego input, so rows sharing a
// resource path must cost one evaluation between them, not one each.

// countingQuery records every Eval and decides from allowScopes, keyed on the
// required_scope the engine derived for that candidate.
type countingQuery struct {
	evals       int
	seenScopes  []string
	allowScopes map[string]bool
}

func (c *countingQuery) Eval(_ context.Context, options ...rego.EvalOption) (rego.ResultSet, error) {
	c.evals++

	// Recover the input the engine built. rego.EvalInput is opaque, so apply the
	// options to an EvalContext the same way rego does and read it back.
	ec := &rego.EvalContext{}
	for _, opt := range options {
		opt(ec)
	}
	raw := ec.RawInput()
	if raw == nil {
		return nil, fmt.Errorf("no input supplied to Eval")
	}
	in, ok := (*raw).(opaInput)
	if !ok {
		return nil, fmt.Errorf("unexpected input type %T", *raw)
	}
	c.seenScopes = append(c.seenScopes, in.RequiredScope)

	return rego.ResultSet{{
		Bindings: rego.Vars{"x": map[string]any{
			"allow":  c.allowScopes[in.RequiredScope],
			"reason": "test",
		}},
		Expressions: []*rego.ExpressionValue{{}},
	}}, nil
}

func queryTestClaims() *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
		Kora: token.KoraClaims{
			TenantID:      "t1",
			PrincipalType: token.PrincipalTypeHuman,
		},
	}
}

// engineWithQuery returns an engine whose t1 bundle evaluates through q.
func engineWithQuery(q preparedQuery) *engine {
	return &engine{bundles: map[string]*tenantBundle{"t1": {query: q}}}
}

func TestQuery_EvaluatesOncePerDistinctResourcePath(t *testing.T) {
	// 300 runs spread over 3 agent types — the shape of GET /api/v1/runs.
	const (
		rows  = 300
		slugs = 3
	)
	q := &countingQuery{allowScopes: map[string]bool{}}
	candidates := make([]Candidate, 0, rows)
	for i := range rows {
		slug := fmt.Sprintf("agent-%d", i%slugs)
		candidates = append(candidates, Candidate{
			ID:           fmt.Sprintf("run-%d", i),
			PathSegments: []string{"runs", slug},
		})
	}
	// Allow every scope so the result is the full set and a dropped candidate
	// would show up as a length mismatch.
	for i := range slugs {
		scope := DeriveRequiredScope("agents", []string{"runs", fmt.Sprintf("agent-%d", i)}, "read")
		q.allowScopes[scope] = true
	}

	allowed, err := engineWithQuery(q).Query(context.Background(), QueryInput{
		Claims:       queryTestClaims(),
		ResourceType: "agents",
		Action:       "read",
		Candidates:   candidates,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(allowed) != rows {
		t.Errorf("allowed = %d candidates, want %d", len(allowed), rows)
	}
	if q.evals != slugs {
		t.Errorf("rego evaluations = %d, want %d (one per distinct resource path, not one per row)", q.evals, slugs)
	}
}

func TestQuery_DecisionsAreUnchangedByMemoisation(t *testing.T) {
	// Two agent types, only one permitted: the memo must not leak an allow from
	// one resource path onto another.
	allowedScope := DeriveRequiredScope("agents", []string{"runs", "visible"}, "read")
	q := &countingQuery{allowScopes: map[string]bool{allowedScope: true}}

	var candidates []Candidate
	want := map[string]bool{}
	for i := range 20 {
		slug := "visible"
		if i%2 == 0 {
			slug = "hidden"
		}
		id := fmt.Sprintf("run-%d", i)
		candidates = append(candidates, Candidate{ID: id, PathSegments: []string{"runs", slug}})
		want[id] = slug == "visible"
	}

	allowed, err := engineWithQuery(q).Query(context.Background(), QueryInput{
		Claims:       queryTestClaims(),
		ResourceType: "agents",
		Action:       "read",
		Candidates:   candidates,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	got := map[string]bool{}
	for _, id := range allowed {
		got[id] = true
	}
	for id, shouldAllow := range want {
		if got[id] != shouldAllow {
			t.Errorf("candidate %s: allowed = %v, want %v", id, got[id], shouldAllow)
		}
	}
	if q.evals != 2 {
		t.Errorf("rego evaluations = %d, want 2", q.evals)
	}
}

func TestQuery_AttributesSeparateOtherwiseIdenticalCandidates(t *testing.T) {
	// Same resource path, different resource attributes. Attributes reach the
	// rego input, so these must not collapse onto one decision.
	q := &countingQuery{allowScopes: map[string]bool{}}
	candidates := []Candidate{
		{ID: "a", PathSegments: []string{"runs", "coder"}, Attributes: map[string]string{"model": "opus"}},
		{ID: "b", PathSegments: []string{"runs", "coder"}, Attributes: map[string]string{"model": "sonnet"}},
		{ID: "c", PathSegments: []string{"runs", "coder"}, Attributes: map[string]string{"model": "opus"}},
	}

	if _, err := engineWithQuery(q).Query(context.Background(), QueryInput{
		Claims:       queryTestClaims(),
		ResourceType: "agents",
		Action:       "read",
		Candidates:   candidates,
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}

	// a and c share attributes and collapse; b is distinct.
	if q.evals != 2 {
		t.Errorf("rego evaluations = %d, want 2 (candidates differing only by attributes must not share a decision)", q.evals)
	}
}

func TestQuery_MapAttributeOrderDoesNotSplitTheKey(t *testing.T) {
	// Go map iteration order is randomised; the key must not depend on it, or
	// the memo silently degrades back to one evaluation per row.
	q := &countingQuery{allowScopes: map[string]bool{}}
	var candidates []Candidate
	for i := range 50 {
		candidates = append(candidates, Candidate{
			ID:           fmt.Sprintf("c-%d", i),
			PathSegments: []string{"runs", "coder"},
			Attributes:   map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"},
		})
	}

	if _, err := engineWithQuery(q).Query(context.Background(), QueryInput{
		Claims:       queryTestClaims(),
		ResourceType: "agents",
		Action:       "read",
		Candidates:   candidates,
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}

	if q.evals != 1 {
		t.Errorf("rego evaluations = %d, want 1", q.evals)
	}
}

func TestQuery_EmptyCandidatesDoesNotEvaluate(t *testing.T) {
	q := &countingQuery{allowScopes: map[string]bool{}}
	allowed, err := engineWithQuery(q).Query(context.Background(), QueryInput{
		Claims:       queryTestClaims(),
		ResourceType: "agents",
		Action:       "read",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(allowed) != 0 {
		t.Errorf("allowed = %v, want empty", allowed)
	}
	if q.evals != 0 {
		t.Errorf("rego evaluations = %d, want 0", q.evals)
	}
}

func TestDecisionKey_DistinguishesEveryInputField(t *testing.T) {
	base := opaInput{
		Principal:     principalInput{Type: "HUMAN", TenantID: "t1", ID: "u1", Roles: []string{"admin"}},
		RequiredScope: "agents:runs:coder:read",
		Resource:      resourceInput{Type: "agents", Attributes: map[string]string{"k": "v"}},
		Action:        "read",
	}
	baseKey, err := decisionKey(base)
	if err != nil {
		t.Fatalf("decisionKey: %v", err)
	}

	mutations := map[string]func(*opaInput){
		"principal type":      func(i *opaInput) { i.Principal.Type = "AGENT" },
		"principal tenant":    func(i *opaInput) { i.Principal.TenantID = "t2" },
		"principal id":        func(i *opaInput) { i.Principal.ID = "u2" },
		"principal roles":     func(i *opaInput) { i.Principal.Roles = []string{"viewer"} },
		"required scope":      func(i *opaInput) { i.RequiredScope = "agents:runs:other:read" },
		"resource type":       func(i *opaInput) { i.Resource.Type = "models" },
		"resource attributes": func(i *opaInput) { i.Resource.Attributes = map[string]string{"k": "w"} },
		"action":              func(i *opaInput) { i.Action = "write" },
		"delegation present":  func(i *opaInput) { i.DelegationPresent = true },
		"delegation scopes":   func(i *opaInput) { i.DelegationScopes = []string{"s"} },
		"delegator id":        func(i *opaInput) { i.DelegatorID = "d1" },
		"delegator type":      func(i *opaInput) { i.DelegatorPrincipalType = "HUMAN" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			mutated := base
			mutate(&mutated)
			key, err := decisionKey(mutated)
			if err != nil {
				t.Fatalf("decisionKey: %v", err)
			}
			if key == baseKey {
				t.Errorf("changing %s did not change the decision key: two inputs the rego would decide separately share one memo entry", name)
			}
		})
	}
}

func TestDecisionKey_IsStableAcrossAttributeOrdering(t *testing.T) {
	in := opaInput{
		RequiredScope: "agents:runs:coder:read",
		Resource: resourceInput{Type: "agents", Attributes: map[string]string{
			"z": "1", "y": "2", "x": "3", "w": "4", "v": "5", "u": "6",
		}},
	}
	first, err := decisionKey(in)
	if err != nil {
		t.Fatalf("decisionKey: %v", err)
	}
	for range 200 {
		next, err := decisionKey(in)
		if err != nil {
			t.Fatalf("decisionKey: %v", err)
		}
		if next != first {
			t.Fatalf("decision key is not stable across map iteration order:\n%s\n%s", first, next)
		}
	}
	if !strings.Contains(first, "agents:runs:coder:read") {
		t.Errorf("key does not carry the required scope: %s", first)
	}
}
