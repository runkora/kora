package config_test

import (
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/storage"
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

// kora-agents gets, creates and deletes run pods, lists a StatefulSet's
// pods to find the Ready ones before a run starts (internal/agents/k8s.go),
// and watches them into the scaler's cache.
func TestChartAgentsClusterRoleGrantsPodVerbs(t *testing.T) {
	verbs := agentsClusterRoleVerbs(t, "", "pods")
	for _, want := range []string{"get", "list", "watch", "create", "delete"} {
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

// The scaler's cache lists and watches agent StatefulSets (internal/agents/workload_cache.go).
func TestChartAgentsClusterRoleWatchesStatefulSets(t *testing.T) {
	verbs := agentsClusterRoleVerbs(t, "apps", "statefulsets")
	for _, want := range []string{"list", "watch"} {
		if !slices.Contains(verbs, want) {
			t.Errorf("kora-agents ClusterRole statefulset verbs = %v, missing %q", verbs, want)
		}
	}
}

// The scaler checks at least one agent at a time.
func TestChartScalerConcurrencyBound(t *testing.T) {
	out, err := helmTemplate(t, "agents:\n  scheduling: {scalerConcurrency: 0}\n")
	if err == nil || !strings.Contains(out, "scalerConcurrency") {
		t.Errorf("scalerConcurrency 0: render err = %v, want the bound check to fail it", err)
	}
}

// agents.database.maxConnections sizes the kora-agents pool only. Other workloads keep the embedded pool size.
func TestChartAgentsDatabaseMaxConnections(t *testing.T) {
	out := renderChart(t, "agents:\n  database: {maxConnections: 7}\n")
	pools := map[string]int32{}
	for doc := range strings.SplitSeq(out, "\n---\n") {
		var cm struct {
			Metadata struct{ Name string } `json:"metadata"`
			Data     map[string]string     `json:"data"`
		}
		if err := yaml.Unmarshal([]byte(doc), &cm); err != nil {
			t.Fatal(err)
		}
		rendered, ok := cm.Data["internal.platform.storage.yaml"]
		if !ok {
			continue
		}
		var cfg storage.StorageConfig
		if err := yaml.Unmarshal([]byte(rendered), &cfg); err != nil {
			t.Fatal(err)
		}
		pools[cm.Metadata.Name] = cfg.Postgres.MaxConnections
	}
	if pools["kora-agents-config"] != 7 {
		t.Errorf("kora-agents maxConnections = %d, want 7", pools["kora-agents-config"])
	}
	if _, ok := pools["kora-identity-config"]; !ok {
		t.Fatal("no kora-identity storage config rendered")
	}
	for name, n := range pools {
		if name != "kora-agents-config" && n != 0 {
			t.Errorf("%s renders maxConnections %d, want none", name, n)
		}
	}
	if out, err := helmTemplate(t, "agents:\n  database: {maxConnections: 0}\n"); err == nil || !strings.Contains(out, "maxConnections must be at least 1") {
		t.Errorf("maxConnections 0: render err = %v, want the bound check to fail it", err)
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
