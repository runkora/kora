package config_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	skillsConfig "github.com/clear-route/orbit/cmd/skills-service/config"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/skills"
)

const skillsMappingValues = `skillsService:
  skillsMapping:
    tenants:
      - id: tenant-a
        marketplaces:
          - https://github.com/example/core-skills
          - https://github.com/example/platform-skills
      - id: tenant-b
        marketplaces:
          - https://github.com/partner/reference-skills
`

func TestChartSkillsMappingLoadsRegistry(t *testing.T) {
	cases := []struct {
		name     string
		values   string
		wantFile bool
		want     map[string][]string
	}{
		{
			// The chart leaves the key to the operator unless sources are listed.
			name:   "empty defaults",
			values: "{}\n",
			want:   map[string][]string{"tenant-a": nil},
		},
		{
			name:     "tenant sources",
			values:   skillsMappingValues,
			wantFile: true,
			want: map[string][]string{
				"tenant-a": {"example__core-skills", "example__platform-skills"},
				"tenant-b": {"partner__reference-skills"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderChart(t, tc.values, "--show-only", "templates/skills-service/configmap.yaml")
			var cm corev1.ConfigMap
			if err := yaml.Unmarshal([]byte(out), &cm); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for name, content := range cm.Data {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("ORBIT_MARKETPLACE_SOURCES_CONFIG", dir)
			t.Setenv("GITHUB_APP_ID", "12345")
			t.Setenv("GITHUB_APP_INSTALLATION_ID", "67890")
			t.Setenv("GITHUB_APP_PRIVATE_KEY_PATH", filepath.Join(dir, "privateKey"))
			var cfg skillsConfig.Config
			if err := cfg.ApplyEnvOverrides(); err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(cfg.Marketplace.SourcesConfigPath)
			if tc.wantFile && statErr != nil {
				t.Fatalf("chart did not supply the file skills-service reads: %v", statErr)
			}
			if !tc.wantFile && statErr == nil {
				t.Fatalf("chart supplied %s with no sources listed; the key belongs to the operator", cfg.Marketplace.SourcesConfigPath)
			}
			registry, err := skills.NewRegistry(cfg.Marketplace.SourcesConfigPath, logging.NewDevelopmentLogger())
			if err != nil {
				t.Fatal(err)
			}
			for tenant, want := range tc.want {
				var got []string
				for _, source := range registry.ListSources(tenant) {
					got = append(got, source.MarketplaceSlug)
				}
				if !slices.Equal(got, want) {
					t.Errorf("tenant %s sources = %v, want %v", tenant, got, want)
				}
			}
			if got := registry.ListSources("unconfigured-tenant"); len(got) != 0 {
				t.Errorf("unconfigured tenant received sources: %v", got)
			}
		})
	}
}

func TestChartSkillsMappingChangeRollsDeployment(t *testing.T) {
	checksum := func(values string) string {
		t.Helper()
		out := renderChart(t, values, "--show-only", "templates/skills-service/deployment.yaml")
		var deploy appsv1.Deployment
		if err := yaml.Unmarshal([]byte(out), &deploy); err != nil {
			t.Fatal(err)
		}
		sum := deploy.Spec.Template.Annotations["checksum/marketplace-sources"]
		if sum == "" {
			t.Fatal("skills-service has no source configuration checksum")
		}
		return sum
	}
	initial := checksum(skillsMappingValues)
	updated := checksum(strings.Replace(skillsMappingValues, "example/core-skills", "example/new-skills", 1))
	if initial == updated {
		t.Error("changing Marketplace sources did not change the pod template checksum")
	}
}

func TestChartSkillsMappingRejectsInvalidShape(t *testing.T) {
	renderChart(t, "{}\n", "--show-only", "templates/skills-service/configmap.yaml")
	chart := filepath.Join("..", "..", "..", "..", "charts", "orbit")
	out, err := exec.Command("helm", "template", "orbit", chart, "-n", "orbit-system",
		"-f", filepath.Join(chart, "ci", "default-values.yaml"),
		"--show-only", "templates/skills-service/configmap.yaml",
		"--set-string", "skillsService.skillsMapping.tenants=not-a-list").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "skillsMapping") {
		t.Errorf("invalid tenant mapping was not rejected by the chart schema: %v\n%s", err, out)
	}
}

func TestChartSkillsServiceWiring(t *testing.T) {
	cfg, err := config.LoadService[skillsConfig.Config]("cmd/skills-service", "production")
	if err != nil {
		t.Fatal(err)
	}
	values := `skillsService:
  extraEnv:
    - name: MARKETPLACE_CACHE_TTL
      value: 15m
`
	decode := func(template string, target any) {
		t.Helper()
		out := renderChart(t, values, "--show-only", template)
		if err := yaml.Unmarshal([]byte(out), target); err != nil {
			t.Fatal(err)
		}
	}
	var deploy appsv1.Deployment
	decode("templates/skills-service/deployment.yaml", &deploy)
	if len(deploy.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected one skills-service container, got %d", len(deploy.Spec.Template.Spec.Containers))
	}
	container := deploy.Spec.Template.Spec.Containers[0]
	port := int32(cfg.Server.Port)
	if len(container.Ports) != 1 || container.Ports[0].ContainerPort != port {
		t.Errorf("container ports = %v, want backend port %d", container.Ports, port)
	}
	for name, probe := range map[string]*corev1.Probe{
		"liveness": container.LivenessProbe, "readiness": container.ReadinessProbe,
	} {
		if probe == nil || probe.HTTPGet == nil || probe.HTTPGet.Port.IntVal != port {
			t.Errorf("%s probe = %v, want HTTP backend port %d", name, probe, port)
		}
	}
	envCounts := make(map[string]int)
	for _, env := range container.Env {
		envCounts[env.Name]++
		if envCounts[env.Name] > 1 {
			t.Errorf("environment variable %s is duplicated", env.Name)
		}
	}
	if envCounts["MARKETPLACE_CACHE_TTL"] != 1 {
		t.Error("custom environment variable was not rendered exactly once")
	}
	var service corev1.Service
	decode("templates/skills-service/service.yaml", &service)
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != port || service.Spec.Ports[0].TargetPort.IntVal != port {
		t.Errorf("service ports = %v, want backend port %d", service.Spec.Ports, port)
	}
	var frontend corev1.ConfigMap
	decode("templates/frontend/configmap.yaml", &frontend)
	want := fmt.Sprintf("http://orbit-skills-service.orbit-system.svc.cluster.local:%d/v1", port)
	if got := frontend.Data["SKILLS_SERVICE_URL"]; got != want {
		t.Errorf("frontend Skills URL = %q, want %q", got, want)
	}
}
