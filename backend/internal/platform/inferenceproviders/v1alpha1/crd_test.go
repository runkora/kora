package v1alpha1

import (
	"context"
	"os"
	"strings"
	"testing"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	celvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

// The API server's CEL limits (k8s.io/apiserver/pkg/apis/cel).
const (
	celPerCallLimit = 1000000
	celCostBudget   = 10000000
)

const crdPath = "../../../../../charts/orbit-crds/templates/crd-inferenceprovider.yaml"

// crdSchema loads the openAPIV3Schema the chart ships. The template has only
// Helm label lines, which carry no schema, so those lines are dropped.
func crdSchema(t *testing.T) *apiextensions.JSONSchemaProps {
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
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal([]byte(strings.Join(kept, "\n")), &crd); err != nil {
		t.Fatal(err)
	}
	if len(crd.Spec.Versions) != 1 || crd.Spec.Versions[0].Schema == nil {
		t.Fatalf("want one version with a schema, got %d", len(crd.Spec.Versions))
	}
	var schema apiextensions.JSONSchemaProps
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(crd.Spec.Versions[0].Schema.OpenAPIV3Schema, &schema, nil); err != nil {
		t.Fatal(err)
	}
	return &schema
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
	validator, _, err := validation.NewSchemaValidator(crdSchema(t))
	if err != nil {
		t.Fatal(err)
	}
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
			errs := validation.ValidateCustomResource(nil, provider(tc.apis...), validator)
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
	items := crdSchema(t).Properties["spec"].Properties["compatible"].Properties["apis"].Items.Schema
	if items == nil {
		t.Fatal("apis has no item schema")
	}
	enum := map[string]bool{}
	for _, v := range items.Enum {
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

func TestCRDDecisionModelsNeedSystemOne(t *testing.T) {
	root := crdSchema(t)
	validator, _, err := validation.NewSchemaValidator(root)
	if err != nil {
		t.Fatal(err)
	}
	structural, err := structuralschema.NewStructural(root)
	if err != nil {
		t.Fatal(err)
	}
	cel := celvalidation.NewValidator(structural, true, celPerCallLimit)
	cases := []struct {
		name           string
		apis           []any
		decisionModels any
		wantErr        bool
	}{
		{"with systemone alone", []any{"systemone"}, []any{"jev"}, false},
		{"with systemone beside chat", []any{"chatCompletions", "systemone"}, []any{"clef"}, false},
		{"without systemone", []any{"chatCompletions"}, []any{"clef"}, true},
		{"empty list without systemone", []any{"chatCompletions"}, []any{}, true},
		{"absent without systemone", []any{"chatCompletions"}, nil, false},
		{"empty id", []any{"systemone"}, []any{""}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := provider(tc.apis...)
			if tc.decisionModels != nil {
				p["spec"].(map[string]any)["compatible"].(map[string]any)["decisionModels"] = tc.decisionModels
			}
			errs := validation.ValidateCustomResource(nil, p, validator)
			celErrs, _ := cel.Validate(context.Background(), field.NewPath(""), structural, p, nil, celCostBudget)
			errs = append(errs, celErrs...)
			if (len(errs) > 0) != tc.wantErr {
				t.Errorf("errors = %v, want error %v", errs, tc.wantErr)
			}
		})
	}
}

func TestDeepCopyKeepsDecisionModels(t *testing.T) {
	p := &InferenceProvider{Spec: Spec{Type: TypeCompatible, Compatible: &Compatible{BaseURL: "https://x/v1", APIs: []API{APISystemOne}, DecisionModels: []string{"clef"}}}}
	cp := p.DeepCopy()
	cp.Spec.Compatible.DecisionModels[0] = "other"
	if p.Spec.Compatible.DecisionModels[0] != "clef" {
		t.Error("DeepCopy shares the DecisionModels slice")
	}
}
