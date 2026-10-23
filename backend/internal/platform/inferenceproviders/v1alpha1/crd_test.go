package v1alpha1

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const crdPath = "../../../../../charts/kora-crds/templates/crd-inferenceprovider.yaml"

// schemaNode is a node of the CRD's openAPIV3Schema, as decoded from YAML.
type schemaNode = map[string]any

// crdSchema loads the openAPIV3Schema the chart ships. The template has only
// Helm label lines, which carry no schema, so those lines are dropped.
//
// The schema is walked as plain data and checked by validate below. The API
// server's own validators (k8s.io/apiextensions-apiserver/pkg/apiserver/validation)
// import cel-go and k8s.io/apiserver, which this module does not take on.
func crdSchema(t *testing.T) schemaNode {
	t.Helper()
	raw, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if !strings.Contains(line, "{{") {
			kept = append(kept, line)
		}
	}
	var crd schemaNode
	if err := yaml.Unmarshal([]byte(strings.Join(kept, "\n")), &crd); err != nil {
		t.Fatal(err)
	}
	versions, _ := child(crd, "spec")["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("want one version, got %d", len(versions))
	}
	schema := child(child(versions[0].(schemaNode), "schema"), "openAPIV3Schema")
	if schema == nil {
		t.Fatal("version has no openAPIV3Schema")
	}
	return schema
}

// child returns the map under key, or nil.
func child(n schemaNode, key string) schemaNode {
	c, _ := n[key].(schemaNode)
	return c
}

// prop returns the schema of a property of an object schema.
func prop(n schemaNode, name string) schemaNode { return child(child(n, "properties"), name) }

// validate checks value against the subset of OpenAPI schema keywords the
// InferenceProvider CRD uses: type, required, properties, items, enum,
// minItems, minLength and pattern. It returns one message per violation.
func validate(schema schemaNode, value any, path string) []string {
	var errs []string
	fail := func(format string, args ...any) { errs = append(errs, path+": "+fmt.Sprintf(format, args...)) }
	switch v := value.(type) {
	case map[string]any:
		if schema["type"] != "object" {
			fail("got an object, want %v", schema["type"])
		}
		required, _ := schema["required"].([]any)
		for _, r := range required {
			if _, ok := v[r.(string)]; !ok {
				fail("%s is required", r)
			}
		}
		for k, item := range v {
			if ps := prop(schema, k); ps != nil {
				errs = append(errs, validate(ps, item, path+"."+k)...)
			}
		}
	case []any:
		if schema["type"] != "array" {
			fail("got an array, want %v", schema["type"])
		}
		if n, ok := schema["minItems"].(float64); ok && float64(len(v)) < n {
			fail("needs at least %v items", n)
		}
		for i, item := range v {
			errs = append(errs, validate(child(schema, "items"), item, fmt.Sprintf("%s[%d]", path, i))...)
		}
	case string:
		if schema["type"] != "string" {
			fail("got a string, want %v", schema["type"])
		}
		if enum, ok := schema["enum"].([]any); ok && !slices.Contains(enum, any(v)) {
			fail("%q is not one of %v", v, enum)
		}
		if n, ok := schema["minLength"].(float64); ok && float64(len(v)) < n {
			fail("shorter than %v", n)
		}
		if pat, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pat).MatchString(v) {
			fail("%q does not match %s", v, pat)
		}
	default:
		fail("unexpected %T, want %v", value, schema["type"])
	}
	return errs
}

func provider(apis ...any) map[string]any {
	return map[string]any{
		"apiVersion": "orbit.io/v1alpha1",
		"kind":       "InferenceProvider",
		"metadata":   map[string]any{"name": "ts"},
		"spec": map[string]any{
			"type":       "compatible",
			"compatible": map[string]any{"baseURL": "https://api.typesafe.ai/v1", "apis": apis},
		},
	}
}

func TestCRDValidatesDeclaredAPIs(t *testing.T) {
	schema := crdSchema(t)
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
			errs := validate(schema, provider(tc.apis...), "")
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
	items := child(prop(prop(prop(crdSchema(t), "spec"), "compatible"), "apis"), "items")
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
	schema := crdSchema(t)
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
			errs := validate(schema, p, "")
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
	compatible := prop(prop(crdSchema(t), "spec"), "compatible")
	rules, _ := compatible["x-kubernetes-validations"].([]any)
	for _, r := range rules {
		rule := r.(schemaNode)
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
