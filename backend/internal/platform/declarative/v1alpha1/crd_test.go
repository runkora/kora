package v1alpha1

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/crdtest"
)

var kinds = []struct {
	file string
	obj  runtime.Object
}{
	{"generated/runkora.io_virtualmodels.yaml", &VirtualModel{}},
	{"generated/runkora.io_environments.yaml", &Environment{}},
	{"generated/runkora.io_agents.yaml", &Agent{}},
	{"generated/runkora.io_triggersources.yaml", &TriggerSource{}},
	{"generated/runkora.io_triggers.yaml", &Trigger{}},
	{"generated/runkora.io_policyroles.yaml", &PolicyRole{}},
}

// TestExampleManifestsValidate checks the local examples in
// deploy/local/declarative and the complete examples in the guide against the
// schemas and the Go types. Snippets without apiVersion are skipped.
func TestExampleManifestsValidate(t *testing.T) {
	sources := map[string]string{}
	for _, f := range []string{"agent.yaml", "policy-role.yaml", "triggers.yaml", "virtual-model.yaml"} {
		raw, err := os.ReadFile("../../../../../deploy/local/declarative/" + f)
		if err != nil {
			t.Fatal(err)
		}
		sources[f] = string(raw)
	}
	guide, err := os.ReadFile("../../../../../docs/guides/declarative-resources.md")
	if err != nil {
		t.Fatal(err)
	}
	for i, block := range regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(guide), -1) {
		sources[fmt.Sprintf("guide block %d", i)] = block[1]
	}
	byKind := map[string]int{}
	for i, k := range kinds {
		byKind[reflect.TypeOf(k.obj).Elem().Name()] = i
	}
	checked := 0
	for src, text := range sources {
		for doc := range strings.SplitSeq(text, "\n---\n") {
			var head struct{ APIVersion, Kind string }
			if err := yaml.Unmarshal([]byte(doc), &head); err != nil {
				t.Fatalf("%s: %v", src, err)
			}
			i, ok := byKind[head.Kind]
			if !ok || head.APIVersion != GroupVersion.String() {
				continue
			}
			var obj map[string]any
			if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
				t.Fatalf("%s: %v", src, err)
			}
			if err := crdtest.Validate(t, crdtest.Schema(t, kinds[i].file), obj); err != nil {
				t.Errorf("%s %s: schema errors: %v", src, head.Kind, err)
			}
			if err := yaml.UnmarshalStrict([]byte(doc), kinds[i].obj.DeepCopyObject()); err != nil {
				t.Errorf("%s %s: decode: %v", src, head.Kind, err)
			}
			checked++
		}
	}
	if checked < 7 {
		t.Errorf("checked %d manifests, want the 7 local examples at least", checked)
	}
}

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

