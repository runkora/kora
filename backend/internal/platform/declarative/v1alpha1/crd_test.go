package v1alpha1

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

const crdDir = "../../../../../charts/kora-crds/templates/"

// schemaNode is a node of a CRD's openAPIV3Schema, as decoded from YAML.
type schemaNode = map[string]any

// crdSchema loads the openAPIV3Schema the chart ships for one kind. Lines
// with Helm template actions carry only labels and are dropped.
//
// The schema is walked as plain data. Running the API server's validators
// and CEL here would pull k8s.io/apiserver and cel-go into this module. The
// CEL rules are asserted as declared, not executed.
func crdSchema(t *testing.T, file string) schemaNode {
	t.Helper()
	raw, err := os.ReadFile(crdDir + file)
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
	spec := child(crd, "spec")
	if spec["scope"] != "Namespaced" {
		t.Errorf("%s: scope = %v, want Namespaced", file, spec["scope"])
	}
	versions, _ := spec["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("%s: want one version, got %d", file, len(versions))
	}
	version := versions[0].(schemaNode)
	if version["name"] != "v1alpha1" || child(child(version, "subresources"), "status") == nil {
		t.Errorf("%s: want v1alpha1 with a status subresource", file)
	}
	schema := child(child(version, "schema"), "openAPIV3Schema")
	if schema == nil {
		t.Fatalf("%s: no openAPIV3Schema", file)
	}
	return schema
}

func child(n schemaNode, key string) schemaNode {
	c, _ := n[key].(schemaNode)
	return c
}

func prop(n schemaNode, name string) schemaNode { return child(child(n, "properties"), name) }

// validate checks value against the OpenAPI keywords these CRDs use. It
// returns one message per violation.
func validate(schema schemaNode, value any, path string) []string {
	var errs []string
	fail := func(format string, args ...any) { errs = append(errs, path+": "+fmt.Sprintf(format, args...)) }
	num := func(key string) (float64, bool) { n, ok := schema[key].(float64); return n, ok }
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
			switch {
			case prop(schema, k) != nil:
				errs = append(errs, validate(prop(schema, k), item, path+"."+k)...)
			case child(schema, "additionalProperties") != nil:
				errs = append(errs, validate(child(schema, "additionalProperties"), item, path+"."+k)...)
			case schema["x-kubernetes-preserve-unknown-fields"] == true || path == ".metadata":
			default:
				fail("unknown field %s", k)
			}
		}
	case []any:
		if schema["type"] != "array" {
			fail("got an array, want %v", schema["type"])
		}
		if n, ok := num("minItems"); ok && float64(len(v)) < n {
			fail("needs at least %v items", n)
		}
		if n, ok := num("maxItems"); ok && float64(len(v)) > n {
			fail("allows at most %v items", n)
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
		if n, ok := num("minLength"); ok && float64(len(v)) < n {
			fail("shorter than %v", n)
		}
		if n, ok := num("maxLength"); ok && float64(len(v)) > n {
			fail("longer than %v", n)
		}
		if pat, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pat).MatchString(v) {
			fail("%q does not match %s", v, pat)
		}
	case float64:
		if schema["type"] != "integer" && schema["type"] != "number" {
			fail("got a number, want %v", schema["type"])
		}
		if n, ok := num("minimum"); ok && (v < n || (v == n && schema["exclusiveMinimum"] == true)) {
			fail("%v is below the minimum %v", v, n)
		}
		if n, ok := num("maximum"); ok && v > n {
			fail("%v is above the maximum %v", v, n)
		}
	case bool:
		if schema["type"] != "boolean" {
			fail("got a boolean, want %v", schema["type"])
		}
	default:
		fail("unexpected %T, want %v", value, schema["type"])
	}
	return errs
}

// rules returns the CEL rules declared on a schema node.
func rules(n schemaNode) []string {
	var out []string
	list, _ := n["x-kubernetes-validations"].([]any)
	for _, r := range list {
		out = append(out, r.(schemaNode)["rule"].(string))
	}
	return out
}

