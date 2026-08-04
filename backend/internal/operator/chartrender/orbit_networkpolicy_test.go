package chartrender

import (
	"strings"
	"testing"
)

// docs/adr/2026-08-04-devenv-mode-platform-support.md, F-10: the orbit
// chart's NetworkPolicy templates must correctly support installing with
// networkPolicy.enabled=false — a generic toggle (not devenv-specific)
// that, per the PRD, had no automated coverage below a live cluster run.

func TestNetworkPolicies_RenderedWhenEnabled(t *testing.T) {
	values := mergeValues(orbitBaseValues(), map[string]any{
		"networkPolicy": map[string]any{"enabled": true},
	})
	objects := Render(t, orbitChartPath, values)

	found := 0
	for key := range objects {
		if strings.HasPrefix(key, "NetworkPolicy/") {
			found++
		}
	}
	if found == 0 {
		t.Error("expected NetworkPolicy objects to render when networkPolicy.enabled=true")
	}
}

func TestNetworkPolicies_SuppressedWhenDisabled(t *testing.T) {
	values := mergeValues(orbitBaseValues(), map[string]any{
		"networkPolicy": map[string]any{"enabled": false},
	})
	objects := Render(t, orbitChartPath, values)

	for key := range objects {
		if strings.HasPrefix(key, "NetworkPolicy/") {
			t.Errorf("expected zero NetworkPolicy objects when networkPolicy.enabled=false, found %s", key)
		}
	}
}
