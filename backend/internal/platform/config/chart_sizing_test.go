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
	"github.com/runkora/kora/backend/internal/agents"
	"github.com/runkora/kora/backend/internal/platform/config"
)

// renderChart renders the kora chart over the CI values plus values, passing
// extra helm arguments through. Needs helm and the chart's dependencies.
func renderChart(t *testing.T, values string, extra ...string) string {
	t.Helper()
	out, err := helmTemplate(t, values, extra...)
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return out
}

// helmTemplate is renderChart returning the render error instead of failing the test.
func helmTemplate(t *testing.T, values string, extra ...string) (string, error) {
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
	return string(out), err
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

// The rendered agents.workloads reaches the session-manager container as equal
// memory request and limit: 1Gi by default, or the configured value.
func TestChartSessionManagerMemory(t *testing.T) {
	for name, tc := range map[string]struct{ values, want string }{
		"default":    {"{}\n", "1Gi"},
		"configured": {"agents:\n  workloads:\n    sessionManager:\n      memory: 2Gi\n", "2Gi"},
	} {
		t.Run(name, func(t *testing.T) {
			out := renderChart(t, tc.values, "--show-only", "templates/agents/workloads-configmap.yaml")
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
			// Checked before the Go fallback, which is also 1Gi, can fill it in.
			if file.Workloads.SessionManager.Memory == nil {
				t.Fatal("chart rendered no agents.workloads.sessionManager.memory")
			}
			objs, err := agents.RenderResources(agents.K8sProvisionerConfig{Workloads: file.Workloads}, agents.AgentType{ID: "a", Slug: "coder", TenantID: "t"}, "tenant-t", false)
			if err != nil {
				t.Fatal(err)
			}
			var resources corev1.ResourceRequirements
			for _, obj := range objs {
				if ss, ok := obj.(*appsv1.StatefulSet); ok {
					resources = ss.Spec.Template.Spec.Containers[0].Resources
				}
			}
			want := resource.MustParse(tc.want)
			if got := resources.Requests[corev1.ResourceMemory]; got.Cmp(want) != 0 {
				t.Errorf("memory request = %s, want %s", got.String(), tc.want)
			}
			if got := resources.Limits[corev1.ResourceMemory]; got.Cmp(want) != 0 {
				t.Errorf("memory limit = %s, want %s", got.String(), tc.want)
			}
		})
	}
}

// The chart renders the memory thresholds kora-agents decodes, and rejects a
// placement watermark at or above the pressure limit.
func TestChartMemoryThresholds(t *testing.T) {
	out := renderChart(t, "agents:\n  scheduling:\n    placementWatermark: 0.6\n    pressureLimit: 0.85\n    startingRunWindowSecs: 45\n    startingRunAllowance: {claudeCode: 200Mi, codex: 0.1Gi, openCode: 500Mi}\n")
	cfg := loadRendered[agentsConfig.Config](t, out, "cmd/kora-agents")
	if err := agentsConfig.Validate(cfg); err != nil {
		t.Fatal(err)
	}
	s := cfg.Agents.Scheduling
	if s.PlacementWatermark != 0.6 || s.PressureLimit != 0.85 || s.StartingRunWindowSecs != 45 {
		t.Errorf("scheduling = watermark %v, pressure limit %v, starting window %d; want 0.6, 0.85, 45", s.PlacementWatermark, s.PressureLimit, s.StartingRunWindowSecs)
	}
	if out, err := helmTemplate(t, "agents:\n  scheduling: {startingRunWindowSecs: 0}\n"); err == nil || !strings.Contains(out, "startingRunWindowSecs must be at least 1") {
		t.Errorf("startingRunWindowSecs 0: render err = %v, want the check to fail it", err)
	}
	a := s.StartingRunAllowance
	for name, tc := range map[string]struct {
		got  resource.Quantity
		want string
	}{"claudeCode": {a.ClaudeCode, "200Mi"}, "codex": {a.Codex, "0.1Gi"}, "openCode": {a.OpenCode, "500Mi"}} {
		if tc.got.Cmp(resource.MustParse(tc.want)) != 0 {
			t.Errorf("startingRunAllowance.%s = %s, want %s", name, tc.got.String(), tc.want)
		}
	}

	for name, values := range map[string]string{
		"watermark equals the limit": "agents:\n  scheduling: {placementWatermark: 0.9, pressureLimit: 0.9}\n",
		"watermark above the limit":  "agents:\n  scheduling: {placementWatermark: 0.95, pressureLimit: 0.9}\n",
		"limit at 1":                 "agents:\n  scheduling: {pressureLimit: 1}\n",
		"watermark zero":             "agents:\n  scheduling: {placementWatermark: 0}\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := helmTemplate(t, values)
			if err == nil || !strings.Contains(out, "placementWatermark must be above 0 and below pressureLimit") {
				t.Errorf("render err = %v, want the threshold check to fail it:\n%s", err, out)
			}
		})
	}

	for name, tc := range map[string]struct{ values, want string }{
		"harness missing":  {"agents:\n  scheduling:\n    startingRunAllowance: {codex: null}\n", "startingRunAllowance.codex must be a positive memory quantity"},
		"harness zero":     {"agents:\n  scheduling:\n    startingRunAllowance: {openCode: 0Mi}\n", "startingRunAllowance.openCode must be a positive memory quantity"},
		"harness negative": {"agents:\n  scheduling:\n    startingRunAllowance: {claudeCode: -1Mi}\n", "startingRunAllowance.claudeCode must be a positive memory quantity"},
		"scalar":           {"agents:\n  scheduling:\n    startingRunAllowance: 431Mi\n", "startingRunAllowance must be a map"},
		"unknown harness":  {"agents:\n  scheduling:\n    startingRunAllowance: {gemini: 100Mi}\n", "startingRunAllowance.gemini: unknown harness"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := helmTemplate(t, tc.values)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Errorf("render err = %v, want %q:\n%s", err, tc.want, out)
			}
		})
	}
}
