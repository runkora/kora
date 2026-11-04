package config_test

import (
	"fmt"
	"maps"
	"strings"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	skillsConfig "github.com/runkora/kora/backend/cmd/skills-service/config"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/marketplace"
)

func TestChartSkillsServiceWiring(t *testing.T) {
	cfg, err := config.LoadService[skillsConfig.Config]("cmd/skills-service", "production")
	if err != nil {
		t.Fatal(err)
	}
	values := `skillsService:
  extraEnv:
    - name: EXTRA_SETTING
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
	if envCounts["EXTRA_SETTING"] != 1 {
		t.Error("custom environment variable was not rendered exactly once")
	}
	mounts := map[string]string{}
	for _, m := range container.VolumeMounts {
		mounts[m.Name] = m.MountPath
	}
	if mounts["skill-marketplaces"] != filepath.Dir(cfg.Marketplace.SourcesPath) || mounts["skill-marketplace-secrets"] != cfg.Marketplace.KeysDir {
		t.Errorf("mounts = %v, want the config's sourcesPath directory and keysDir", mounts)
	}
	sources := map[string]string{}
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		switch {
		case v.ConfigMap != nil:
			sources[v.Name] = v.ConfigMap.Name
		case v.Secret != nil:
			sources[v.Name] = v.Secret.SecretName
		}
	}
	if sources["skill-marketplaces"] != marketplace.ConfigMapName || sources["skill-marketplace-secrets"] != marketplace.SecretName {
		t.Errorf("volumes = %v, want the operator's ConfigMap and Secret", sources)
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

// The chart renders skillsService.limits into the service config, and an empty
// value renders zero, which skills-service reads as no cap.
func TestChartSkillsServiceLimits(t *testing.T) {
	render := func(values string) map[string]int64 {
		t.Helper()
		var cm corev1.ConfigMap
		if err := yaml.Unmarshal([]byte(renderChart(t, values, "--show-only", "templates/skills-service/config.yaml")), &cm); err != nil {
			t.Fatal(err)
		}
		var module struct {
			Marketplace struct {
				Limits map[string]int64 `json:"limits"`
			} `json:"marketplace"`
		}
		if err := yaml.Unmarshal([]byte(cm.Data["cmd.skills-service.yaml"]), &module); err != nil {
			t.Fatal(err)
		}
		return module.Marketplace.Limits
	}
	want := map[string]int64{"skillsPerMarketplace": 500, "filesPerSkill": 500, "bytesPerFile": 131072, "skillMdBytesPerMarketplace": 8388608}
	if got := render("{}\n"); !maps.Equal(got, want) {
		t.Errorf("default limits = %v, want %v", got, want)
	}
	want["filesPerSkill"] = 0
	if got := render("skillsService:\n  limits:\n    filesPerSkill:\n"); !maps.Equal(got, want) {
		t.Errorf("empty filesPerSkill = %v, want %v", got, want)
	}
}

// Under REGISTRY_ONLY the chart registers api.github.com for skills-service.
// Under ALLOW_ANY it renders no ServiceEntry.
func TestChartGitHubServiceEntry(t *testing.T) {
	out := renderChart(t, "istio:\n  outboundTrafficPolicy: REGISTRY_ONLY\n", "--show-only", "templates/platform/serviceentries.yaml")
	var found bool
	for doc := range strings.SplitSeq(out, "\n---\n") {
		var entry struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				ExportTo []string `json:"exportTo"`
				Hosts    []string `json:"hosts"`
				Location string   `json:"location"`
				Ports    []struct {
					Number   int    `json:"number"`
					Protocol string `json:"protocol"`
				} `json:"ports"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &entry); err != nil || entry.Metadata.Name != "kora-github-api" {
			continue
		}
		found = true
		if len(entry.Spec.Hosts) != 1 || entry.Spec.Hosts[0] != "api.github.com" || entry.Spec.Location != "MESH_EXTERNAL" ||
			len(entry.Spec.Ports) != 1 || entry.Spec.Ports[0].Number != 443 || entry.Spec.Ports[0].Protocol != "TLS" {
			t.Errorf("ServiceEntry = %+v, want api.github.com:443 TLS, mesh external", entry.Spec)
		}
		if len(entry.Spec.ExportTo) != 1 || entry.Spec.ExportTo[0] != "." {
			t.Errorf("exportTo = %v, want only the system namespace", entry.Spec.ExportTo)
		}
	}
	if !found {
		t.Errorf("no kora-github-api ServiceEntry under REGISTRY_ONLY:\n%s", out)
	}
}
