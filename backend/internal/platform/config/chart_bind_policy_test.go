package config_test

import (
	"maps"
	"reflect"
	"testing"

	"github.com/google/cel-go/cel"
)

// admission is one pod admission request for a rendered policy.
type admission struct {
	op, user, namespaceRole string
	old, new                map[string]any // labels; old is nil for a CREATE
	oldSpec, newSpec        string
	oldNote, newNote        string         // an annotation value, none when empty
	owner                   string         // the controller ReplicaSet of a created pod
	notController           bool           // the owner reference is not the controller
	kind                    string         // the request kind, Pod when empty
	obj, oldObj             map[string]any // a whole object, in place of the pod built from the fields above
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
	pod := func(labels map[string]any, spec, note, owner string, notController bool) any {
		meta := map[string]any{"name": "p"}
		if labels != nil {
			meta["labels"] = labels
		}
		if note != "" {
			meta["annotations"] = map[string]any{"note": note}
		}
		if owner != "" {
			meta["ownerReferences"] = []any{map[string]any{"kind": "ReplicaSet", "name": owner, "controller": !notController}}
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
		kind := a.kind
		if kind == "" {
			kind = "Pod"
		}
		in := map[string]any{
			"object":          pod(a.new, a.newSpec, a.newNote, a.owner, a.notController),
			"oldObject":       nil,
			"request":         map[string]any{"operation": a.op, "kind": map[string]any{"kind": kind}, "userInfo": map[string]any{"username": a.user}},
			"namespaceObject": map[string]any{"metadata": map[string]any{"labels": map[string]any{"runkora.io/role": a.namespaceRole}}},
			"variables":       map[string]any{},
		}
		if a.op == "UPDATE" {
			in["oldObject"] = pod(a.old, a.oldSpec, a.oldNote, "", false)
		}
		if a.obj != nil {
			in["object"], in["oldObject"] = a.obj, nil
			if a.oldObj != nil {
				in["oldObject"] = a.oldObj
			}
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
	bound := map[string]any{"spiffe.io/agent": "reviewer", "app": "reviewer", "runkora.io/managed-agent": "true"}
	with := func(base map[string]any, k, v string) map[string]any {
		out := maps.Clone(base)
		out[k] = v
		return out
	}
	without := func(base map[string]any, k string) map[string]any {
		out := maps.Clone(base)
		delete(out, k)
		return out
	}
	const poolRS = "session-manager-pool-7c9d"
	newPool := with(pool, "pod-template-hash", "7c9d")
	tests := []struct {
		name string
		a    admission
		want bool
	}{
		{"bind by kora-agents", admission{op: "UPDATE", user: koraAgents, old: pool, new: bound}, true},
		{"bind without app", admission{op: "UPDATE", user: koraAgents, old: pool, new: without(bound, "app")}, false},
		{"bind with another app", admission{op: "UPDATE", user: koraAgents, old: pool, new: with(bound, "app", "other")}, false},
		{"bind by another account", admission{op: "UPDATE", user: "system:serviceaccount:kora-tenant-a:default", old: pool, new: bound}, false},
		{"bind to an empty agent", admission{op: "UPDATE", user: koraAgents, old: pool, new: map[string]any{"spiffe.io/agent": "", "app": ""}}, false},
		{"empty agent label on a pool pod", admission{op: "UPDATE", user: "anyone", old: pool, new: with(pool, "spiffe.io/agent", "")}, false},
		{"bind that keeps the pool label", admission{op: "UPDATE", user: koraAgents, old: pool, new: with(bound, "runkora.io/pool", "true")}, false},
		{"agent label on a pod outside the pool", admission{op: "UPDATE", user: koraAgents, old: map[string]any{"app": "reviewer"}, new: map[string]any{"app": "reviewer", "spiffe.io/agent": "reviewer"}}, false},
		{"move to another agent", admission{op: "UPDATE", user: koraAgents, old: bound, new: with(with(bound, "spiffe.io/agent", "other"), "app", "other")}, false},
		{"remove the agent label", admission{op: "UPDATE", user: koraAgents, old: bound, new: without(bound, "spiffe.io/agent")}, false},
		{"change app on a bound pod", admission{op: "UPDATE", user: "anyone", old: bound, new: with(bound, "app", "other")}, false},
		{"remove managed-agent from a bound pod", admission{op: "UPDATE", user: "anyone", old: bound, new: without(bound, "runkora.io/managed-agent")}, false},
		{"sandbox labels on a bound pod", admission{op: "UPDATE", user: koraAgents, old: bound, new: with(with(with(bound, "runkora.io/role", "sandbox"), "runkora.io/agent-slug", "other"), "runkora.io/run-id", "r1")}, false},
		{"service label on a bound pod", admission{op: "UPDATE", user: koraAgents, old: bound, new: with(bound, "spiffe.io/service", "svc")}, false},
		{"change a sandbox run id", admission{op: "UPDATE", user: koraAgents, old: map[string]any{"runkora.io/role": "sandbox", "runkora.io/run-id": "r1"}, new: map[string]any{"runkora.io/role": "sandbox", "runkora.io/run-id": "r2"}}, false},
		{"pool label added to an existing pod", admission{op: "UPDATE", user: koraAgents, old: map[string]any{"app": "x"}, new: map[string]any{"app": "x", "runkora.io/pool": "true"}}, false},
		{"other label change on a bound pod", admission{op: "UPDATE", user: "anyone", old: bound, new: with(bound, "x", "y")}, true},
		{"pod with no labels", admission{op: "UPDATE", user: "anyone"}, true},
		{"pool pod leaves the pool unbound", admission{op: "UPDATE", user: "anyone", old: pool, new: without(pool, "runkora.io/pool")}, true},
		{"pool pod created by the ReplicaSet controller", admission{op: "CREATE", user: rsCtrl, new: newPool, newSpec: "session-manager-pool", owner: poolRS}, true},
		{"pool pod from another ReplicaSet", admission{op: "CREATE", user: rsCtrl, new: newPool, newSpec: "session-manager-pool", owner: "other-7c9d"}, false},
		{"pool pod from a look-alike ReplicaSet name", admission{op: "CREATE", user: rsCtrl, new: newPool, newSpec: "session-manager-pool", owner: "session-manager-pool-evil"}, false},
		{"pool pod whose owner is not its controller", admission{op: "CREATE", user: rsCtrl, new: newPool, newSpec: "session-manager-pool", owner: poolRS, notController: true}, false},
		{"pool pod with another ServiceAccount", admission{op: "CREATE", user: rsCtrl, new: newPool, newSpec: "default", owner: poolRS}, false},
		{"pool pod with no owner", admission{op: "CREATE", user: rsCtrl, new: newPool, newSpec: "session-manager-pool"}, false},
		{"pool pod created with an agent label", admission{op: "CREATE", user: rsCtrl, new: with(newPool, "spiffe.io/agent", "reviewer"), newSpec: "session-manager-pool", owner: poolRS}, false},
		{"pool pod created by another account", admission{op: "CREATE", user: koraAgents, new: newPool, newSpec: "session-manager-pool", owner: poolRS}, false},
		{"agent pod created by the StatefulSet controller", admission{op: "CREATE", user: "system:serviceaccount:kube-system:statefulset-controller", new: bound}, false},
		{"agent pod created by kora-agents", admission{op: "CREATE", user: koraAgents, new: bound}, false},
		{"pod created with an empty agent label", admission{op: "CREATE", user: "anyone", new: map[string]any{"spiffe.io/agent": ""}}, false},
		{"pod created with no agent label", admission{op: "CREATE", user: "anyone", new: map[string]any{"app": "mcp"}}, true},
		{"change the agent type on a bound pod", admission{op: "UPDATE", user: "anyone", old: with(bound, "runkora.io/agent-type", "a1"), new: with(bound, "runkora.io/agent-type", "a2")}, false},
		{"remove the agent type from a bound pod", admission{op: "UPDATE", user: "anyone", old: with(bound, "runkora.io/agent-type", "a1"), new: bound}, false},
		{"add the agent type to a bound pod", admission{op: "UPDATE", user: "anyone", old: bound, new: with(bound, "runkora.io/agent-type", "a1")}, false},
		{"bind that sets the agent type", admission{op: "UPDATE", user: koraAgents, old: pool, new: with(bound, "runkora.io/agent-type", "a1")}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.a); got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

// The bind policy matches pods and their subresources in tenant namespaces,
// on CREATE and UPDATE.
func TestChartBindPolicyMatch(t *testing.T) {
	for _, doc := range renderDocs(t, "templates/agents/bind-policy.yaml") {
		if doc["kind"] != "ValidatingAdmissionPolicy" || field(doc, "metadata", "name") != "kora-agent-bind" {
			continue
		}
		if role := field(doc, "spec", "matchConstraints", "namespaceSelector", "matchLabels", "runkora.io/role"); role != "tenant" {
			t.Errorf("namespace selector role = %v, want tenant", role)
		}
		rule := field(doc, "spec", "matchConstraints", "resourceRules").([]any)[0]
		if got := field(rule, "resources"); !reflect.DeepEqual(got, []any{"pods", "pods/*"}) {
			t.Errorf("resources = %v, want pods and pods/*", got)
		}
		if got := field(rule, "operations"); !reflect.DeepEqual(got, []any{"CREATE", "UPDATE"}) {
			t.Errorf("operations = %v, want CREATE and UPDATE", got)
		}
		return
	}
	t.Fatal("no kora-agent-bind ValidatingAdmissionPolicy")
}

func TestChartKoraAgentsPodUpdatePolicy(t *testing.T) {
	allowed := admissionPolicy(t, "kora-agents-pod-updates")
	pool := map[string]any{"runkora.io/pool": "true", "runkora.io/managed-agent": "true", "pod-template-hash": "7c9d"}
	bound := map[string]any{"spiffe.io/agent": "reviewer", "app": "reviewer", "runkora.io/agent-type": "a1", "runkora.io/managed-agent": "true", "pod-template-hash": "7c9d"}
	tests := []struct {
		name string
		a    admission
		want bool
	}{
		{"bind in a tenant namespace", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: pool, new: bound, oldSpec: "sa", newSpec: "sa"}, true},
		{"bind with the agent config stamp", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: pool, new: maps.Clone(map[string]any{"spiffe.io/agent": "reviewer", "app": "reviewer", "runkora.io/agent-type": "a1", "runkora.io/agent-config": "c1", "runkora.io/managed-agent": "true", "pod-template-hash": "7c9d"}), oldSpec: "sa", newSpec: "sa"}, true},
		{"another label", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: pool, new: map[string]any{"runkora.io/pool": "true", "runkora.io/managed-agent": "true", "pod-template-hash": "7c9d", "x": "y"}, oldSpec: "sa", newSpec: "sa"}, false},
		{"remove managed-agent", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: pool, new: map[string]any{"runkora.io/pool": "true", "pod-template-hash": "7c9d"}, oldSpec: "sa", newSpec: "sa"}, false},
		{"annotation change", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: pool, new: pool, oldSpec: "sa", newSpec: "sa", newNote: "x"}, false},
		{"pod outside the pool", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: bound, new: bound, oldSpec: "sa", newSpec: "sa", oldNote: "a", newNote: "a"}, false},
		{"spec change", admission{op: "UPDATE", user: koraAgents, namespaceRole: "tenant", old: pool, new: pool, oldSpec: "sa", newSpec: "other"}, false},
		{"update outside tenant namespaces", admission{op: "UPDATE", user: koraAgents, namespaceRole: "", old: pool, new: bound, oldSpec: "sa", newSpec: "sa"}, false},
		{"another account", admission{op: "UPDATE", user: "anyone", namespaceRole: "", old: bound, new: bound, oldSpec: "sa", newSpec: "other"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.a); got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChartPoolWorkloadPolicy(t *testing.T) {
	allowed := admissionPolicy(t, "kora-agent-pool-workloads")
	const deployCtrl = "system:serviceaccount:kube-system:deployment-controller"
	workload := func(name string, poolTemplate bool, owner string) map[string]any {
		labels := map[string]any{"app": "x"}
		if poolTemplate {
			labels["runkora.io/pool"] = "true"
		}
		meta := map[string]any{"name": name}
		if owner != "" {
			meta["ownerReferences"] = []any{map[string]any{"kind": "Deployment", "name": owner, "controller": true}}
		}
		return map[string]any{"metadata": meta, "spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"labels": labels}}}}
	}
	tests := []struct {
		name string
		a    admission
		want bool
	}{
		{"pool Deployment by kora-agents", admission{op: "CREATE", kind: "Deployment", user: koraAgents, obj: workload("session-manager-pool", true, "")}, true},
		{"pool Deployment by an admin", admission{op: "CREATE", kind: "Deployment", user: "admin", obj: workload("session-manager-pool", true, "")}, false},
		{"pool template in another Deployment", admission{op: "CREATE", kind: "Deployment", user: koraAgents, obj: workload("mcp", true, "")}, false},
		{"look-alike Deployment name", admission{op: "CREATE", kind: "Deployment", user: "admin", obj: workload("session-manager-pool-x", false, "")}, false},
		{"other Deployment", admission{op: "CREATE", kind: "Deployment", user: "admin", obj: workload("mcp", false, "")}, true},
		{"pool ReplicaSet by the Deployment controller", admission{op: "CREATE", kind: "ReplicaSet", user: deployCtrl, obj: workload("session-manager-pool-7c9d", true, "session-manager-pool")}, true},
		{"pool ReplicaSet for another Deployment", admission{op: "CREATE", kind: "ReplicaSet", user: deployCtrl, obj: workload("session-manager-pool-7c9d", true, "mcp")}, false},
		{"pool ReplicaSet by an admin", admission{op: "CREATE", kind: "ReplicaSet", user: "admin", obj: workload("session-manager-pool-7c9d", true, "session-manager-pool")}, false},
		{"replica change on a pool ReplicaSet", admission{op: "UPDATE", kind: "ReplicaSet", user: "admin", obj: workload("session-manager-pool-7c9d", true, "session-manager-pool"), oldObj: workload("session-manager-pool-7c9d", true, "session-manager-pool")}, true},
		{"template change on the pool Deployment by an admin", admission{op: "UPDATE", kind: "Deployment", user: "admin", obj: workload("session-manager-pool", false, ""), oldObj: workload("session-manager-pool", true, "")}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.a); got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChartKoraAgentsDeploymentPolicy(t *testing.T) {
	allowed := admissionPolicy(t, "kora-agents-deployments")
	dep := func(name string) map[string]any { return map[string]any{"metadata": map[string]any{"name": name}} }
	tests := []struct {
		name string
		a    admission
		want bool
	}{
		{"pool in a tenant namespace", admission{op: "CREATE", kind: "Deployment", user: koraAgents, namespaceRole: "tenant", obj: dep("session-manager-pool")}, true},
		{"pool outside tenant namespaces", admission{op: "CREATE", kind: "Deployment", user: koraAgents, obj: dep("session-manager-pool")}, false},
		{"another Deployment", admission{op: "CREATE", kind: "Deployment", user: koraAgents, namespaceRole: "tenant", obj: dep("mcp")}, false},
		{"another account", admission{op: "CREATE", kind: "Deployment", user: "admin", obj: dep("mcp")}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.a); got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChartKoraAgentsDeletePolicy(t *testing.T) {
	allowed := admissionPolicy(t, "kora-agents-deletes")
	obj := map[string]any{"metadata": map[string]any{"name": "coder"}}
	tests := []struct {
		name string
		a    admission
		want bool
	}{
		{"StatefulSet in a tenant namespace", admission{op: "DELETE", kind: "StatefulSet", user: koraAgents, namespaceRole: "tenant", obj: obj}, true},
		{"StatefulSet in kora-system", admission{op: "DELETE", kind: "StatefulSet", user: koraAgents, obj: obj}, false},
		{"claim outside tenant namespaces", admission{op: "DELETE", kind: "PersistentVolumeClaim", user: koraAgents, namespaceRole: "system", obj: obj}, false},
		{"another account", admission{op: "DELETE", kind: "StatefulSet", user: "admin", obj: obj}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.a); got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

// The delete policy matches DELETE on the four kinds of object that kora-agents deletes for a retired agent StatefulSet.
func TestChartKoraAgentsDeletePolicyMatch(t *testing.T) {
	for _, doc := range renderDocs(t, "templates/agents/bind-policy.yaml") {
		if doc["kind"] != "ValidatingAdmissionPolicy" || field(doc, "metadata", "name") != "kora-agents-deletes" {
			continue
		}
		var got []string
		for _, rule := range field(doc, "spec", "matchConstraints", "resourceRules").([]any) {
			if ops := field(rule, "operations"); !reflect.DeepEqual(ops, []any{"DELETE"}) {
				t.Errorf("operations = %v, want DELETE", ops)
			}
			for _, r := range field(rule, "resources").([]any) {
				got = append(got, r.(string))
			}
		}
		want := []string{"statefulsets", "horizontalpodautoscalers", "poddisruptionbudgets", "persistentvolumeclaims"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("resources = %v, want %v", got, want)
		}
		return
	}
	t.Fatal("no kora-agents-deletes ValidatingAdmissionPolicy")
}
