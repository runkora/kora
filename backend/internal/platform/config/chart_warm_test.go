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

// renderWakeReserve renders the wake-reserve DaemonSet and PriorityClass over values.
func renderWakeReserve(t *testing.T, values string) (appsv1.DaemonSet, schedulingv1.PriorityClass) {
	t.Helper()
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
	return ds, pc
}

// The wake reserve copies only the session-manager's node affinity, and with no placement set it has none.
func TestChartWakeReservePlacement(t *testing.T) {
	values := "agents:\n  workloads:\n    sessionManager:\n      affinity:\n" +
		"        nodeAffinity: {requiredDuringSchedulingIgnoredDuringExecution: {nodeSelectorTerms: [{matchExpressions: [{key: tier, operator: In, values: [kora]}]}]}}\n" +
		"        podAntiAffinity: {preferredDuringSchedulingIgnoredDuringExecution: [{weight: 1, podAffinityTerm: {topologyKey: kubernetes.io/hostname, labelSelector: {matchLabels: {app: coder}}}}]}\n"
	ds, _ := renderWakeReserve(t, values)
	a := ds.Spec.Template.Spec.Affinity
	if a == nil || a.NodeAffinity == nil || a.PodAffinity != nil || a.PodAntiAffinity != nil {
		t.Errorf("reserve affinity = %+v, want the node affinity only", a)
	}
}

// The wake reserve does not run with the warm tier off, with no session-manager pinning, or when disabled.
func TestChartWakeReserveOff(t *testing.T) {
	pinned := "agents:\n  workloads:\n    sessionManager:\n      nodeSelector: {workload-tier: kora}\n"
	for name, values := range map[string]string{
		"no pinning":    "{}\n",
		"warm tier off": pinned + "  scheduling: {warmAfterSecs: 0}\n",
		"disabled":      pinned + "  wakeReserve: {enabled: false}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if out, err := helmTemplate(t, values, "--show-only", "templates/agents/wake-reserve.yaml"); err == nil {
				t.Errorf("reserve rendered:\n%s", out)
			}
		})
	}
}

// The wake reserve runs on the session-manager nodes at priority 0 with no preemption and holds one grow by default.
func TestChartWakeReserve(t *testing.T) {
	values := "agents:\n  workloads:\n    sessionManager:\n      nodeSelector: {workload-tier: kora}\n      tolerations: [{key: workload-tier, operator: Equal, value: kora, effect: NoSchedule}]\n"
	ds, pc := renderWakeReserve(t, values)
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
	c := spec.Containers[0]
	if !strings.Contains(c.Image, "kora-session-manager") {
		t.Errorf("reserve image = %q, want the session-manager image", c.Image)
	}
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken ||
		spec.SecurityContext == nil || spec.SecurityContext.RunAsNonRoot == nil || !*spec.SecurityContext.RunAsNonRoot ||
		c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation ||
		c.SecurityContext.Capabilities == nil || !slices.Contains(c.SecurityContext.Capabilities.Drop, "ALL") {
		t.Errorf("reserve security: token %v, pod %+v, container %+v; want no token, non-root, no escalation and ALL dropped",
			spec.AutomountServiceAccountToken, spec.SecurityContext, c.SecurityContext)
	}
	res := spec.Containers[0].Resources
	for name, want := range map[corev1.ResourceName]string{corev1.ResourceCPU: "225m", corev1.ResourceMemory: "896Mi"} {
		got, limit := res.Requests[name], res.Limits[name]
		if got.Cmp(resource.MustParse(want)) != 0 || limit.Cmp(got) != 0 {
			t.Errorf("reserve %s request %s limit %s, want %s for both", name, got.String(), limit.String(), want)
		}
	}
}

// Run start and the scaler resize agent pods in place.
func TestChartAgentsClusterRoleResizesPods(t *testing.T) {
	if got := agentsClusterRoleVerbs(t, "", "pods/resize"); !slices.Equal(got, []string{"patch"}) {
		t.Errorf("pods/resize verbs = %v, want [patch]", got)
	}
}
