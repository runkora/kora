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

// kora-agents creates sandbox pods, watches the bound and pool pods into its
// caches, patches a pool pod's labels to bind it and deletes a bound pod to
// drain it (docs/adr/2026-11-22-agent-cold-tier.md).
func TestChartAgentsClusterRoleGrantsPodVerbs(t *testing.T) {
	verbs := agentsClusterRoleVerbs(t, "", "pods")
	for _, want := range []string{"get", "list", "watch", "create", "delete", "patch"} {
		if !slices.Contains(verbs, want) {
			t.Errorf("kora-agents ClusterRole pod verbs = %v, missing %q", verbs, want)
		}
	}
}

// kora-agents only deletes what an earlier release created for each agent: its StatefulSet, HPA and
// PodDisruptionBudget (docs/adr/2026-11-22-agent-cold-tier.md).
func TestChartAgentsClusterRoleRetiresStatefulSets(t *testing.T) {
	for _, r := range []struct{ group, resource string }{
		{"apps", "statefulsets"}, {"autoscaling", "horizontalpodautoscalers"}, {"policy", "poddisruptionbudgets"},
	} {
		if got := agentsClusterRoleVerbs(t, r.group, r.resource); !slices.Equal(got, []string{"delete"}) {
			t.Errorf("%s verbs = %v, want [delete]", r.resource, got)
		}
	}
	if got := agentsClusterRoleVerbs(t, "apps", "statefulsets/scale"); len(got) != 0 {
		t.Errorf("statefulsets/scale verbs = %v, want none", got)
	}
}

// The scaler checks at least one agent at a time.
func TestChartScalerConcurrencyBound(t *testing.T) {
	out, err := helmTemplate(t, "agents:\n  scheduling: {scalerConcurrency: 0}\n")
	if err == nil || !strings.Contains(out, "scalerConcurrency") {
		t.Errorf("scalerConcurrency 0: render err = %v, want the bound check to fail it", err)
	}
}

// minReplicas needs at least one pod and at most maxReplicas.
func TestChartMinReplicasBounds(t *testing.T) {
	for _, v := range []string{"{minReplicas: 0}", "{minReplicas: -1}", "{minReplicas: 11}", "{minReplicas: 3, maxReplicas: 2}"} {
		out, err := helmTemplate(t, "agents:\n  scheduling: "+v+"\n")
		if err == nil || !strings.Contains(out, "minReplicas must be at least 1 and at most maxReplicas") {
			t.Errorf("%s: render err = %v, want the bounds check to fail it", v, err)
		}
	}
	for _, v := range []string{"{minReplicas: 10}", "{minReplicas: 12, maxReplicas: 20}"} {
		if _, err := helmTemplate(t, "agents:\n  scheduling: "+v+"\n"); err != nil {
			t.Errorf("%s: render err = %v, want it accepted", v, err)
		}
	}
}

// kora-agents reconciles each tenant's pool Deployment, watches it for its current template, and reads a
// pool pod's ReplicaSet before the bind (docs/adr/2026-11-22-agent-cold-tier.md).
func TestChartAgentsClusterRolePoolDeployments(t *testing.T) {
	if got := agentsClusterRoleVerbs(t, "apps", "deployments"); !slices.Equal(got, []string{"create", "get", "list", "update", "watch"}) {
		t.Errorf("deployments verbs = %v, want [create get list update watch]", got)
	}
	if got := agentsClusterRoleVerbs(t, "apps", "replicasets"); !slices.Equal(got, []string{"get"}) {
		t.Errorf("replicasets verbs = %v, want [get]", got)
	}
}

func TestChartPoolSizeBounds(t *testing.T) {
	if out, err := helmTemplate(t, "agents:\n  pool: {size: -1}\n"); err == nil || !strings.Contains(out, "agents.pool.size must be at least 0") {
		t.Errorf("size -1: render err = %v, want the bounds check to fail it", err)
	}
	if _, err := helmTemplate(t, "agents:\n  pool: {size: 0}\n"); err != nil {
		t.Errorf("size 0: render err = %v, want it accepted", err)
	}
}
