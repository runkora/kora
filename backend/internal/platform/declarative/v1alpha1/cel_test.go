package v1alpha1

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"sigs.k8s.io/yaml"
)

// celCase is one accepted and one rejected object for a rule. old, when set,
// is the previous object a transition rule compares against.
type celCase struct {
	accept, reject, old string
}

// dnsLabel is the name rule on every kind but Agent.
const dnsLabel = "self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63"

var dnsLabelCase = celCase{accept: `{metadata: {name: go-build}}`, reject: `{metadata: {name: go.build}}`}

// celCases holds a case for every x-kubernetes-validations rule, keyed by CRD
// file and rule. TestEveryCELRuleHasACase fails on a rule without one.
var celCases = map[string]map[string]celCase{
	"generated/runkora.io_virtualmodels.yaml":  {dnsLabel: dnsLabelCase},
	"generated/runkora.io_environments.yaml":   {dnsLabel: dnsLabelCase},
	"generated/runkora.io_triggersources.yaml": {dnsLabel: dnsLabelCase},
	"generated/runkora.io_policyroles.yaml":    {dnsLabel: dnsLabelCase},
	"generated/runkora.io_agents.yaml": {
		"self.metadata.name.matches('^[a-z]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 52": {
			accept: `{metadata: {name: code-reviewer}}`,
			reject: `{metadata: {name: 1-reviewer}}`,
		},
		"!(self.metadata.name in ['delegated', 'autonomous'])": {
			accept: `{metadata: {name: reviewer}}`,
			reject: `{metadata: {name: delegated}}`,
		},
		"self == oldSelf": {
			accept: `{metadata: {name: a}, spec: {identityMode: delegated}}`,
			reject: `{metadata: {name: a}, spec: {identityMode: autonomous}}`,
			old:    `{metadata: {name: a}, spec: {identityMode: delegated}}`,
		},
	},
	"generated/runkora.io_triggers.yaml": {
		dnsLabel: dnsLabelCase,
		"has(self.agent) != has(self.routing)": {
			accept: `{metadata: {name: t}, spec: {kind: manual, agent: a}}`,
			reject: `{metadata: {name: t}, spec: {kind: manual}}`,
		},
		"self.kind != 'webhook' || (has(self.source) && has(self.eventType))": {
			accept: `{metadata: {name: t}, spec: {kind: webhook, agent: a, source: s, eventType: e}}`,
			reject: `{metadata: {name: t}, spec: {kind: webhook, agent: a, source: s}}`,
		},
		"self.kind != 'scheduled' || has(self.schedule)": {
			accept: `{metadata: {name: t}, spec: {kind: scheduled, agent: a, schedule: {cronExpression: '0 9 * * 1'}}}`,
			reject: `{metadata: {name: t}, spec: {kind: scheduled, agent: a}}`,
		},
		"self.kind == 'webhook' || !(has(self.source) || has(self.eventType) || has(self.match) || has(self.matchContains) || has(self.senderRestriction) || has(self.coalesce) || has(self.routing))": {
			accept: `{metadata: {name: t}, spec: {kind: manual, agent: a}}`,
			reject: `{metadata: {name: t}, spec: {kind: manual, agent: a, eventType: e}}`,
		},
		"self.kind == 'scheduled' || !has(self.schedule)": {
			accept: `{metadata: {name: t}, spec: {kind: manual, agent: a}}`,
			reject: `{metadata: {name: t}, spec: {kind: manual, agent: a, schedule: {cronExpression: '0 9 * * 1'}}}`,
		},
		"self.kind == 'manual' || !has(self.manual)": {
			accept: `{metadata: {name: t}, spec: {kind: manual, agent: a, manual: {}}}`,
			reject: `{metadata: {name: t}, spec: {kind: scheduled, agent: a, schedule: {cronExpression: '0 9 * * 1'}, manual: {}}}`,
		},
	},
}

// structural loads a generated CRD's schema as the API server does.
func structural(t *testing.T, file string) (*structuralschema.Structural, []apiextensionsv1.ValidationRule) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../../../../charts/kora-crds", file))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	v1 := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
	var internal apiextensions.JSONSchemaProps
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(v1, &internal, nil); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	s, err := structuralschema.NewStructural(&internal)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return s, rules(v1)
}

