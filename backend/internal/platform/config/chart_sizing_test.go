package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	agentsConfig "github.com/runkora/kora/backend/cmd/kora-agents/config"
	"github.com/runkora/kora/backend/internal/platform/config"
)

// renderChart renders the kora chart over the CI values plus values, passing
// extra helm arguments through. Needs helm and the chart's dependencies.
func renderChart(t *testing.T, values string, extra ...string) string {
	t.Helper()
	chart := filepath.Join("..", "..", "..", "..", "charts", "kora")
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	if deps, _ := filepath.Glob(filepath.Join(chart, "charts", "*.tgz")); len(deps) == 0 {
		t.Skip("chart dependencies not built")
	}
	file := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(file, []byte(values), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"template", "kora", chart, "-n", "kora-system", "-f", filepath.Join(chart, "ci", "default-values.yaml"), "-f", file}, extra...)
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return string(out)
}

// A maxSize field left unset takes the default size's value.
func TestChartMaxSizeFallsBackToDefaultSize(t *testing.T) {
	out := renderChart(t, "agents:\n  environments:\n    defaultSize: {cpu: \"3\", memory: 6Gi, disk: 12Gi}\n    maxSize: {cpu: \"6\"}\n")
	for _, root := range writeMountedConfig(t, out) {
		if _, err := os.Stat(filepath.Join(root, "cmd", "kora-agents.yaml")); err != nil {
			continue
		}
		config.SetOverlayRoot(root)
		cfg, err := config.LoadService[agentsConfig.Config]("cmd/kora-agents", "production")
		config.SetOverlayRoot("/etc/kora/config")
		if err != nil {
			t.Fatal(err)
		}
		want := agentsConfig.EnvironmentSize{CPU: "6", Memory: "6Gi", Disk: "12Gi"}
		if got := cfg.Agents.Environments.MaxSize; got != want {
			t.Errorf("maxSize = %+v, want %+v", got, want)
		}
		return
	}
	t.Fatal("no workload mounts the kora-agents config")
}

// With no resources set, the headroom placeholder reserves one default sandbox
// pod: defaultSize, 1Gi of scratch disk and the pinned sidecars.
func TestChartHeadroomReservesDefaultSandboxPod(t *testing.T) {
	out := renderChart(t, "agents:\n  sandboxWarming:\n    mode: capacity\n", "--show-only", "templates/agents/sandbox-headroom.yaml")
	var deploy appsv1.Deployment
	for doc := range strings.SplitSeq(out, "\n---\n") {
		var d appsv1.Deployment
		if err := yaml.Unmarshal([]byte(doc), &d); err != nil {
			t.Fatal(err)
		}
		if d.Kind == "Deployment" {
			deploy = d
		}
	}
	if len(deploy.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("no headroom Deployment rendered")
	}
	total := corev1.ResourceList{}
	for _, c := range deploy.Spec.Template.Spec.Containers {
		for name, q := range c.Resources.Requests {
			if limit := c.Resources.Limits[name]; limit.Cmp(q) != 0 {
				t.Errorf("%s %s: limit %s, want the request %s", c.Name, name, limit.String(), q.String())
			}
			sum := total[name]
			sum.Add(q)
			total[name] = sum
		}
	}
	want := map[corev1.ResourceName]string{
		corev1.ResourceCPU:              "2900m",
		corev1.ResourceMemory:           "4416Mi",
		corev1.ResourceEphemeralStorage: "11Gi",
	}
	for name, w := range want {
		if got := total[name]; got.Cmp(resource.MustParse(w)) != 0 {
			t.Errorf("total %s request = %s, want %s", name, got.String(), w)
		}
	}
}

// The chart exposes both idle timeouts, with the manual one at today's 20
// minutes, and kora-agents decodes what it renders.
func TestChartIdleTimeouts(t *testing.T) {
	tests := []struct {
		name       string
		values     string
		wantManual int
		wantTrig   int
	}{
		{name: "defaults", values: "", wantManual: 1200, wantTrig: 300},
		{name: "overridden", values: "agents:\n  idleTimeout: {manualSecs: 3600, triggerSecs: 90}\n", wantManual: 3600, wantTrig: 90},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderChart(t, tt.values)
			for _, root := range writeMountedConfig(t, out) {
				if _, err := os.Stat(filepath.Join(root, "cmd", "kora-agents.yaml")); err != nil {
					continue
				}
				config.SetOverlayRoot(root)
				cfg, err := config.LoadService[agentsConfig.Config]("cmd/kora-agents", "production")
				config.SetOverlayRoot("/etc/kora/config")
				if err != nil {
					t.Fatal(err)
				}
				if got := cfg.Agents.IdleTimeout.ManualSecs; got != tt.wantManual {
					t.Errorf("manualSecs = %d, want %d", got, tt.wantManual)
				}
				if got := cfg.Agents.IdleTimeout.TriggerSecs; got != tt.wantTrig {
					t.Errorf("triggerSecs = %d, want %d", got, tt.wantTrig)
				}
				return
			}
			t.Fatal("no rendered kora-agents config found")
		})
	}
}
