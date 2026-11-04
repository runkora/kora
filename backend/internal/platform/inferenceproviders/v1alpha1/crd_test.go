package v1alpha1

import (
	"testing"

	"github.com/runkora/kora/backend/internal/platform/crdtest"
)

const crdFile = "generated/runkora.io_inferenceproviders.yaml"

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
			err := crdtest.Validate(t, schema, provider(tc.apis...))
			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v, want error %v", err, tc.wantErr)
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
			err := crdtest.Validate(t, schema, p)
			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}
