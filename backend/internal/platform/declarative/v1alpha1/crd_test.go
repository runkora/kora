package v1alpha1

import (
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/crdtest"
)

var kinds = []struct {
	file string
	obj  runtime.Object
}{
	{"crd-virtualmodel.yaml", &VirtualModel{}},
	{"crd-environment.yaml", &Environment{}},
	{"crd-agent.yaml", &Agent{}},
	{"crd-triggersource.yaml", &TriggerSource{}},
	{"crd-trigger.yaml", &Trigger{}},
	{"crd-policyrole.yaml", &PolicyRole{}},
}

// The examples from docs/adr/2026-10-24-declarative-resources.md.
// TestCRDShape checks each CRD is namespaced with one served version and a
// status subresource.
func TestCRDShape(t *testing.T) {
	for _, k := range kinds {
		spec := crdtest.Child(crdtest.CRD(t, k.file), "spec")
		versions, _ := spec["versions"].([]any)
		if spec["scope"] != "Namespaced" || len(versions) != 1 {
			t.Errorf("%s: scope %v with %d versions", k.file, spec["scope"], len(versions))
			continue
		}
		v := versions[0].(crdtest.Node)
		if v["name"] != "v1alpha1" || crdtest.Child(crdtest.Child(v, "subresources"), "status") == nil {
			t.Errorf("%s: want v1alpha1 with a status subresource", k.file)
		}
	}
}

func TestGoTypesMatchSchema(t *testing.T) {
	for _, k := range kinds {
		t.Run(k.file, func(t *testing.T) {
			schema := crdtest.Schema(t, k.file)
			typ := reflect.TypeOf(k.obj).Elem()
			for _, f := range []string{"Spec", "Status"} {
				field, _ := typ.FieldByName(f)
				compareFields(t, "."+strings.ToLower(f), crdtest.Prop(schema, strings.ToLower(f)), field.Type)
			}
		})
	}
}

var (
	conditionType = reflect.TypeFor[metav1.Condition]()
	rawType       = reflect.TypeFor[runtime.RawExtension]()
)

func compareFields(t *testing.T, path string, schema crdtest.Node, typ reflect.Type) {
	t.Helper()
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if schema == nil {
		t.Errorf("%s: no schema for Go type %s", path, typ)
		return
	}
	switch {
	case typ == conditionType:
		return
	case typ == rawType:
		if schema["x-kubernetes-preserve-unknown-fields"] != true {
			t.Errorf("%s: a raw field needs x-kubernetes-preserve-unknown-fields", path)
		}
		return
	}
	switch typ.Kind() {
	case reflect.Struct:
		goFields := map[string]reflect.Type{}
		collectFields(typ, goFields)
		for name, ft := range goFields {
			compareFields(t, path+"."+name, crdtest.Prop(schema, name), ft)
		}
		for name := range crdtest.Child(schema, "properties") {
			if _, ok := goFields[name]; !ok {
				t.Errorf("%s.%s: in the schema but not in Go type %s", path, name, typ)
			}
		}
	case reflect.Slice:
		compareFields(t, path+"[]", crdtest.Child(schema, "items"), typ.Elem())
	case reflect.Map:
		compareFields(t, path+"{}", crdtest.Child(schema, "additionalProperties"), typ.Elem())
	}
}

// collectFields maps the JSON names of a struct's fields to their types,
// flattening inline embeds.
func collectFields(typ reflect.Type, out map[string]reflect.Type) {
	for f := range typ.Fields() {
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" && strings.Contains(opts, "inline") {
			collectFields(f.Type, out)
			continue
		}
		out[name] = f.Type
	}
}

