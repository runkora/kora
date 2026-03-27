package operator

import (
	"fmt"
	"maps"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

// chartVersions holds the pinned Helm chart versions for all platform releases.
// These must stay in sync with scripts/cluster/versions.env.
type chartVersions struct {
	SPIRECRDs string
	SPIRE     string
	Istio     string
}

// defaultChartVersions returns the pinned versions that match
// scripts/cluster/versions.env. Update both locations together when bumping.
func defaultChartVersions() *chartVersions {
	return &chartVersions{
		SPIRECRDs: "0.5.0",
		SPIRE:     "0.28.3",
		Istio:     "1.29.1",
	}
}

// readYAMLFile reads a YAML file from disk and returns it as an untyped map.
// Returns an error if the file does not exist or is not valid YAML.
func readYAMLFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}
	out := make(map[string]any)
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse YAML %q: %w", path, err)
	}
	return out, nil
}

// mergeMaps performs a shallow merge of src into dst and returns the result.
// Keys in src override keys in dst; nested maps are merged recursively.
// Neither input map is modified.
func mergeMaps(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst))
	maps.Copy(out, dst)
	for k, v := range src {
		if vMap, ok := v.(map[string]any); ok {
			if dstMap, ok := out[k].(map[string]any); ok {
				out[k] = mergeMaps(dstMap, vMap)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// isHelmBusy returns true when the error is Helm's "another operation
// (install/upgrade/rollback) is in progress" sentinel. This happens when two
// reconcile goroutines race on the same release. The caller should requeue
// with a short backoff rather than marking the platform as failed.
func isHelmBusy(err error) bool {
	return err != nil && strings.Contains(err.Error(), "another operation (install/upgrade/rollback) is in progress")
}

// toMap safely casts an interface{} to map[string]interface{}.
// Returns an empty map when the cast fails (e.g. when the value is nil).
func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
