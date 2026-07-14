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
	VCluster  string
}

// defaultChartVersions returns the pinned versions that match
// scripts/cluster/versions.env. Update both locations together when bumping —
// the version-bump regression suite (scripts/cluster/spike-*.sh, run against
// the cloud host per the vcluster ADR §2 pinned decision 1/2) re-verifies
// each pin on every bump.
func defaultChartVersions() *chartVersions {
	return &chartVersions{
		SPIRECRDs: "0.5.0",
		SPIRE:     "0.28.3",
		Istio:     "1.29.1",
		VCluster:  "0.35.1",
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

// Default image references for Orbit platform services.
// These are intentionally blank until images are published to a registry.
// Once published, set these to versioned OCI references:
//
//	const defaultIdentityImage = "ghcr.io/orbit-sh/orbit-identity:v0.1.0"
const (
	defaultIdentityImage      = ""
	defaultIdentityProxyImage = ""
)

// orbitImageRef returns the full image reference for an Orbit platform service.
// Resolution order:
//  1. Built-in versioned constant (defaultXxxImage) — used in production once published.
//  2. imageRegistry prefix from the OrbitPlatform CR — used for local dev and air-gapped
//     environments: "{imageRegistry}/orbit-{service}:latest"
//
// If neither is set (both blank), returns a bare "orbit-{service}:latest" which will
// only work if the image is already present on the node (e.g. in a local kind/k3d cluster
// loaded directly via `docker load`).
func orbitImageRef(defaultImage, service, imageRegistry string) string {
	if defaultImage != "" {
		return defaultImage
	}
	if imageRegistry != "" {
		return fmt.Sprintf("%s/orbit-%s:latest", imageRegistry, service)
	}
	return fmt.Sprintf("orbit-%s:latest", service)
}

// tenantNamespace returns the Kubernetes namespace to use for a tenant.
// If spec.namespace is set it is used as-is; otherwise the namespace is
// derived as "orbit-tenant-{tenantID}" so that users don't have to declare
// it explicitly in the OrbitTenant CR.
func tenantNamespace(t *OrbitTenant) string {
	if t.Spec.Namespace != "" {
		return t.Spec.Namespace
	}
	return fmt.Sprintf("orbit-tenant-%s", t.Name)
}
