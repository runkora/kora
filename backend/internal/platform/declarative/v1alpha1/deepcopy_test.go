package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

// full sets every field of every kind, so a shared slice, map or pointer
// anywhere shows up.
var full = map[string]string{
	"VirtualModel": `
metadata: {name: m, labels: {a: b}}
spec: {family: a, name: b, displayName: B, routes: [{provider: 'bedrock:p', modelID: m, priority: 1, weight: 100, enabled: true}]}
status: {id: v1, observedGeneration: 1, conditions: [{type: Ready, status: "True", reason: R, message: M, lastTransitionTime: "2026-10-24T00:00:00Z"}]}`,
	"Environment": `
metadata: {name: e}
spec: {displayName: E, size: {cpu: "1"}, egressHosts: [a], connectors: [{connection: gh, actions: [read], resources: [r]}]}
status: {conditions: [{type: Ready, status: "True", reason: R, message: M, lastTransitionTime: "2026-10-24T00:00:00Z"}]}`,
	"Agent": `
metadata: {name: a}
spec:
  displayName: A
  model: {family: a, name: b}
  environments: [{name: e, default: true}]
  connectivity: {mode: whitelist, hosts: [h]}
  connectors: [{connection: gh, actions: [read], resources: [r]}]
  mcpTools: [{server: s, tools: [t]}]
  scopes: [x]
  guardrails: {repeatedCallWarn: 1, repeatedCallPause: 2, waitRearmWarn: 3, waitRearmPause: 4}
  budget: {limitUSD: 1, perRunLimitUSD: 2}
status: {conditions: [{type: Ready, status: "True", reason: R, message: M, lastTransitionTime: "2026-10-24T00:00:00Z"}]}`,
	"TriggerSource": `
metadata: {name: s}
spec: {provider: github, config: {org: acme}, signingSecretRef: {name: s, key: k}}
status: {webhookPath: /p, conditions: [{type: Ready, status: "True", reason: R, message: M, lastTransitionTime: "2026-10-24T00:00:00Z"}]}`,
	"Trigger": `
metadata: {name: t}
spec:
  match: {a: [b]}
  matchContains: {c: [d]}
  senderRestriction: {ids: [i], roles: [r]}
  coalesce: {key: k, windowSeconds: 1}
  routing: {candidates: [a], model: {family: f, name: m}, joinRuns: true}
  schedule: {cronExpression: '* * * * *'}
  manual: {inputs: [{name: x, type: choice, choices: [a]}]}
  promptTemplate: p
  maxParallel: 1
  overflow: wait
  maxQueueDepth: 1
  timeoutSeconds: 5
  dedupeEnabled: true
  autopauseEnabled: true
status: {conditions: [{type: Ready, status: "True", reason: R, message: M, lastTransitionTime: "2026-10-24T00:00:00Z"}]}`,
	"PolicyRole": `
metadata: {name: r}
spec: {tenant: acme, role: r, scopes: [a], budget: {limitUSD: 1}}
status: {conditions: [{type: Ready, status: "True", reason: R, message: M, lastTransitionTime: "2026-10-24T00:00:00Z"}]}`,
}

func TestDeepCopySharesNothing(t *testing.T) {
	objs := map[string]runtime.Object{
		"VirtualModel": &VirtualModel{}, "Environment": &Environment{}, "Agent": &Agent{},
		"TriggerSource": &TriggerSource{}, "Trigger": &Trigger{}, "PolicyRole": &PolicyRole{},
	}
	for kind, obj := range objs {
		t.Run(kind, func(t *testing.T) {
			if err := yaml.UnmarshalStrict([]byte(full[kind]), obj); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(obj)
			cp := obj.DeepCopyObject()
			if !reflect.DeepEqual(cp, obj) {
				t.Fatal("copy differs from the original")
			}
			mutate(reflect.ValueOf(cp))
			after, _ := json.Marshal(obj)
			if string(before) != string(after) {
				t.Errorf("mutating the copy changed the original:\n%s\n%s", before, after)
			}

			list := listOf(t, obj)
			lcp := list.DeepCopyObject()
			mutate(reflect.ValueOf(lcp))
			if afterList, _ := json.Marshal(obj); string(before) != string(afterList) {
				t.Error("mutating a copied list changed the original item")
			}
		})
	}
}

// listOf wraps obj in its list type.
func listOf(t *testing.T, obj runtime.Object) runtime.Object {
	t.Helper()
	switch o := obj.(type) {
	case *VirtualModel:
		return &VirtualModelList{Items: []VirtualModel{*o}}
	case *Environment:
		return &EnvironmentList{Items: []Environment{*o}}
	case *Agent:
		return &AgentList{Items: []Agent{*o}}
	case *TriggerSource:
		return &TriggerSourceList{Items: []TriggerSource{*o}}
	case *Trigger:
		return &TriggerList{Items: []Trigger{*o}}
	case *PolicyRole:
		return &PolicyRoleList{Items: []PolicyRole{*o}}
	}
	t.Fatalf("no list for %T", obj)
	return nil
}

// mutate changes every value reachable through a pointer, slice or map.
func mutate(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			mutate(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				mutate(v.Field(i))
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 && v.CanSet() {
			for i := range v.Len() {
				v.Index(i).SetUint(0)
			}
			return
		}
		for i := range v.Len() {
			mutate(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(v.MapIndex(k))
			mutate(e)
			if e.Kind() == reflect.String {
				e.SetString("changed")
			}
			v.SetMapIndex(k, e)
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString("changed")
		}
	case reflect.Int, reflect.Int32, reflect.Int64:
		if v.CanSet() {
			v.SetInt(v.Int() + 7)
		}
	case reflect.Float64:
		if v.CanSet() {
			v.SetFloat(v.Float() + 7)
		}
	case reflect.Bool:
		if v.CanSet() {
			v.SetBool(!v.Bool())
		}
	}
}
