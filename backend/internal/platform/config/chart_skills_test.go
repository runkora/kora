package config_test

import (
	"fmt"
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
