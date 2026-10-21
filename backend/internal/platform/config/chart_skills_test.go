package config_test

import (
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
		name   string
		values string
		want   map[string][]string
	}{
		{
			name:   "empty defaults",
			values: "{}\n",
			want:   map[string][]string{"tenant-a": nil},
		},
		{
			name:   "tenant sources",
			values: skillsMappingValues,
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
			if _, err := os.Stat(cfg.Marketplace.SourcesConfigPath); err != nil {
				t.Fatalf("chart did not supply the file skills-service reads: %v", err)
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
