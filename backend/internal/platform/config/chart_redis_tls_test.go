package config_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/storage"
)

// redisCAUsers returns the path where each rendered Deployment mounts the
// redis-ca volume, and the ConfigMaps whose storage config reads the CA file.
func redisCAUsers(t *testing.T, rendered string) (mounts map[string]string, caFileConfigs []string) {
	t.Helper()
	mounts = map[string]string{}
	for doc := range strings.SplitSeq(rendered, "\n---\n") {
		var obj struct {
			Metadata struct{ Name string } `json:"metadata"`
			Data     map[string]string     `json:"data"`
			Spec     struct {
				Template struct {
					Spec corev1.PodSpec `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(obj.Data["internal.platform.storage.yaml"], "caFile: /etc/orbit/redis-ca/ca.crt") {
			caFileConfigs = append(caFileConfigs, obj.Metadata.Name)
		}
		for _, c := range obj.Spec.Template.Spec.Containers {
			for _, m := range c.VolumeMounts {
				if m.Name == "redis-ca" {
					mounts[obj.Metadata.Name] = m.MountPath
				}
			}
		}
	}
	slices.Sort(caFileConfigs)
	return mounts, caFileConfigs
}

// The chart default turns Redis TLS off, overriding the production default, and mounts no CA.
func TestChartRedisTLSOffByDefault(t *testing.T) {
	out := renderChart(t, "{}\n")
	r := loadRendered[storage.StorageConfig](t, out, "internal/platform/storage").Redis
	if r.TLSEnabled || r.CAFile != "" || r.ServerName != "" {
		t.Errorf("default redis config = tls %v, caFile %q, serverName %q; want plaintext", r.TLSEnabled, r.CAFile, r.ServerName)
	}
	if m, cfgs := redisCAUsers(t, out); len(m) != 0 || len(cfgs) != 0 {
		t.Errorf("default chart mounts the Redis CA in %v and reads it in %v", m, cfgs)
	}
}

// With TLS and a CA secret, only the three Redis clients mount the secret and
// read the CA file from it.
func TestChartRedisTLSWithCA(t *testing.T) {
	out := renderChart(t, "config:\n  redis:\n    tls: {enabled: true, caSecret: redis-ca, serverName: redis.internal}\n")
	r := loadRendered[storage.StorageConfig](t, out, "internal/platform/storage").Redis
	if !r.TLSEnabled || r.ServerName != "redis.internal" {
		t.Errorf("redis config = tls %v, serverName %q", r.TLSEnabled, r.ServerName)
	}
	mounts, cfgs := redisCAUsers(t, out)
	for name, path := range mounts {
		if path != "/etc/orbit/redis-ca" {
			t.Errorf("%s mounts the CA at %s", name, path)
		}
	}
	if names := slices.Sorted(maps.Keys(mounts)); strings.Join(names, ",") != "orbit-identity,orbit-inference-gateway,orbit-triggers" {
		t.Errorf("Redis CA mounted in %v, want identity, inference-gateway and triggers", names)
	}
	if strings.Join(cfgs, ",") != "orbit-identity-config,orbit-inference-gateway-config,orbit-triggers-config" {
		t.Errorf("caFile rendered in %v, want identity, inference-gateway and triggers", cfgs)
	}
}
