package v1alpha1

import (
	"testing"

	"github.com/runkora/kora/backend/internal/platform/crdtest"
)

const crdFile = "crd-inferenceprovider.yaml"

func provider(apis ...any) map[string]any {
	return map[string]any{
		"apiVersion": "runkora.io/v1alpha1",
		"kind":       "InferenceProvider",
		"metadata":   map[string]any{"name": "ts"},
		"spec": map[string]any{
			"type":       "compatible",
			"compatible": map[string]any{"baseURL": "https://api.typesafe.ai/v1", "apis": apis},
		},
	}
}

func TestCRDValidatesDeclaredAPIs(t *testing.T) {
	schema := crdtest.Schema(t, crdFile)
	cases := []struct {
		name    string
		apis    []any
		wantErr bool
	}{
		{"systemone", []any{"systemone"}, false},
		{"systemone with chat", []any{"systemone", "chatCompletions"}, false},
		{"every declared API", []any{"chatCompletions", "embeddings", "responses", "messages", "systemone"}, false},
		{"unknown API", []any{"decisions"}, true},
		{"unknown beside a known one", []any{"systemone", "decisions"}, true},
		{"wrong case", []any{"SystemOne"}, true},
		{"no APIs", []any{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := crdtest.Validate(schema, provider(tc.apis...), "")
			if (len(errs) > 0) != tc.wantErr {
				t.Errorf("errors = %v, want error %v", errs, tc.wantErr)
			}
		})
	}
}

// TestCRDEnumMatchesAPIConstants keeps the Go constants and the chart's enum
// in step: each declared API is accepted, and the enum lists nothing else.
func TestCRDEnumMatchesAPIConstants(t *testing.T) {
	declared := []API{APIChatCompletions, APIEmbeddings, APIResponses, APIMessages, APISystemOne}
	items := crdtest.Child(crdtest.Prop(crdtest.Prop(crdtest.Prop(crdtest.Schema(t, crdFile), "spec"), "compatible"), "apis"), "items")
	if items == nil {
		t.Fatal("apis has no item schema")
	}
	enum := map[string]bool{}
	for _, v := range items["enum"].([]any) {
		enum[v.(string)] = true
	}
	for _, api := range declared {
		if !enum[string(api)] {
			t.Errorf("API %q is not in the CRD enum", api)
		}
		delete(enum, string(api))
	}
	for extra := range enum {
		t.Errorf("CRD enum has %q, which has no API constant", extra)
	}
	if APISystemOne != "systemone" {
		t.Errorf("APISystemOne = %q", APISystemOne)
	}
}

func TestDeepCopyKeepsAPIs(t *testing.T) {
	p := &InferenceProvider{Spec: Spec{Type: TypeCompatible, Compatible: &Compatible{BaseURL: "https://x/v1", APIs: []API{APISystemOne}}}}
	cp := p.DeepCopy()
	cp.Spec.Compatible.APIs[0] = APIMessages
	if p.Spec.Compatible.APIs[0] != APISystemOne {
		t.Error("DeepCopy shares the APIs slice")
	}
}

// TestCRDDecisionModelsShape checks the schema of decisionModels. The CEL rule
// that ties it to systemone is not executed here; see
// TestCRDDecisionModelsRuleIsDeclared.
func TestCRDDecisionModelsShape(t *testing.T) {
	schema := crdtest.Schema(t, crdFile)
	cases := []struct {
		name           string
		decisionModels any
		wantErr        bool
	}{
		{"list of ids", []any{"jev", "clef"}, false},
		{"absent", nil, false},
		{"empty id", []any{""}, true},
		{"not a list", "jev", true},
		{"id is not a string", []any{1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := provider("systemone")
			if tc.decisionModels != nil {
				p["spec"].(map[string]any)["compatible"].(map[string]any)["decisionModels"] = tc.decisionModels
			}
			errs := crdtest.Validate(schema, p, "")
			if (len(errs) > 0) != tc.wantErr {
				t.Errorf("errors = %v, want error %v", errs, tc.wantErr)
			}
		})
	}
}

// TestCRDDecisionModelsRuleIsDeclared asserts that the CRD carries the CEL rule
// requiring systemone in apis for decisionModels. The rule is asserted, not
// executed: running CEL needs k8s.io/apiserver and cel-go, which this module
// does not take on. The API server enforces it.
func TestCRDDecisionModelsRuleIsDeclared(t *testing.T) {
	const (
		wantRule    = "!has(self.decisionModels) || 'systemone' in self.apis"
		wantMessage = "decisionModels needs systemone in apis."
	)
	compatible := crdtest.Prop(crdtest.Prop(crdtest.Schema(t, crdFile), "spec"), "compatible")
	rules, _ := compatible["x-kubernetes-validations"].([]any)
	for _, r := range rules {
		rule := r.(crdtest.Node)
		if rule["rule"] == wantRule {
			if rule["message"] != wantMessage {
				t.Errorf("rule message = %q, want %q", rule["message"], wantMessage)
			}
			return
		}
	}
	t.Errorf("spec.compatible has no x-kubernetes-validations rule %q", wantRule)
}

func TestDeepCopyKeepsDecisionModels(t *testing.T) {
	p := &InferenceProvider{Spec: Spec{Type: TypeCompatible, Compatible: &Compatible{BaseURL: "https://x/v1", APIs: []API{APISystemOne}, DecisionModels: []string{"clef"}}}}
	cp := p.DeepCopy()
	cp.Spec.Compatible.DecisionModels[0] = "other"
	if p.Spec.Compatible.DecisionModels[0] != "clef" {
		t.Error("DeepCopy shares the DecisionModels slice")
	}
}
