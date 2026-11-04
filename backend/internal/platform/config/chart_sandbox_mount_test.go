package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	agentsConfig "github.com/runkora/kora/backend/cmd/kora-agents/config"
	sandboxMountConfig "github.com/runkora/kora/backend/cmd/sandbox-mount/config"
	"github.com/runkora/kora/backend/internal/platform/config"
)

// loadRendered loads a service's config with the file the rendered chart mounts for it.
func loadRendered[T any](t *testing.T, rendered, module string) *T {
	t.Helper()
	for _, root := range writeMountedConfig(t, rendered) {
		if _, err := os.Stat(filepath.Join(root, module+".yaml")); err != nil {
			continue
		}
		config.SetOverlayRoot(root)
		cfg, err := config.LoadService[T](module, "production")
		config.SetOverlayRoot("/etc/kora/config")
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	t.Fatalf("no workload mounts the %s config", module)
	return nil
}

// Enabled, the chart runs the node agent privileged in the host network in a privileged
// namespace, carries the sandbox and session-manager tolerations, and turns the feature on in kora-agents.
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
	if !loadRendered[agentsConfig.Config](t, out, "cmd/kora-agents").Agents.SandboxMount {
		t.Error("kora-agents sandboxMount = false, want true")
	}
	var ds appsv1.DaemonSet
	var ns corev1.Namespace
	var role rbacv1.ClusterRole
	for doc := range strings.SplitSeq(out, "\n---\n") {
		var meta struct{ Kind string }
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			t.Fatal(err)
		}
		switch {
		case meta.Kind == "DaemonSet" && strings.Contains(doc, "name: kora-sandbox-mount"):
			if err := yaml.Unmarshal([]byte(doc), &ds); err != nil {
				t.Fatal(err)
			}
		case meta.Kind == "ClusterRole" && strings.Contains(doc, "name: kora-sandbox-mount"):
			if err := yaml.Unmarshal([]byte(doc), &role); err != nil {
				t.Fatal(err)
			}
		case meta.Kind == "Namespace" && strings.Contains(doc, "name: kora-sandbox-mount"):
			if err := yaml.Unmarshal([]byte(doc), &ns); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The node agent reads pods and nothing else.
	if len(role.Rules) != 1 || strings.Join(role.Rules[0].Resources, ",") != "pods" || strings.Join(role.Rules[0].Verbs, ",") != "list,watch" || strings.Join(role.Rules[0].APIGroups, ",") != "" {
		t.Errorf("ClusterRole rules = %+v, want list and watch on pods only", role.Rules)
	}
	if ns.Labels["pod-security.kubernetes.io/enforce"] != "privileged" {
		t.Errorf("namespace labels = %v, want enforce=privileged", ns.Labels)
	}
	spec := ds.Spec.Template.Spec
	if ds.Namespace != "kora-sandbox-mount" || !spec.HostNetwork || !spec.HostPID || len(spec.Containers) != 1 {
		t.Fatalf("DaemonSet = %+v", ds.ObjectMeta)
	}
	c := spec.Containers[0]
	if c.SecurityContext == nil || c.SecurityContext.Privileged == nil || !*c.SecurityContext.Privileged {
		t.Error("node agent is not privileged")
	}
	if len(c.VolumeMounts) != 2 || c.VolumeMounts[0].MountPath != "/var/lib/kubelet" || *c.VolumeMounts[0].MountPropagation != corev1.MountPropagationBidirectional {
		t.Errorf("volume mounts = %+v, want the kubelet directory and the config files", c.VolumeMounts)
	}
	var keys []string
	for _, tol := range spec.Tolerations {
		keys = append(keys, tol.Key)
	}
	if strings.Join(keys, ",") != "sandbox,agents" {
		t.Errorf("tolerations = %v, want sandbox and agents", keys)
	}
}

// Disabled by default: no node agent, and kora-agents leaves sandboxes as they were.
func TestChartSandboxMountDisabledByDefault(t *testing.T) {
	out := renderChart(t, "{}\n")
	if strings.Contains(out, "kora-sandbox-mount") {
		t.Error("default chart renders the node agent")
	}
	if loadRendered[agentsConfig.Config](t, out, "cmd/kora-agents").Agents.SandboxMount {
		t.Error("kora-agents sandboxMount = true by default")
	}
}

// The node agent's config carries sandboxMount.port, 20490 by default, and
// sandboxMount.kubeletDir. The chart refuses a port that is not a TCP port.
func TestChartSandboxMountConfig(t *testing.T) {
	for values, want := range map[string]int{
		"sandboxMount:\n  enabled: true\n":                                       20490,
		"sandboxMount:\n  enabled: true\n  port: 31049\n  kubeletDir: /data/k\n": 31049,
	} {
		cfg := loadRendered[sandboxMountConfig.Config](t, renderChart(t, values), "cmd/sandbox-mount")
		wantDir := "/var/lib/kubelet"
		if want != 20490 {
			wantDir = "/data/k"
		}
		if cfg.Server.Port != want || cfg.Node.HostKubeletDir != wantDir || cfg.Node.KubeletDir != "/var/lib/kubelet" {
			t.Errorf("values %q: port %d, host kubelet dir %q, kubelet dir %q", values, cfg.Server.Port, cfg.Node.HostKubeletDir, cfg.Node.KubeletDir)
		}
	}
	// renderChart above skipped the test if helm or the chart dependencies are missing.
	chart := filepath.Join("..", "..", "..", "..", "charts", "kora")
	out, err := exec.Command("helm", "template", "kora", chart, "-n", "kora-system", "-f", filepath.Join(chart, "ci", "default-values.yaml"), "--set", "sandboxMount.enabled=true", "--set", "sandboxMount.port=70000").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "sandboxMount.port must be a TCP port") {
		t.Errorf("port 70000 rendered: %v\n%s", err, out)
	}
}