// rules collects every x-kubernetes-validations rule in a schema.
func rules(s *apiextensionsv1.JSONSchemaProps) []apiextensionsv1.ValidationRule {
	if s == nil {
		return nil
	}
	out := append([]apiextensionsv1.ValidationRule(nil), s.XValidations...)
	for _, p := range s.Properties {
		out = append(out, rules(&p)...)
	}
	if s.Items != nil {
		out = append(out, rules(s.Items.Schema)...)
	}
	if s.AdditionalProperties != nil {
		out = append(out, rules(s.AdditionalProperties.Schema)...)
	}
	return out
}

func decode(t *testing.T, doc string) any {
	t.Helper()
	if doc == "" {
		return nil
	}
	var v map[string]any
	if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("%s: %v", doc, err)
	}
	return v
}

// refusedBy reports whether the CEL validator refuses obj with the rule's message.
func refusedBy(t *testing.T, s *structuralschema.Structural, rule apiextensionsv1.ValidationRule, obj, old string) bool {
	t.Helper()
	errs, _ := cel.NewValidator(s, true, celconfig.PerCallLimit).
		Validate(context.Background(), field.NewPath("root"), s, decode(t, obj), decode(t, old), celconfig.RuntimeCELCostBudget)
	for _, e := range errs {
		if e.Type == field.ErrorTypeInternal {
			t.Fatalf("rule %q: %v", rule.Rule, e)
		}
		if strings.Contains(e.Error(), rule.Message) {
			return true
		}
	}
	return false
}

func TestCELRules(t *testing.T) {
	for file, cases := range celCases {
		s, all := structural(t, file)
		for _, rule := range all {
			tc, ok := cases[rule.Rule]
			if !ok {
				continue
			}
			t.Run(fmt.Sprintf("%s/%s", filepath.Base(file), rule.Message), func(t *testing.T) {
				if refusedBy(t, s, rule, tc.accept, tc.old) {
					t.Errorf("accepted case refused: %s", tc.accept)
				}
				if !refusedBy(t, s, rule, tc.reject, tc.old) {
					t.Errorf("rejected case accepted: %s", tc.reject)
				}
			})
		}
	}
}

// TestEveryCELRuleHasACase keeps a new rule from shipping untested.
func TestEveryCELRuleHasACase(t *testing.T) {
	for _, k := range kinds {
		_, all := structural(t, k.file)
		for _, rule := range all {
			if _, ok := celCases[k.file][rule.Rule]; !ok {
				t.Errorf("%s: rule %q has no case in celCases", k.file, rule.Rule)
			}
			// The cases match refusals by message.
			if rule.Message == "" {
				t.Errorf("%s: rule %q has no message", k.file, rule.Rule)
			}
		}
	}
}

// TestConnectionBaseHostRules covers the kubernetes baseHost rules, which need
// more than one accepted and rejected object each.
func TestConnectionBaseHostRules(t *testing.T) {
	const file = "generated/runkora.io_connections.yaml"
	const (
		missing = "kubernetes connections require a non-empty baseHost."
		ipLit   = "kubernetes connections must use a DNS hostname as baseHost"
	)
	s, all := structural(t, file)
	msgs := map[string]apiextensionsv1.ValidationRule{}
	for _, r := range all {
		msgs[r.Message] = r
	}
	for _, c := range []struct {
		name, obj, refusal string
	}{
		{"absent", `{metadata: {name: c}, spec: {adapter: kubernetes}}`, missing},
		{"empty", `{metadata: {name: c}, spec: {adapter: kubernetes, baseHost: ""}}`, missing},
		{"ip literal", `{metadata: {name: c}, spec: {adapter: kubernetes, baseHost: 10.0.0.1}}`, ipLit},
		{"dns name", `{metadata: {name: c}, spec: {adapter: kubernetes, baseHost: api.example.com}}`, ""},
		{"other adapter without baseHost", `{metadata: {name: c}, spec: {adapter: github_app}}`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			for msg, rule := range msgs {
				want := c.refusal != "" && strings.HasPrefix(msg, c.refusal)
				if got := refusedBy(t, s, rule, c.obj, ""); got != want {
					t.Errorf("rule %q refused=%v, want %v", msg, got, want)
				}
			}
		})
	}
}
