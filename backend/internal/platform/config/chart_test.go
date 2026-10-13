package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	egressConfig "github.com/clear-route/orbit/cmd/egress-gateway/config"
	identityConfig "github.com/clear-route/orbit/cmd/identity/config"
	inferenceConfig "github.com/clear-route/orbit/cmd/inference-gateway/config"
	operatorConfig "github.com/clear-route/orbit/cmd/operator/config"
	agentsConfig "github.com/clear-route/orbit/cmd/orbit-agents/config"
	mcpConfig "github.com/clear-route/orbit/cmd/orbit-mcp/config"
	triggersConfig "github.com/clear-route/orbit/cmd/orbit-triggers/config"
	policyGatewayConfig "github.com/clear-route/orbit/cmd/policy-gateway/config"
	policyServiceConfig "github.com/clear-route/orbit/cmd/policy-service/config"
	sandboxMountConfig "github.com/clear-route/orbit/cmd/sandbox-mount/config"
	toolGatewayConfig "github.com/clear-route/orbit/cmd/tool-gateway/config"
	"github.com/clear-route/orbit/internal/identity"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/clear-route/orbit/internal/platform/token"
)

// moduleLoaders loads each config module the chart renders, the way its binary does.
var moduleLoaders = map[string]func() error{
	"cmd/egress-gateway":    loadService[egressConfig.Config]("cmd/egress-gateway"),
	"cmd/identity":          loadService[identityConfig.Config]("cmd/identity"),
	"cmd/inference-gateway": loadService[inferenceConfig.Config]("cmd/inference-gateway"),
	"cmd/operator":          loadService[operatorConfig.Config]("cmd/operator"),
	"cmd/orbit-agents":      loadService[agentsConfig.Config]("cmd/orbit-agents"),
	"cmd/orbit-mcp":         loadService[mcpConfig.Config]("cmd/orbit-mcp"),
	"cmd/orbit-triggers":    loadService[triggersConfig.Config]("cmd/orbit-triggers"),
	"cmd/policy-gateway":    loadService[policyGatewayConfig.Config]("cmd/policy-gateway"),
	"cmd/policy-service":    loadService[policyServiceConfig.Config]("cmd/policy-service"),
	"cmd/sandbox-mount":     loadService[sandboxMountConfig.Config]("cmd/sandbox-mount"),
	"cmd/tool-gateway":      loadService[toolGatewayConfig.Config]("cmd/tool-gateway"),

	"internal/identity": func() error {
		_, err := identity.LoadConfig()
		return err
	},
	"internal/platform/token": loadService[token.Config]("internal/platform/token"),

	"internal/platform/observability": loadService[observability.ObservabilityConfig]("internal/platform/observability"),
	"internal/platform/storage": func() error {
		_, err := config.Load[storage.StorageConfig]("internal/platform/storage", "production")
		return err
	},
}

func loadService[T any](module string) func() error {
	return func() error {
		_, err := config.LoadService[T](module, "production")
		return err
	}
}

// TestChartRenderedConfigDecodes renders the orbit chart and loads every config
// file it mounts through the module's own loader, so a chart key the Go config
// does not declare fails here rather than at pod startup. Needs helm and the
// chart's dependencies (make chart-deps).
func TestChartRenderedConfigDecodes(t *testing.T) {
	chart := filepath.Join("..", "..", "..", "..", "charts", "orbit")
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	if deps, _ := filepath.Glob(filepath.Join(chart, "charts", "*.tgz")); len(deps) == 0 {
		t.Skip("chart dependencies not built")
	}
	// Values a user may write unquoted, such as a whole number of CPUs.
	numeric := filepath.Join(t.TempDir(), "numeric.yaml")
	if err := os.WriteFile(numeric, []byte("agents:\n  environments:\n    defaultSize: {cpu: 1, memory: 4Gi, disk: 10Gi}\n    maxSize: {cpu: 8.5, memory: 16Gi, disk: 50Gi}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	valueSets := map[string][]string{
		"ci defaults":    {filepath.Join(chart, "ci", "default-values.yaml")},
		"numeric values": {filepath.Join(chart, "ci", "default-values.yaml"), numeric},
		"local":          {filepath.Join(chart, "ci", "default-values.yaml"), filepath.Join(chart, "..", "..", "deploy", "reference", "values", "orbit.local.yaml")},
	}
	for name, files := range valueSets {
		t.Run(name, func(t *testing.T) {
			args := []string{"template", "orbit", chart, "-n", "orbit-system"}
			for _, f := range files {
				args = append(args, "-f", f)
			}
			out, err := exec.Command("helm", args...).CombinedOutput()
			if err != nil {
				t.Fatalf("helm template: %v\n%s", err, out)
			}
			for _, root := range writeMountedConfig(t, string(out)) {
				config.SetOverlayRoot(root)
				err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return err
					}
					module := strings.TrimSuffix(strings.TrimPrefix(path, root+string(filepath.Separator)), ".yaml")
					load, ok := moduleLoaders[module]
					if !ok {
						t.Errorf("no loader for rendered module %q", module)
						return nil
					}
					if err := load(); err != nil {
						t.Errorf("%s: %v", path, err)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			config.SetOverlayRoot("/etc/orbit/config")
		})
	}
}

// writeMountedConfig writes each rendered config-files volume, as the pod sees
// it, under its own temporary root and returns the roots.
func writeMountedConfig(t *testing.T, rendered string) []string {
	t.Helper()
	configMaps := map[string]map[string]string{}
	var volumes []corev1.ConfigMapVolumeSource
	for doc := range strings.SplitSeq(rendered, "\n---\n") {
		var obj struct {
			Kind     string                `json:"kind"`
			Metadata struct{ Name string } `json:"metadata"`
			Data     map[string]string     `json:"data"`
			Spec     struct {
				Volumes  []corev1.Volume `json:"volumes"`
				Template struct {
					Spec corev1.PodSpec `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		if obj.Kind == "ConfigMap" {
			configMaps[obj.Metadata.Name] = obj.Data
		}
		for _, v := range append(obj.Spec.Volumes, obj.Spec.Template.Spec.Volumes...) {
			if v.Name == "config-files" && v.ConfigMap != nil {
				volumes = append(volumes, *v.ConfigMap)
			}
		}
	}
	if len(volumes) == 0 {
		t.Fatal("no workload mounts a config-files volume")
	}
	var roots []string
	for _, v := range volumes {
		data, ok := configMaps[v.Name]
		if !ok {
			t.Fatalf("volume references missing ConfigMap %s", v.Name)
		}
		root := t.TempDir()
		for _, item := range v.Items {
			// A printf misuse in a template renders Go's %!(...) marker into the value.
			if strings.Contains(data[item.Key], "%!") {
				t.Errorf("%s %s has a printf error:\n%s", v.Name, item.Key, data[item.Key])
			}
			path := filepath.Join(root, item.Path)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(data[item.Key]), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		roots = append(roots, root)
	}
	return roots
}