// kinds maps each CRD file to its Go type.
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
var adrExamples = map[string]string{
	"crd-virtualmodel.yaml": `
apiVersion: runkora.io/v1alpha1
kind: VirtualModel
metadata:
  name: claude-sonnet
  namespace: kora-system
spec:
  family: anthropic
  name: sonnet
  displayName: Claude Sonnet
  strategy: priority
  routes:
    - provider: bedrock:prod
      modelID: anthropic.claude-sonnet-4-5-20250929-v1:0
      priority: 1
`,
	"crd-environment.yaml": `
apiVersion: runkora.io/v1alpha1
kind: Environment
metadata:
  name: go-build
  namespace: kora-tenant-acme
spec:
  displayName: Go build
  sandboxImage: kora-go
  size: {cpu: "2", memory: 4Gi, disk: 10Gi}
  egressHosts: [proxy.golang.org]
`,
	"crd-agent.yaml": `
apiVersion: runkora.io/v1alpha1
kind: Agent
metadata:
  name: code-reviewer
  namespace: kora-tenant-acme
spec:
  displayName: Code reviewer
  framework: claude-code
  identityMode: autonomous
  model: {family: anthropic, name: sonnet}
  systemPrompt: |
    Review pull requests for correctness.
  environments:
    - name: go-build
      default: true
  connectors:
    - connection: github
      actions: [read, write]
  mcpTools:
    - server: github
      tools: [create_review]
  budget: {limitUSD: 50, resetCadence: monthly}
`,
	"crd-triggersource.yaml": `
apiVersion: runkora.io/v1alpha1
kind: TriggerSource
metadata:
  name: github-org
  namespace: kora-tenant-acme
spec:
  provider: github
  signingSecretRef: {name: github-webhook, key: secret}
`,
	"crd-trigger.yaml": `
apiVersion: runkora.io/v1alpha1
kind: Trigger
metadata:
  name: review-on-pr
  namespace: kora-tenant-acme
spec:
  kind: webhook
  source: github-org
  eventType: pull_request
  agent: code-reviewer
  promptTemplate: "Review {{ pull_request.html_url }}"
`,
	"crd-policyrole.yaml": `
apiVersion: runkora.io/v1alpha1
kind: PolicyRole
metadata:
  name: reviewers
  namespace: kora-tenant-acme
spec:
  role: Reviewers
  scopes: ["agents:runs:code-reviewer:read", "agents:runs:code-reviewer:write"]
  budget: {limitUSD: 200, resetCadence: monthly}
`,
}

func TestADRExamplesValidate(t *testing.T) {
	for _, k := range kinds {
		t.Run(k.file, func(t *testing.T) {
			var obj map[string]any
			if err := yaml.Unmarshal([]byte(adrExamples[k.file]), &obj); err != nil {
				t.Fatal(err)
			}
			if errs := validate(crdSchema(t, k.file), obj, ""); len(errs) > 0 {
				t.Errorf("schema errors: %v", errs)
			}
			// Strict decoding fails on a field the Go type does not declare.
			typed := k.obj.DeepCopyObject()
			if err := yaml.UnmarshalStrict([]byte(adrExamples[k.file]), typed); err != nil {
				t.Errorf("decode into %T: %v", typed, err)
			}
		})
	}
}

// TestGoTypesMatchSchema keeps the Go types and the chart's schemas in step:
// every spec and status field exists on both sides.
func TestGoTypesMatchSchema(t *testing.T) {
	for _, k := range kinds {
		t.Run(k.file, func(t *testing.T) {
			schema := crdSchema(t, k.file)
			typ := reflect.TypeOf(k.obj).Elem()
			for _, f := range []string{"Spec", "Status"} {
				field, _ := typ.FieldByName(f)
				compareFields(t, "."+strings.ToLower(f), prop(schema, strings.ToLower(f)), field.Type)
			}
		})
	}
}

