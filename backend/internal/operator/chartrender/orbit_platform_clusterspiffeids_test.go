package chartrender

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const orbitPlatformChartPath = "../../../../charts/orbit-platform"

// docs/adr/2026-08-04-devenv-mode-platform-support.md, F-1: spire.enabled=false
// must suppress every ClusterSPIFFEID template render entirely — a
// SPIRE-disabled (devenv-mode) install has no in-vcluster SPIRE controller
// manager to satisfy them.

func TestClusterSPIFFEIDs_RenderedWhenSpireEnabled(t *testing.T) {
	values := mergeValues(orbitPlatformBaseValues(), map[string]any{
		"spire": map[string]any{"enabled": true},
	})
	objects := Render(t, orbitPlatformChartPath, values)

	found := 0
	for key := range objects {
		if strings.HasPrefix(key, "ClusterSPIFFEID/") {
			found++
		}
	}
	if found == 0 {
		t.Error("expected ClusterSPIFFEID objects to render when spire.enabled=true")
	}
}

func TestClusterSPIFFEIDs_SuppressedWhenSpireDisabled(t *testing.T) {
	values := mergeValues(orbitPlatformBaseValues(), map[string]any{
		"spire": map[string]any{"enabled": false},
	})
	objects := Render(t, orbitPlatformChartPath, values)

	for key := range objects {
		if strings.HasPrefix(key, "ClusterSPIFFEID/") {
			t.Errorf("expected zero ClusterSPIFFEID objects when spire.enabled=false, found %s", key)
		}
	}
}

func TestClusterSPIFFEIDs_IstioSidecarExcludesDevenvNamespaces(t *testing.T) {
	values := mergeValues(orbitPlatformBaseValues(), map[string]any{
		"spire": map[string]any{"enabled": true},
	})
	objects := Render(t, orbitPlatformChartPath, values)

	sidecar, ok := objects["ClusterSPIFFEID/istio-sidecar"]
	if !ok {
		t.Fatal("expected ClusterSPIFFEID/istio-sidecar to render")
	}

	matchExpressions, found, err := unstructured.NestedSlice(sidecar.Object, "spec", "namespaceSelector", "matchExpressions")
	if err != nil || !found {
		t.Fatalf("spec.namespaceSelector.matchExpressions: found=%v err=%v", found, err)
	}

	excludesDevenv := false
	for _, expr := range matchExpressions {
		exprMap, ok := expr.(map[string]any)
		if !ok || exprMap["key"] != "orbit.io/role" {
			continue
		}
		values, ok := exprMap["values"].([]any)
		if !ok {
			continue
		}
		for _, v := range values {
			if v == "devenv" {
				excludesDevenv = true
			}
		}
	}
	if !excludesDevenv {
		t.Error("expected istio-sidecar ClusterSPIFFEID's orbit.io/role NotIn exclusion to include \"devenv\"")
	}
}
