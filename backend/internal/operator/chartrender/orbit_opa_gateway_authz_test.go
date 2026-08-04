package chartrender

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const orbitChartPath = "../../../../charts/orbit"

const authzPolicyName = "AuthorizationPolicy/orbit-policy-service-gateway-bundle"

// docs/adr/2026-08-04-devenv-mode-platform-support.md, F-7: opa-gateway's
// policy-bundle-fetch AuthorizationPolicy must accept every identity shape a
// SPIRE-disabled devenv workload can actually present — a genuine startup
// race between host-SPIRE delivery and istiod's own fallback CA, so BOTH
// plausible shapes must be exempted, never just one.

func notPrincipals(t *testing.T, objects map[string]*unstructured.Unstructured) []any {
	t.Helper()
	policy, ok := objects[authzPolicyName]
	if !ok {
		t.Fatalf("expected %s to render", authzPolicyName)
	}
	rules, found, err := unstructured.NestedSlice(policy.Object, "spec", "rules")
	if err != nil || !found || len(rules) == 0 {
		t.Fatalf("spec.rules: found=%v err=%v", found, err)
	}
	rule, ok := rules[0].(map[string]any)
	if !ok {
		t.Fatal("spec.rules[0] is not an object")
	}
	from, ok := rule["from"].([]any)
	if !ok || len(from) == 0 {
		t.Fatal("spec.rules[0].from is empty")
	}
	fromEntry, ok := from[0].(map[string]any)
	if !ok {
		t.Fatal("spec.rules[0].from[0] is not an object")
	}
	source, ok := fromEntry["source"].(map[string]any)
	if !ok {
		t.Fatal("spec.rules[0].from[0].source is not an object")
	}
	notPrincipals, ok := source["notPrincipals"].([]any)
	if !ok {
		t.Fatal("spec.rules[0].from[0].source.notPrincipals is not a list")
	}
	return notPrincipals
}

func TestOPAGatewayAuthz_HostInstallExemptsOnlyTheSpireShape(t *testing.T) {
	values := mergeValues(orbitBaseValues(), map[string]any{
		"platform": map[string]any{
			"spiffe": map[string]any{
				"enabled":      true,
				"spireEnabled": true,
				"devEnvID":     "",
			},
		},
	})
	objects := Render(t, orbitChartPath, values)
	principals := notPrincipals(t, objects)

	if len(principals) != 1 {
		t.Fatalf("host install: expected exactly one exempted principal, got %v", principals)
	}
	if principals[0] != "cluster.local/system/opa-gateway" {
		t.Errorf("host install: notPrincipals[0] = %v, want %q", principals[0], "cluster.local/system/opa-gateway")
	}
}

func TestOPAGatewayAuthz_DevenvInstallExemptsBothShapes(t *testing.T) {
	values := mergeValues(orbitBaseValues(), map[string]any{
		"platform": map[string]any{
			"spiffe": map[string]any{
				"enabled":      true,
				"spireEnabled": false,
				"devEnvID":     "spike-1",
			},
		},
	})
	objects := Render(t, orbitChartPath, values)
	principals := notPrincipals(t, objects)

	if len(principals) != 2 {
		t.Fatalf("devenv install: expected exactly two exempted principals (SPIRE-style + istiod-fallback), got %v", principals)
	}

	wantSpireStyle := "cluster.local/devenv/spike-1/system/opa-gateway"
	wantIstiodFallback := "*/sa/orbit-opa-gateway-x-orbit-system-x-vcluster"

	got := map[string]bool{}
	for _, p := range principals {
		got[p.(string)] = true
	}
	if !got[wantSpireStyle] {
		t.Errorf("devenv install: missing SPIRE-style principal %q in %v", wantSpireStyle, principals)
	}
	if !got[wantIstiodFallback] {
		t.Errorf("devenv install: missing istiod-fallback principal %q in %v", wantIstiodFallback, principals)
	}
}
