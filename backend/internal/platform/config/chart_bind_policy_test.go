package config_test

import (
	"testing"

	"github.com/google/cel-go/cel"
)

// bindPolicy renders the kora-agent-bind ValidatingAdmissionPolicy and returns a
// function that evaluates it for one pod update, as the API server does.
func bindPolicy(t *testing.T) func(oldLabels, newLabels map[string]any, user string) bool {
	t.Helper()
	var policy map[string]any
	for _, doc := range renderDocs(t, "templates/agents/bind-policy.yaml") {
		if doc["kind"] == "ValidatingAdmissionPolicy" {
			policy = doc
		}
	}
	if policy == nil {
		t.Fatal("no kora-agent-bind ValidatingAdmissionPolicy")
	}
	env, err := cel.NewEnv(
		cel.Variable("object", cel.DynType),
		cel.Variable("oldObject", cel.DynType),
		cel.Variable("request", cel.DynType),
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
	type variable struct {
		name string
		prg  cel.Program
	}
	var vars []variable
	for _, v := range field(policy, "spec", "variables").([]any) {
		m := v.(map[string]any)
		vars = append(vars, variable{m["name"].(string), compile(m["expression"].(string))})
	}
	var checks []cel.Program
	for _, v := range field(policy, "spec", "validations").([]any) {
		checks = append(checks, compile(v.(map[string]any)["expression"].(string)))
	}
	pod := func(labels map[string]any) map[string]any {
		meta := map[string]any{"name": "p"}
		if labels != nil {
			meta["labels"] = labels
		}
		return map[string]any{"metadata": meta}
	}
	return func(oldLabels, newLabels map[string]any, user string) bool {
		in := map[string]any{
			"object":    pod(newLabels),
			"oldObject": pod(oldLabels),
			"request":   map[string]any{"userInfo": map[string]any{"username": user}},
		}
		values := map[string]any{}
		for _, v := range vars {
			in["variables"] = values
			out, _, err := v.prg.Eval(in)
			if err != nil {
				t.Fatalf("variable %s: %v", v.name, err)
			}
			values[v.name] = out.Value()
		}
		in["variables"] = values
		for _, c := range checks {
			out, _, err := c.Eval(in)
			if err != nil {
				t.Fatalf("validation: %v", err)
			}
			if out.Value() != true {
				return false
			}
		}
		return true
	}
}

func TestChartBindPolicy(t *testing.T) {
	allowed := bindPolicy(t)
	const agents = "system:serviceaccount:kora-system:kora-agents"
	pool := map[string]any{"runkora.io/pool": "true", "runkora.io/managed-agent": "true"}
	bound := map[string]any{"spiffe.io/agent": "reviewer", "runkora.io/managed-agent": "true"}
	tests := []struct {
		name     string
		old, new map[string]any
		user     string
		want     bool
	}{
		{"bind by kora-agents", pool, bound, agents, true},
		{"bind by another account", pool, bound, "system:serviceaccount:kora-tenant-a:default", false},
		{"bind that keeps the pool label", pool, map[string]any{"runkora.io/pool": "true", "spiffe.io/agent": "reviewer"}, agents, false},
		{"agent label on a pod outside the pool", map[string]any{"app": "x"}, map[string]any{"app": "x", "spiffe.io/agent": "reviewer"}, agents, false},
		{"move to another agent", bound, map[string]any{"spiffe.io/agent": "other", "runkora.io/managed-agent": "true"}, agents, false},
		{"remove the agent label", bound, map[string]any{"runkora.io/managed-agent": "true"}, agents, false},
		{"other label change on a bound pod", bound, map[string]any{"spiffe.io/agent": "reviewer", "runkora.io/managed-agent": "true", "x": "y"}, "anyone", true},
		{"pod with no labels", nil, nil, "anyone", true},
		{"pool pod leaves the pool unbound", pool, map[string]any{"runkora.io/managed-agent": "true"}, "anyone", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.old, tc.new, tc.user); got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}
