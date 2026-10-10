package config_test

import (
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"
)

// Each Deployment receives only the platform Secret keys its service reads, by
// secretKeyRef, and never the whole Secret through envFrom.
func TestChartPlatformSecretKeysPerService(t *testing.T) {
	out := renderChart(t, "")
	want := map[string][]string{
		"orbit-identity":               {"DB_PASSWORD", "REDIS_PASSWORD", "ORBIT_TOKEN_SIGNING_KEY"},
		"orbit-triggers":               {"DB_PASSWORD", "REDIS_PASSWORD", "ORBIT_TRIGGERS_ENCRYPTION_KEY"},
		"orbit-agents":                 {"DB_PASSWORD"},
		"orbit-inference-gateway":      {"DB_PASSWORD", "REDIS_PASSWORD"},
		"orbit-policy-service":         {"DB_PASSWORD", "REDIS_PASSWORD"},
		"orbit-tool-gateway":           {"DB_PASSWORD"},
		"orbit-tool-gateway-publisher": {"DB_PASSWORD"},
		"orbit-egress-gateway":         nil,
	}
	seen := map[string]bool{}
	for doc := range strings.SplitSeq(out, "\n---\n") {
		var d appsv1.Deployment
		if err := yaml.Unmarshal([]byte(doc), &d); err != nil || d.Kind != "Deployment" {
			continue
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			for _, ef := range c.EnvFrom {
				if ef.SecretRef != nil && ef.SecretRef.Name == "orbit-secrets" {
					t.Errorf("%s/%s loads orbit-secrets through envFrom, want per-key secretKeyRef", d.Name, c.Name)
				}
			}
			var got []string
			for _, e := range c.Env {
				if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil && e.ValueFrom.SecretKeyRef.Name == "orbit-secrets" {
					got = append(got, e.ValueFrom.SecretKeyRef.Key)
				}
			}
			keys, ok := want[d.Name]
			if !ok {
				continue
			}
			seen[d.Name] = true
			slices.Sort(got)
			slices.Sort(keys)
			if !slices.Equal(got, keys) {
				t.Errorf("%s/%s orbit-secrets keys = %v, want %v", d.Name, c.Name, got, keys)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("Deployment %s not rendered", name)
		}
	}
}

// A platform key a service's extraEnv sets is left to extraEnv, so the pod has
// one env entry with that name rather than a duplicate.
func TestChartExtraEnvOverridesPlatformSecretKey(t *testing.T) {
	out := renderChart(t, "identity:\n  extraEnv:\n    - name: DB_PASSWORD\n      valueFrom:\n        secretKeyRef: {name: external-db, key: password}\n")
	for doc := range strings.SplitSeq(out, "\n---\n") {
		var d appsv1.Deployment
		if err := yaml.Unmarshal([]byte(doc), &d); err != nil || d.Kind != "Deployment" || d.Name != "orbit-identity" {
			continue
		}
		var sources []string
		for _, e := range d.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "DB_PASSWORD" && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				sources = append(sources, e.ValueFrom.SecretKeyRef.Name)
			}
		}
		if !slices.Equal(sources, []string{"external-db"}) {
			t.Errorf("orbit-identity DB_PASSWORD sources = %v, want [external-db]", sources)
		}
		return
	}
	t.Error("Deployment orbit-identity not rendered")
}
