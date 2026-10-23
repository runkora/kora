package config_test

import (
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// Each workload receives only the platform Secret keys its service reads, by
// secretKeyRef, and never the whole Secret through envFrom.
func TestChartPlatformSecretKeysPerService(t *testing.T) {
	rendered := workloadContainers(renderChart(t, ""))
	want := map[string][]string{
		"orbit-identity":               {"DB_PASSWORD", "KORA_TOKEN_SIGNING_KEY", "REDIS_PASSWORD"},
		"orbit-triggers":               {"DB_PASSWORD", "KORA_TRIGGERS_ENCRYPTION_KEY", "REDIS_PASSWORD"},
		"orbit-agents":                 {"DB_PASSWORD", "REDIS_PASSWORD"},
		"orbit-inference-gateway":      {"DB_PASSWORD", "REDIS_PASSWORD"},
		"orbit-policy-service":         {"DB_PASSWORD", "REDIS_PASSWORD"},
		"orbit-tool-gateway":           {"DB_PASSWORD", "REDIS_PASSWORD"},
		"orbit-tool-gateway-publisher": {"DB_PASSWORD"},
		"orbit-egress-gateway":         {"REDIS_PASSWORD"},
		"orbit-policy-gateway":         {"REDIS_PASSWORD"},
		"orbit-migrate":                {"DB_PASSWORD"},
	}
	for name, keys := range want {
		c, ok := rendered[name]
		if !ok {
			t.Errorf("%s not rendered", name)
			continue
		}
		for _, ef := range c.EnvFrom {
			if ef.SecretRef != nil && ef.SecretRef.Name == "orbit-secrets" {
				t.Errorf("%s loads orbit-secrets through envFrom", name)
			}
		}
		var got []string
		for _, e := range c.Env {
			if ref := secretRef(e); ref != nil && ref.Name == "orbit-secrets" {
				got = append(got, ref.Key)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, keys) {
			t.Errorf("%s orbit-secrets keys = %v, want %v", name, got, keys)
		}
	}
}

// A platform key set in a workload's extraEnv replaces the platform entry.
func TestChartExtraEnvOverridesPlatformSecretKey(t *testing.T) {
	override := "  extraEnv:\n    - name: DB_PASSWORD\n      valueFrom:\n        secretKeyRef: {name: external-db, key: password}\n"
	rendered := workloadContainers(renderChart(t, "identity:\n"+override+"migrate:\n"+override))
	for _, name := range []string{"orbit-identity", "orbit-migrate"} {
		var sources []string
		for _, e := range rendered[name].Env {
			if ref := secretRef(e); e.Name == "DB_PASSWORD" && ref != nil {
				sources = append(sources, ref.Name)
			}
		}
		if !slices.Equal(sources, []string{"external-db"}) {
			t.Errorf("%s DB_PASSWORD sources = %v, want [external-db]", name, sources)
		}
	}
}

// workloadContainers maps each rendered Deployment and Job name to its first
// container. Both kinds carry the pod template at spec.template.
func workloadContainers(out string) map[string]corev1.Container {
	containers := map[string]corev1.Container{}
	for doc := range strings.SplitSeq(out, "\n---\n") {
		var w appsv1.Deployment
		if yaml.Unmarshal([]byte(doc), &w) != nil || (w.Kind != "Deployment" && w.Kind != "Job") || len(w.Spec.Template.Spec.Containers) == 0 {
			continue
		}
		containers[w.Name] = w.Spec.Template.Spec.Containers[0]
	}
	return containers
}

func secretRef(e corev1.EnvVar) *corev1.SecretKeySelector {
	if e.ValueFrom == nil {
		return nil
	}
	return e.ValueFrom.SecretKeyRef
}
