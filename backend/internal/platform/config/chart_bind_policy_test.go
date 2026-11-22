package config_test

import (
	"maps"
	"testing"

	"github.com/google/cel-go/cel"
)

// admission is one pod admission request for a rendered policy.
type admission struct {
	op, user, namespaceRole string
	old, new                map[string]any // labels; old is nil for a CREATE
	oldSpec, newSpec        string
}

// admissionPolicy renders a ValidatingAdmissionPolicy from the agents bind
// policy template and returns whether it admits a request. It evaluates the
// match conditions, the variables and the validations in order, as the API
// server does. A request the match conditions skip is admitted.
func admissionPolicy(t *testing.T, name string) func(admission) bool {
	t.Helper()
	var policy map[string]any
	for _, doc := range renderDocs(t, "templates/agents/bind-policy.yaml") {
		if doc["kind"] == "ValidatingAdmissionPolicy" && field(doc, "metadata", "name") == name {
			policy = doc
		}
	}
	if policy == nil {
		t.Fatalf("no ValidatingAdmissionPolicy %s", name)
	}
	env, err := cel.NewEnv(
		cel.Variable("object", cel.DynType),
		cel.Variable("oldObject", cel.DynType),
		cel.Variable("request", cel.DynType),
		cel.Variable("namespaceObject", cel.DynType),
		cel.Variable("variables", cel.DynType),
	)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(expr string) cel.Program {
		ast, iss := env.Compile(expr)
		if iss.Err() != nil {
			t.Fatalf("compile %q: %v", expr, iss.Err())
		}
		prg, err := env.Program(ast)
		if err != nil {
			t.Fatal(err)
		}
		return prg
	}
	list := func(path ...string) []cel.Program {
		items, _ := field(policy, path...).([]any)
		var out []cel.Program
		for _, item := range items {
			out = append(out, compile(item.(map[string]any)["expression"].(string)))
		}
		return out
	}
	conditions, checks := list("spec", "matchConditions"), list("spec", "validations")
	var names []string
	vars := list("spec", "variables")
	if items, ok := field(policy, "spec", "variables").([]any); ok {
		for _, item := range items {
			names = append(names, item.(map[string]any)["name"].(string))
		}
	}
	pod := func(labels map[string]any, spec string) any {
		meta := map[string]any{"name": "p"}
		if labels != nil {
			meta["labels"] = labels
		}
		return map[string]any{"metadata": meta, "spec": map[string]any{"serviceAccountName": spec}}
	}
	eval := func(prg cel.Program, in map[string]any) bool {
		out, _, err := prg.Eval(in)
		if err != nil {
			t.Fatalf("eval: %v", err)
		}
		return out.Value() == true
	}
	return func(a admission) bool {
		in := map[string]any{
			"object":          pod(a.new, a.newSpec),
			"oldObject":       nil,
			"request":         map[string]any{"operation": a.op, "userInfo": map[string]any{"username": a.user}},
			"namespaceObject": map[string]any{"metadata": map[string]any{"labels": map[string]any{"runkora.io/role": a.namespaceRole}}},
			"variables":       map[string]any{},
		}
		if a.op == "UPDATE" {
			in["oldObject"] = pod(a.old, a.oldSpec)
		}
		for _, c := range conditions {
			if !eval(c, in) {
				return true
			}
		}
		values := map[string]any{}
		for i, v := range vars {
			in["variables"] = values
			out, _, err := v.Eval(in)
			if err != nil {
				t.Fatalf("variable %s: %v", names[i], err)
			}
			values[names[i]] = out.Value()
		}
		in["variables"] = values
		for _, c := range checks {
			if !eval(c, in) {
				return false
			}
		}
		return true
	}
}

const (
	koraAgents = "system:serviceaccount:kora-system:kora-agents"
	rsCtrl     = "system:serviceaccount:kube-system:replicaset-controller"
)

