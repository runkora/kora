package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	agentsConfig "github.com/clear-route/orbit/cmd/orbit-agents/config"
	"github.com/clear-route/orbit/internal/platform/config"
)

// agentsSandboxMount loads orbit-agents' sandboxMount setting from the rendered chart.
func agentsSandboxMount(t *testing.T, rendered string) bool {
	t.Helper()
	for _, root := range writeMountedConfig(t, rendered) {
		if _, err := os.Stat(filepath.Join(root, "cmd", "orbit-agents.yaml")); err != nil {
			continue
		}
		config.SetOverlayRoot(root)
		cfg, err := config.LoadService[agentsConfig.Config]("cmd/orbit-agents", "production")
		config.SetOverlayRoot("/etc/orbit/config")
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Agents.SandboxMount
	}
	t.Fatal("no workload mounts the orbit-agents config")
	return false
}

// Enabled, the chart runs the node agent privileged in the host network in a privileged
// namespace, carries the sandbox and session-manager tolerations, and turns the feature on in orbit-agents.
func TestChartSandboxMountEnabled(t *testing.T) {
	out := renderChart(t, `sandboxMount:
  enabled: true
agents:
  workloads:
    sandbox:
      tolerations: [{key: sandbox, operator: Exists}]
    sessionManager:
      tolerations: [{key: agents, operator: Exists}]
`)
	if !agentsSandboxMount(t, out) {
		t.Error("orbit-agents sandboxMount = false, want true")
	}
	var ds appsv1.DaemonSet
	var ns corev1.Namespace
	for doc := range strings.SplitSeq(out, "\n---\n") {
		var meta struct{ Kind string }
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			t.Fatal(err)
		}
		switch {
		case meta.Kind == "DaemonSet" && strings.Contains(doc, "name: orbit-sandbox-mount"):
			if err := yaml.Unmarshal([]byte(doc), &ds); err != nil {
				t.Fatal(err)
			}
		case meta.Kind == "Namespace" && strings.Contains(doc, "name: orbit-node"):
			if err := yaml.Unmarshal([]byte(doc), &ns); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ns.Labels["pod-security.kubernetes.io/enforce"] != "privileged" {
		t.Errorf("namespace labels = %v, want enforce=privileged", ns.Labels)
	}
	spec := ds.Spec.Template.Spec
	if ds.Namespace != "orbit-node" || !spec.HostNetwork || len(spec.Containers) != 1 {
		t.Fatalf("DaemonSet = %+v", ds.ObjectMeta)
	}
	c := spec.Containers[0]
	if c.SecurityContext == nil || c.SecurityContext.Privileged == nil || !*c.SecurityContext.Privileged {
		t.Error("node agent is not privileged")
	}
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].MountPath != "/var/lib/kubelet" || *c.VolumeMounts[0].MountPropagation != corev1.MountPropagationBidirectional {
		t.Errorf("volume mounts = %+v", c.VolumeMounts)
	}
	var keys []string
	for _, tol := range spec.Tolerations {
		keys = append(keys, tol.Key)
	}
	if strings.Join(keys, ",") != "sandbox,agents" {
		t.Errorf("tolerations = %v, want sandbox and agents", keys)
	}
}

// Disabled by default: no node agent, and orbit-agents leaves sandboxes as they were.
func TestChartSandboxMountDisabledByDefault(t *testing.T) {
	out := renderChart(t, "{}\n")
	if strings.Contains(out, "orbit-sandbox-mount") {
		t.Error("default chart renders the node agent")
	}
	if agentsSandboxMount(t, out) {
		t.Error("orbit-agents sandboxMount = true by default")
	}
}
