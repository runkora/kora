package config_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/storage"
)

// redisCAMounts returns the path where each rendered Deployment mounts the redis-ca volume.
func redisCAMounts(t *testing.T, rendered string) map[string]string {
	t.Helper()
	mounts := map[string]string{}
	for doc := range strings.SplitSeq(rendered, "\n---\n") {
		var d appsv1.Deployment
		if err := yaml.Unmarshal([]byte(doc), &d); err != nil {
			t.Fatal(err)
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			for _, m := range c.VolumeMounts {
				if m.Name == "redis-ca" {
					mounts[d.Name] = m.MountPath
				}
			}
		}
	}
	return mounts
}

// The chart default turns Redis TLS off, overriding the production default, and mounts no CA.
func TestChartRedisTLSOffByDefault(t *testing.T) {
	out := renderChart(t, "{}\n")
	r := loadRendered[storage.StorageConfig](t, out, "internal/platform/storage").Redis
	if r.TLSEnabled || r.CAFile != "" || r.ServerName != "" {
		t.Errorf("default redis config = tls %v, caFile %q, serverName %q; want plaintext", r.TLSEnabled, r.CAFile, r.ServerName)
	}
	if m := redisCAMounts(t, out); len(m) != 0 {
		t.Errorf("default chart mounts the Redis CA in %v", m)
	}
}

// With TLS and a CA secret, the storage config reads the CA file from the
// directory where the three Redis clients mount the secret.
func TestChartRedisTLSWithCA(t *testing.T) {
	out := renderChart(t, "config:\n  redis:\n    tls: {enabled: true, caSecret: redis-ca, serverName: redis.internal}\n")
	r := loadRendered[storage.StorageConfig](t, out, "internal/platform/storage").Redis
	if !r.TLSEnabled || r.ServerName != "redis.internal" || r.CAFile != "/etc/orbit/redis-ca/ca.crt" {
		t.Errorf("redis config = tls %v, caFile %q, serverName %q", r.TLSEnabled, r.CAFile, r.ServerName)
	}
	mounts := redisCAMounts(t, out)
	for name, path := range mounts {
		if path != "/etc/orbit/redis-ca" {
			t.Errorf("%s mounts the CA at %s", name, path)
		}
	}
	if names := slices.Sorted(maps.Keys(mounts)); strings.Join(names, ",") != "orbit-identity,orbit-inference-gateway,orbit-triggers" {
		t.Errorf("Redis CA mounted in %v, want identity, inference-gateway and triggers", names)
	}
}
