package config_test

import (
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	agentsConfig "github.com/runkora/kora/backend/cmd/kora-agents/config"
	"github.com/runkora/kora/backend/internal/agents"
)

// The warm tier's threshold and warm requests reach kora-agents, and a negative threshold fails the render
// (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 3).
func TestChartWarmTier(t *testing.T) {
	cfg := loadRendered[agentsConfig.Config](t, renderChart(t, "{}\n"), "cmd/kora-agents")
	if got := cfg.Agents.Scheduling.WarmAfterSecs; got != 300 {
		t.Errorf("warmAfterSecs = %d, want 300", got)
	}
	out := renderChart(t, "agents:\n  workloads:\n    sessionManager:\n      warm: {cpu: 10m, memory: 64Mi}\n", "--show-only", "templates/agents/workloads-configmap.yaml")
	var cm corev1.ConfigMap
	if err := yaml.Unmarshal([]byte(out), &cm); err != nil {
		t.Fatal(err)
	}
	var file struct {
		Workloads agents.WorkloadsConfig `json:"workloads"`
	}
	if err := yaml.Unmarshal([]byte(cm.Data["workloads.yaml"]), &file); err != nil {
		t.Fatal(err)
	}
	if err := file.Workloads.Validate(); err != nil {
		t.Fatal(err)
	}
	if warm := file.Workloads.ForSessionManager().Warm; warm.CPU.String() != "10m" || warm.Memory.String() != "64Mi" {
		t.Errorf("warm requests = %v and %v, want 10m and 64Mi", warm.CPU, warm.Memory)
	}
	if out, err := helmTemplate(t, "agents:\n  scheduling: {warmAfterSecs: -1}\n"); err == nil || !strings.Contains(out, "warmAfterSecs must be at least 0") {
		t.Errorf("warmAfterSecs -1: render err = %v, want the check to fail it", err)
	}
}

// The wake reserve runs on the session-manager nodes at priority 0 with no preemption, holds one grow by default,
// and is off when disabled.
func TestChartWakeReserve(t *testing.T) {
	values := "agents:\n  workloads:\n    sessionManager:\n      nodeSelector: {workload-tier: kora}\n      tolerations: [{key: workload-tier, operator: Equal, value: kora, effect: NoSchedule}]\n"
	out := renderChart(t, values, "--show-only", "templates/agents/wake-reserve.yaml")
	var ds appsv1.DaemonSet
	var pc schedulingv1.PriorityClass
	for doc := range strings.SplitSeq(out, "\n---\n") {
		switch {
		case strings.Contains(doc, "kind: DaemonSet"):
			if err := yaml.Unmarshal([]byte(doc), &ds); err != nil {
				t.Fatal(err)
			}
		case strings.Contains(doc, "kind: PriorityClass"):
			if err := yaml.Unmarshal([]byte(doc), &pc); err != nil {
				t.Fatal(err)
			}
		}
	}
	if pc.Value != 0 || pc.PreemptionPolicy == nil || *pc.PreemptionPolicy != corev1.PreemptNever {
		t.Errorf("PriorityClass value %d, preemption %v; want 0 and Never", pc.Value, pc.PreemptionPolicy)
	}
	spec := ds.Spec.Template.Spec
	if spec.PriorityClassName != pc.Name || ds.Spec.Template.Labels["app"] != "kora-agent-wake-reserve" {
		t.Errorf("reserve priority class %q and app label %q", spec.PriorityClassName, ds.Spec.Template.Labels["app"])
	}
	if spec.NodeSelector["workload-tier"] != "kora" || len(spec.Tolerations) != 1 {
		t.Errorf("reserve placement = %v and %v, want the session-manager's", spec.NodeSelector, spec.Tolerations)
	}
	if len(spec.Containers) != 1 {
		t.Fatalf("reserve containers = %d, want 1", len(spec.Containers))
	}
	res := spec.Containers[0].Resources
	for name, want := range map[corev1.ResourceName]string{corev1.ResourceCPU: "225m", corev1.ResourceMemory: "896Mi"} {
		got, limit := res.Requests[name], res.Limits[name]
		if got.Cmp(resource.MustParse(want)) != 0 || limit.Cmp(got) != 0 {
			t.Errorf("reserve %s request %s limit %s, want %s for both", name, got.String(), limit.String(), want)
		}
	}
	if out, err := helmTemplate(t, "agents:\n  wakeReserve: {enabled: false}\n", "--show-only", "templates/agents/wake-reserve.yaml"); err == nil {
		t.Errorf("disabled reserve rendered:\n%s", out)
	}
}

// Run start and the scaler resize agent pods in place.
func TestChartAgentsClusterRoleResizesPods(t *testing.T) {
	if got := agentsClusterRoleVerbs(t, "", "pods/resize"); !slices.Equal(got, []string{"patch"}) {
		t.Errorf("pods/resize verbs = %v, want [patch]", got)
	}
}
