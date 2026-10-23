package config_test

import (
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// kora-agents gets, creates and deletes run pods, and lists a StatefulSet's
// pods to find the Ready ones before a run starts (internal/agents/k8s.go).
func TestChartAgentsClusterRoleGrantsPodVerbs(t *testing.T) {
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
		if slices.Contains(r.APIGroups, "") && slices.Contains(r.Resources, "pods") {
			verbs = append(verbs, r.Verbs...)
		}
	}
	for _, want := range []string{"get", "list", "create", "delete"} {
		if !slices.Contains(verbs, want) {
			t.Errorf("kora-agents ClusterRole pod verbs = %v, missing %q", verbs, want)
		}
	}
}
