package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/storage"
)

// redisConfigs loads the storage config of every rendered workload that mounts one.
func redisConfigs(t *testing.T, rendered string) []storage.RedisConfig {
	t.Helper()
	var out []storage.RedisConfig
	for _, root := range writeMountedConfig(t, rendered) {
		if _, err := os.Stat(filepath.Join(root, "internal", "platform", "storage.yaml")); err != nil {
			continue
		}
		config.SetOverlayRoot(root)
		cfg, err := config.Load[storage.StorageConfig]("internal/platform/storage", "production")
		config.SetOverlayRoot("/etc/orbit/config")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, cfg.Redis)
	}
	if len(out) == 0 {
		t.Fatal("no workload mounts the storage config")
	}
	return out
}

// redisCAMounts returns the Deployments that mount the redis-ca volume, keyed by
// name, with the mount path.
func redisCAMounts(t *testing.T, rendered string) map[string]string {
	t.Helper()
	mounts := map[string]string{}
	for doc := range strings.SplitSeq(rendered, "\n---\n") {
		var d appsv1.Deployment
		if err := yaml.Unmarshal([]byte(doc), &d.TypeMeta); err != nil {
			t.Fatal(err)
		}
		if d.Kind != "Deployment" {
			continue
		}
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
	for _, r := range redisConfigs(t, out) {
		if r.TLSEnabled || r.CAFile != "" || r.ServerName != "" {
			t.Errorf("default redis config = tls %v, caFile %q, serverName %q; want plaintext", r.TLSEnabled, r.CAFile, r.ServerName)
		}
	}
	if m := redisCAMounts(t, out); len(m) != 0 {
		t.Errorf("default chart mounts the Redis CA in %v", m)
	}
}

// With TLS and a CA secret, every storage config points at the CA file, and the
// three Redis clients mount the secret where that file lives.
func TestChartRedisTLSWithCA(t *testing.T) {
	out := renderChart(t, "config:\n  redis:\n    tls: {enabled: true, caSecret: redis-ca, serverName: redis.internal}\n")
	var caFile string
	for _, r := range redisConfigs(t, out) {
		if !r.TLSEnabled || r.ServerName != "redis.internal" || r.CAFile == "" {
			t.Errorf("redis config = tls %v, caFile %q, serverName %q", r.TLSEnabled, r.CAFile, r.ServerName)
		}
		caFile = r.CAFile
	}
	mounts := redisCAMounts(t, out)
	names := make([]string, 0, len(mounts))
	for name, path := range mounts {
		names = append(names, name)
		if path != filepath.Dir(caFile) {
			t.Errorf("%s mounts the CA at %s, config reads %s", name, path, caFile)
		}
	}
	slices.Sort(names)
	if strings.Join(names, ",") != "orbit-identity,orbit-inference-gateway,orbit-triggers" {
		t.Errorf("Redis CA mounted in %v, want identity, inference-gateway and triggers", names)
	}
}