var (
	conditionType = reflect.TypeFor[metav1.Condition]()
	rawType       = reflect.TypeFor[runtime.RawExtension]()
)

func compareFields(t *testing.T, path string, schema schemaNode, typ reflect.Type) {
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
			compareFields(t, path+"."+name, prop(schema, name), ft)
		}
		for name := range child(schema, "properties") {
			if _, ok := goFields[name]; !ok {
				t.Errorf("%s.%s: in the schema but not in Go type %s", path, name, typ)
			}
		}
	case reflect.Slice:
		compareFields(t, path+"[]", child(schema, "items"), typ.Elem())
	case reflect.Map:
		compareFields(t, path+"{}", child(schema, "additionalProperties"), typ.Elem())
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

func TestStatusConditionsAreAMapByType(t *testing.T) {
	for _, k := range kinds {
		conditions := prop(prop(crdSchema(t, k.file), "status"), "conditions")
		keys, _ := conditions["x-kubernetes-list-map-keys"].([]any)
		if conditions["x-kubernetes-list-type"] != "map" || len(keys) != 1 || keys[0] != "type" {
			t.Errorf("%s: status.conditions must be a list map keyed by type", k.file)
		}
	}
}

func TestMetadataNameRules(t *testing.T) {
	const dnsLabel = "self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63"
	for _, k := range kinds {
		got := rules(crdSchema(t, k.file))
		if k.file == "crd-agent.yaml" {
			want := []string{
				"self.metadata.name.matches('^[a-z]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 52",
				"!(self.metadata.name in ['delegated', 'autonomous'])",
			}
			if !slices.Equal(got, want) {
				t.Errorf("agent name rules = %q, want %q", got, want)
			}
			continue
		}
		if !slices.Equal(got, []string{dnsLabel}) {
			t.Errorf("%s: name rules = %q", k.file, got)
		}
	}
}

func TestAgentIdentityModeIsImmutable(t *testing.T) {
	mode := prop(prop(crdSchema(t, "crd-agent.yaml"), "spec"), "identityMode")
	if !slices.Equal(rules(mode), []string{"self == oldSelf"}) {
		t.Errorf("identityMode rules = %q", rules(mode))
	}
}

func TestTriggerRulesAreDeclared(t *testing.T) {
	want := []string{
		"has(self.agent) != has(self.routing)",
		"self.kind != 'webhook' || (has(self.source) && has(self.eventType))",
		"self.kind != 'scheduled' || has(self.schedule)",
		"self.kind == 'webhook' || !(has(self.source) || has(self.eventType) || has(self.match) || has(self.matchContains) || has(self.senderRestriction) || has(self.coalesce) || has(self.routing))",
		"self.kind == 'scheduled' || !has(self.schedule)",
		"self.kind == 'manual' || !has(self.manual)",
	}
	if got := rules(prop(crdSchema(t, "crd-trigger.yaml"), "spec")); !slices.Equal(got, want) {
		t.Errorf("trigger spec rules =\n%q\nwant\n%q", got, want)
	}
}

// TestSchemaRejections covers the OpenAPI validation each kind mirrors from
// its API.
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
		{"crd-policyrole.yaml", "pooled weekly", "{scopes: [a], budget: {limitUSD: 10, resetCadence: weekly, pooled: true}}", false},
	}
	for _, tc := range cases {
		t.Run(tc.file+"/"+tc.name, func(t *testing.T) {
			var spec map[string]any
			if err := yaml.Unmarshal([]byte(tc.spec), &spec); err != nil {
				t.Fatal(err)
			}
			obj := map[string]any{"apiVersion": "runkora.io/v1alpha1", "metadata": map[string]any{"name": "x"}, "spec": spec}
			errs := validate(crdSchema(t, tc.file), obj, "")
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