func TestChartBindPolicy(t *testing.T) {
	allowed := admissionPolicy(t, "kora-agent-bind")
	pool := map[string]any{"runkora.io/pool": "true", "runkora.io/managed-agent": "true"}
	bound := map[string]any{"spiffe.io/agent": "reviewer", "runkora.io/managed-agent": "true"}
	with := func(base map[string]any, k, v string) map[string]any {
		out := map[string]any{}
		maps.Copy(out, base)
		out[k] = v
		return out
	}
	tests := []struct {
		name string
		a    admission
		want bool
	}{
		{"bind by kora-agents", admission{op: "UPDATE", user: koraAgents, old: pool, new: bound}, true},
		{"bind by another account", admission{op: "UPDATE", user: "system:serviceaccount:kora-tenant-a:default", old: pool, new: bound}, false},
		{"bind to an empty agent", admission{op: "UPDATE", user: koraAgents, old: pool, new: map[string]any{"spiffe.io/agent": ""}}, false},
		{"empty agent label on a pool pod", admission{op: "UPDATE", user: "anyone", old: pool, new: with(pool, "spiffe.io/agent", "")}, false},
		{"bind that keeps the pool label", admission{op: "UPDATE", user: koraAgents, old: pool, new: with(pool, "spiffe.io/agent", "reviewer")}, false},
		{"agent label on a pod outside the pool", admission{op: "UPDATE", user: koraAgents, old: map[string]any{"app": "x"}, new: map[string]any{"app": "x", "spiffe.io/agent": "reviewer"}}, false},
		{"move to another agent", admission{op: "UPDATE", user: koraAgents, old: bound, new: with(bound, "spiffe.io/agent", "other")}, false},
		{"remove the agent label", admission{op: "UPDATE", user: koraAgents, old: bound, new: map[string]any{"runkora.io/managed-agent": "true"}}, false},
		{"sandbox labels on a bound pod", admission{op: "UPDATE", user: koraAgents, old: bound, new: with(with(with(bound, "runkora.io/role", "sandbox"), "runkora.io/agent-slug", "other"), "runkora.io/run-id", "r1")}, false},
		{"service label on a bound pod", admission{op: "UPDATE", user: koraAgents, old: bound, new: with(bound, "spiffe.io/service", "svc")}, false},
		{"change a sandbox run id", admission{op: "UPDATE", user: koraAgents, old: map[string]any{"runkora.io/role": "sandbox", "runkora.io/run-id": "r1"}, new: map[string]any{"runkora.io/role": "sandbox", "runkora.io/run-id": "r2"}}, false},
		{"pool label added to an existing pod", admission{op: "UPDATE", user: koraAgents, old: map[string]any{"app": "x"}, new: map[string]any{"app": "x", "runkora.io/pool": "true"}}, false},
		{"other label change on a bound pod", admission{op: "UPDATE", user: "anyone", old: bound, new: with(bound, "x", "y")}, true},
		{"pod with no labels", admission{op: "UPDATE", user: "anyone"}, true},
		{"pool pod leaves the pool unbound", admission{op: "UPDATE", user: "anyone", old: pool, new: map[string]any{"runkora.io/managed-agent": "true"}}, true},
		{"pool pod created by the ReplicaSet controller", admission{op: "CREATE", user: rsCtrl, new: pool}, true},
		{"pool pod created by another account", admission{op: "CREATE", user: koraAgents, new: pool}, false},
		{"agent pod created by the StatefulSet controller", admission{op: "CREATE", user: "system:serviceaccount:kube-system:statefulset-controller", new: bound}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.a); got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChartKoraAgentsPodUpdatePolicy(t *testing.T) {
	allowed := admissionPolicy(t, "kora-agents-pod-updates")
	labels := map[string]any{"app": "x"}
	tests := []struct {
		name string
		a    admission
		want bool
	}{
		{"label change in a tenant namespace", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: labels, new: map[string]any{"app": "y"}, oldSpec: "sa", newSpec: "sa"}, true},
		{"spec change", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: labels, new: labels, oldSpec: "sa", newSpec: "other"}, false},
		{"update outside tenant namespaces", admission{op: "UPDATE", user: koraAgents, namespaceRole: "", old: labels, new: map[string]any{"app": "y"}, oldSpec: "sa", newSpec: "sa"}, false},
		{"another account", admission{op: "UPDATE", user: "anyone", namespaceRole: "", old: labels, new: labels, oldSpec: "sa", newSpec: "other"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.a); got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}