func TestSchemaRejections(t *testing.T) {
	cases := []struct {
		file, name, spec string
		wantErr          bool
	}{
		{"generated/runkora.io_virtualmodels.yaml", "lowest cost", "{family: a, name: b, displayName: B, strategy: lowest_cost}", false},
		{"generated/runkora.io_virtualmodels.yaml", "unknown strategy", "{family: a, name: b, displayName: B, strategy: random}", true},
		{"generated/runkora.io_virtualmodels.yaml", "uppercase family", "{family: Anthropic, name: b, displayName: B}", true},
		{"generated/runkora.io_virtualmodels.yaml", "provider without type", "{family: a, name: b, displayName: B, routes: [{provider: bedrock, modelID: m}]}", true},
		{"generated/runkora.io_virtualmodels.yaml", "weight above 100", "{family: a, name: b, displayName: B, routes: [{provider: 'bedrock:p', modelID: m, weight: 101}]}", true},
		{"generated/runkora.io_virtualmodels.yaml", "priority zero", "{family: a, name: b, displayName: B, routes: [{provider: 'bedrock:p', modelID: m, priority: 0}]}", true},
		{"generated/runkora.io_virtualmodels.yaml", "no display name", "{family: a, name: b}", true},
		{"generated/runkora.io_environments.yaml", "no display name", "{sandboxImage: kora-go}", true},
		{"generated/runkora.io_environments.yaml", "connector without actions", "{displayName: E, connectors: [{connection: gh, actions: []}]}", true},
		{"generated/runkora.io_agents.yaml", "no model", "{displayName: A}", true},
		{"generated/runkora.io_agents.yaml", "unknown framework", "{displayName: A, model: {family: a, name: b}, framework: cursor}", true},
		{"generated/runkora.io_agents.yaml", "unknown identity mode", "{displayName: A, model: {family: a, name: b}, identityMode: shared}", true},
		{"generated/runkora.io_agents.yaml", "unknown connectivity", "{displayName: A, model: {family: a, name: b}, connectivity: {mode: some}}", true},
		{"generated/runkora.io_agents.yaml", "whitelist", "{displayName: A, model: {family: a, name: b}, connectivity: {mode: whitelist, hosts: [github.com]}}", false},
		{"generated/runkora.io_agents.yaml", "mcp server without tools", "{displayName: A, model: {family: a, name: b}, mcpTools: [{server: gh, tools: []}]}", true},
		{"generated/runkora.io_agents.yaml", "negative budget", "{displayName: A, model: {family: a, name: b}, budget: {limitUSD: -1}}", true},
		{"generated/runkora.io_agents.yaml", "daily cadence", "{displayName: A, model: {family: a, name: b}, budget: {limitUSD: 1, resetCadence: daily}}", true},
		{"generated/runkora.io_agents.yaml", "guardrail zero", "{displayName: A, model: {family: a, name: b}, guardrails: {repeatedCallWarn: 0}}", true},
		{"generated/runkora.io_agents.yaml", "guardrails", "{displayName: A, model: {family: a, name: b}, guardrails: {repeatedCallWarn: 3, repeatedCallPause: 6}, maxConcurrentRunsPerPod: 4, scopes: ['agents:runs:x:read']}", false},
		{"generated/runkora.io_triggersources.yaml", "no secret ref", "{provider: github}", true},
		{"generated/runkora.io_triggersources.yaml", "secret ref without key", "{provider: github, signingSecretRef: {name: s}}", true},
		{"generated/runkora.io_triggersources.yaml", "free-form config", "{provider: slack, config: {team: T1, nested: {a: [1, 2]}}, signingSecretRef: {name: s, key: k}, enabled: false}", false},
		{"generated/runkora.io_triggers.yaml", "no prompt", "{source: s, eventType: e, agent: a}", true},
		{"generated/runkora.io_triggers.yaml", "unknown kind", "{kind: email, agent: a, promptTemplate: p}", true},
		{"generated/runkora.io_triggers.yaml", "max parallel above 16", "{source: s, eventType: e, agent: a, promptTemplate: p, maxParallel: 17}", true},
		{"generated/runkora.io_triggers.yaml", "queue depth above 500", "{source: s, eventType: e, agent: a, promptTemplate: p, maxQueueDepth: 501}", true},
		{"generated/runkora.io_triggers.yaml", "timeout below 5", "{source: s, eventType: e, agent: a, promptTemplate: p, timeoutSeconds: 4}", true},
		{"generated/runkora.io_triggers.yaml", "unknown overflow", "{source: s, eventType: e, agent: a, promptTemplate: p, overflow: drop}", true},
		{"generated/runkora.io_triggers.yaml", "coalesce window above 300", "{source: s, eventType: e, agent: a, promptTemplate: p, coalesce: {key: k, windowSeconds: 301}}", true},
		{"generated/runkora.io_triggers.yaml", "21 candidates", "{source: s, eventType: e, promptTemplate: p, routing: {candidates: [a,b,c,d,e,f,g,h,i,j,k,l,m,o,p,q,r,s,t,u,v], model: {family: f, name: d1}}}", true},
		{"generated/runkora.io_triggers.yaml", "unknown confidence", "{source: s, eventType: e, promptTemplate: p, routing: {candidates: [a], model: {family: f, name: d1}, confidence: high}}", true},
		{"generated/runkora.io_triggers.yaml", "unknown input type", "{kind: manual, agent: a, promptTemplate: p, manual: {inputs: [{name: x, type: date}]}}", true},
		{"generated/runkora.io_triggers.yaml", "every field", `{source: s, eventType: e, promptTemplate: p, match: {action: [opened]}, matchContains: {labels: [bug]},
			senderRestriction: {ids: [x], roles: [MEMBER]}, coalesce: {key: k, windowSeconds: 30},
			routing: {candidates: [a, b], model: {family: liquid, name: d1}, confidence: strict, fallback: a, joinRuns: false, joinKey: k},
			maxParallel: 4, overflow: wait, maxQueueDepth: 10, timeoutSeconds: 60, dedupeEnabled: true, autopauseEnabled: false, enabled: true}`, false},
		{"generated/runkora.io_triggers.yaml", "scheduled", "{kind: scheduled, agent: a, promptTemplate: p, schedule: {cronExpression: '0 9 * * 1', timezone: Australia/Sydney, catchUp: true}}", false},
		{"generated/runkora.io_policyroles.yaml", "no scopes", "{role: r}", true},
		{"generated/runkora.io_policyroles.yaml", "empty scopes", "{scopes: []}", true},
		{"generated/runkora.io_policyroles.yaml", "zero budget", "{scopes: [a], budget: {limitUSD: 0}}", true},
		{"generated/runkora.io_policyroles.yaml", "system role with a tenant", "{tenant: acme, role: admins, scopes: ['platform:catalog:write']}", false},
		{"generated/runkora.io_policyroles.yaml", "empty tenant", "{tenant: '', scopes: [a]}", true},
		{"generated/runkora.io_policyroles.yaml", "pooled weekly", "{scopes: [a], budget: {limitUSD: 10, resetCadence: weekly, pooled: true}}", false},
	}
	for _, tc := range cases {
		t.Run(tc.file+"/"+tc.name, func(t *testing.T) {
			var spec map[string]any
			if err := yaml.Unmarshal([]byte(tc.spec), &spec); err != nil {
				t.Fatal(err)
			}
			obj := map[string]any{"apiVersion": "runkora.io/v1alpha1", "metadata": map[string]any{"name": "x"}, "spec": spec}
			err := crdtest.Validate(t, crdtest.Schema(t, tc.file), obj)
			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}
