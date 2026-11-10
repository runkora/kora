package config_test

import (
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// agentsClusterRoleVerbs renders the chart and returns the kora-agents ClusterRole's verbs on group/resource.
func agentsClusterRoleVerbs(t *testing.T, group, resource string) []string {
	t.Helper()
	out := renderChart(t, "{}\n")
	var role rbacv1.ClusterRole
	for doc := range strings.SplitSeq(out, "\n---\n") {
		if strings.Contains(doc, "kind: ClusterRole\n") && strings.Contains(doc, "name: kora-agents\n") {
			if err := yaml.Unmarshal([]byte(doc), &role); err != nil {
				t.Fatal(err)
			}
		}
	}
	var verbs []string
	for _, r := range role.Rules {
		if slices.Contains(r.APIGroups, group) && slices.Contains(r.Resources, resource) {
			verbs = append(verbs, r.Verbs...)
		}
	}
	slices.Sort(verbs)
	return verbs
}

// kora-agents gets, creates and deletes run pods, and lists a StatefulSet's
// pods to find the Ready ones before a run starts (internal/agents/k8s.go).
func TestChartAgentsClusterRoleGrantsPodVerbs(t *testing.T) {
	verbs := agentsClusterRoleVerbs(t, "", "pods")
	for _, want := range []string{"get", "list", "create", "delete"} {
		if !slices.Contains(verbs, want) {
			t.Errorf("kora-agents ClusterRole pod verbs = %v, missing %q", verbs, want)
		}
	}
}

// The scaler sets replicas through the scale subresource, and apply only deletes an earlier release's HPA.
func TestChartAgentsClusterRoleScaleAndStaleHPA(t *testing.T) {
	if got := agentsClusterRoleVerbs(t, "apps", "statefulsets/scale"); !slices.Equal(got, []string{"get", "update"}) {
		t.Errorf("statefulsets/scale verbs = %v, want [get update]", got)
	}
	if got := agentsClusterRoleVerbs(t, "autoscaling", "horizontalpodautoscalers"); !slices.Equal(got, []string{"delete", "get"}) {
		t.Errorf("horizontalpodautoscalers verbs = %v, want [delete get]", got)
	}
}

// minReplicas needs at least one pod and has no upper bound now the HPA is gone.
func TestChartMinReplicasBounds(t *testing.T) {
	for _, n := range []string{"0", "-1"} {
		out, err := helmTemplate(t, "agents:\n  scheduling: {minReplicas: "+n+"}\n")
		if err == nil || !strings.Contains(out, "agents.scheduling.minReplicas must be at least 1") {
			t.Errorf("minReplicas %s: render err = %v, want the floor check to fail it", n, err)
		}
	}
	if _, err := helmTemplate(t, "agents:\n  scheduling: {minReplicas: 12}\n"); err != nil {
		t.Errorf("minReplicas 12: render err = %v, want no upper bound", err)
	}
}