func TestSchemaRejections(t *testing.T) {
	cases := []struct {
		file, name, spec string
		wantErr          bool
	}{
		{"crd-virtualmodel.yaml", "lowest cost", "{family: a, name: b, displayName: B, strategy: lowest_cost}", false},
		{"crd-virtualmodel.yaml", "unknown strategy", "{family: a, name: b, displayName: B, strategy: random}", true},
		{"crd-virtualmodel.yaml", "uppercase family", "{family: Anthropic, name: b, displayName: B}", true},
		{"crd-virtualmodel.yaml", "provider without type", "{family: a, name: b, displayName: B, routes: [{provider: bedrock, modelID: m}]}", true},
		{"crd-virtualmodel.yaml", "weight above 100", "{family: a, name: b, displayName: B, routes: [{provider: 'bedrock:p', modelID: m, weight: 101}]}", true},
		{"crd-virtualmodel.yaml", "priority zero", "{family: a, name: b, displayName: B, routes: [{provider: 'bedrock:p', modelID: m, priority: 0}]}", true},
		{"crd-virtualmodel.yaml", "no display name", "{family: a, name: b}", true},
		{"crd-environment.yaml", "no display name", "{sandboxImage: kora-go}", true},
		{"crd-environment.yaml", "connector without actions", "{displayName: E, connectors: [{connection: gh, actions: []}]}", true},
		{"crd-agent.yaml", "no model", "{displayName: A}", true},
		{"crd-agent.yaml", "unknown framework", "{displayName: A, model: {family: a, name: b}, framework: cursor}", true},
		{"crd-agent.yaml", "unknown identity mode", "{displayName: A, model: {family: a, name: b}, identityMode: shared}", true},
		{"crd-agent.yaml", "unknown connectivity", "{displayName: A, model: {family: a, name: b}, connectivity: {mode: some}}", true},
		{"crd-agent.yaml", "whitelist", "{displayName: A, model: {family: a, name: b}, connectivity: {mode: whitelist, hosts: [github.com]}}", false},
		{"crd-agent.yaml", "mcp server without tools", "{displayName: A, model: {family: a, name: b}, mcpTools: [{server: gh, tools: []}]}", true},
		{"crd-agent.yaml", "negative budget", "{displayName: A, model: {family: a, name: b}, budget: {limitUSD: -1}}", true},
		{"crd-agent.yaml", "daily cadence", "{displayName: A, model: {family: a, name: b}, budget: {limitUSD: 1, resetCadence: daily}}", true},
		{"crd-agent.yaml", "guardrail zero", "{displayName: A, model: {family: a, name: b}, guardrails: {repeatedCallWarn: 0}}", true},
		{"crd-agent.yaml", "guardrails", "{displayName: A, model: {family: a, name: b}, guardrails: {repeatedCallWarn: 3, repeatedCallPause: 6}, maxConcurrentRunsPerPod: 4, scopes: ['agents:runs:x:read']}", false},
		{"crd-triggersource.yaml", "no secret ref", "{provider: github}", true},
		{"crd-triggersource.yaml", "secret ref without key", "{provider: github, signingSecretRef: {name: s}}", true},
		{"crd-triggersource.yaml", "free-form config", "{provider: slack, config: {team: T1, nested: {a: [1, 2]}}, signingSecretRef: {name: s, key: k}, enabled: false}", false},
		{"crd-trigger.yaml", "no prompt", "{source: s, eventType: e, agent: a}", true},
		{"crd-trigger.yaml", "unknown kind", "{kind: email, agent: a, promptTemplate: p}", true},
		{"crd-trigger.yaml", "max parallel above 16", "{source: s, eventType: e, agent: a, promptTemplate: p, maxParallel: 17}", true},
		{"crd-trigger.yaml", "queue depth above 500", "{source: s, eventType: e, agent: a, promptTemplate: p, maxQueueDepth: 501}", true},
		{"crd-trigger.yaml", "timeout below 5", "{source: s, eventType: e, agent: a, promptTemplate: p, timeoutSeconds: 4}", true},
		{"crd-trigger.yaml", "unknown overflow", "{source: s, eventType: e, agent: a, promptTemplate: p, overflow: drop}", true},
		{"crd-trigger.yaml", "coalesce window above 300", "{source: s, eventType: e, agent: a, promptTemplate: p, coalesce: {key: k, windowSeconds: 301}}", true},
		{"crd-trigger.yaml", "21 candidates", "{source: s, eventType: e, promptTemplate: p, routing: {candidates: [a,b,c,d,e,f,g,h,i,j,k,l,m,o,p,q,r,s,t,u,v], model: {family: f, name: d1}}}", true},
		{"crd-trigger.yaml", "unknown confidence", "{source: s, eventType: e, promptTemplate: p, routing: {candidates: [a], model: {family: f, name: d1}, confidence: high}}", true},
		{"crd-trigger.yaml", "unknown input type", "{kind: manual, agent: a, promptTemplate: p, manual: {inputs: [{name: x, type: date}]}}", true},
		{"crd-trigger.yaml", "every field", `{source: s, eventType: e, promptTemplate: p, match: {action: [opened]}, matchContains: {labels: [bug]},
			senderRestriction: {ids: [x], roles: [MEMBER]}, coalesce: {key: k, windowSeconds: 30},
			routing: {candidates: [a, b], model: {family: liquid, name: d1}, confidence: strict, fallback: a, joinRuns: false, joinKey: k},
			maxParallel: 4, overflow: wait, maxQueueDepth: 10, timeoutSeconds: 60, dedupeEnabled: true, autopauseEnabled: false, enabled: true}`, false},
		{"crd-trigger.yaml", "scheduled", "{kind: scheduled, agent: a, promptTemplate: p, schedule: {cronExpression: '0 9 * * 1', timezone: Australia/Sydney, catchUp: true}}", false},
		{"crd-policyrole.yaml", "no scopes", "{role: r}", true},
		{"crd-policyrole.yaml", "empty scopes", "{scopes: []}", true},
		{"crd-policyrole.yaml", "zero budget", "{scopes: [a], budget: {limitUSD: 0}}", true},
		{"crd-policyrole.yaml", "system role with a tenant", "{tenant: acme, role: admins, scopes: ['platform:catalog:write']}", false},
		{"crd-policyrole.yaml", "empty tenant", "{tenant: '', scopes: [a]}", true},
		{"crd-policyrole.yaml", "pooled weekly", "{scopes: [a], budget: {limitUSD: 10, resetCadence: weekly, pooled: true}}", false},
	}
	for _, tc := range cases {
		t.Run(tc.file+"/"+tc.name, func(t *testing.T) {
			var spec map[string]any
			if err := yaml.Unmarshal([]byte(tc.spec), &spec); err != nil {
				t.Fatal(err)
			}
			obj := map[string]any{"apiVersion": "runkora.io/v1alpha1", "metadata": map[string]any{"name": "x"}, "spec": spec}
			errs := crdtest.Validate(crdtest.Schema(t, tc.file), obj, "")
			if (len(errs) > 0) != tc.wantErr {
				t.Errorf("errors = %v, want error %v", errs, tc.wantErr)
			}
		})
	}
}

func TestPolicyRoleName(t *testing.T) {
	r := &PolicyRole{ObjectMeta: metav1.ObjectMeta{Name: "reviewers"}}
	if r.RoleName() != "reviewers" {
		t.Errorf("RoleName() = %q, want metadata.name", r.RoleName())
	}
	r.Spec.Role = "Reviewers"
	if r.RoleName() != "Reviewers" {
		t.Errorf("RoleName() = %q, want spec.role", r.RoleName())
	}
}

func TestAddToScheme(t *testing.T) {
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"VirtualModel", "Environment", "Agent", "TriggerSource", "Trigger", "PolicyRole"} {
		for _, k := range []string{kind, kind + "List"} {
			if !s.Recognizes(GroupVersion.WithKind(k)) {
				t.Errorf("scheme does not recognise %s", k)
			}
		}
	}
}
