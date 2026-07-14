package operator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestDevEnvClusterSPIFFEIDSpecs_SelectorsAreDisjoint guards pinned decision
// 4: the component-scoped and per-pod-catch-all host ClusterSPIFFEIDs must
// select mutually exclusive pod sets. Overlapping selectors let a pod match
// both ClusterSPIFFEIDs at once, and SPIRE's SDS handler resolves the
// ambiguous "default" resource to whichever entry was created first — not
// guaranteed to be the intended one — giving the pod a nondeterministic
// SPIFFE identity.
func TestDevEnvClusterSPIFFEIDSpecs_SelectorsAreDisjoint(t *testing.T) {
	env := newTestEnvForIdentity("alice-env")
	specs := devEnvClusterSPIFFEIDSpecs(env, "orbit.test")

	componentName := devEnvComponentClusterSPIFFEIDName(env)
	catchAllName := devEnvClusterSPIFFEIDName(env)

	componentSpec, ok := specs[componentName]
	if !ok {
		t.Fatalf("missing component-scoped ClusterSPIFFEID %q", componentName)
	}
	catchAllSpec, ok := specs[catchAllName]
	if !ok {
		t.Fatalf("missing per-pod catch-all ClusterSPIFFEID %q", catchAllName)
	}

	componentOp := podSelectorOperator(t, componentSpec, "spiffe.io/component")
	catchAllOp := podSelectorOperator(t, catchAllSpec, "spiffe.io/component")

	if componentOp != "Exists" {
		t.Errorf("component-scoped podSelector operator = %q, want Exists", componentOp)
	}
	if catchAllOp != "DoesNotExist" {
		t.Errorf("catch-all podSelector operator = %q, want DoesNotExist", catchAllOp)
	}
	if componentOp == catchAllOp {
		t.Fatalf("both ClusterSPIFFEIDs use operator %q — selectors are not disjoint, a pod could match both", componentOp)
	}
}

// TestDevEnvClusterSPIFFEIDSpecs_NamespaceScopedToEnvironment asserts both
// ClusterSPIFFEIDs are scoped to the environment's own flattened host
// namespace (via the orbit.io/devenv label), never cluster-wide — otherwise
// the disjoint podSelectors above would still leak identity to unrelated
// environments' pods.
func TestDevEnvClusterSPIFFEIDSpecs_NamespaceScopedToEnvironment(t *testing.T) {
	env := newTestEnvForIdentity("alice-env")
	specs := devEnvClusterSPIFFEIDSpecs(env, "orbit.test")

	for name, spec := range specs {
		nsSelector, ok := spec["namespaceSelector"].(map[string]any)
		if !ok {
			t.Fatalf("%s: namespaceSelector missing or wrong type", name)
		}
		matchLabels, ok := nsSelector["matchLabels"].(map[string]any)
		if !ok {
			t.Fatalf("%s: namespaceSelector.matchLabels missing or wrong type", name)
		}
		if matchLabels["orbit.io/devenv"] != env.Name {
			t.Errorf("%s: namespaceSelector.matchLabels[orbit.io/devenv] = %v, want %q", name, matchLabels["orbit.io/devenv"], env.Name)
		}
	}
}

// podSelectorOperator extracts the operator of the single matchExpressions
// entry keyed by key from spec.podSelector, failing the test on any
// unexpected shape.
func podSelectorOperator(t *testing.T, spec map[string]any, key string) string {
	t.Helper()
	podSelector, ok := spec["podSelector"].(map[string]any)
	if !ok {
		t.Fatalf("podSelector missing or wrong type in %#v", spec)
	}
	exprs, ok := podSelector["matchExpressions"].([]any)
	if !ok || len(exprs) != 1 {
		t.Fatalf("podSelector.matchExpressions = %#v, want exactly one entry", podSelector["matchExpressions"])
	}
	expr, ok := exprs[0].(map[string]any)
	if !ok {
		t.Fatalf("matchExpressions[0] wrong type: %#v", exprs[0])
	}
	if expr["key"] != key {
		t.Fatalf("matchExpressions[0].key = %v, want %q", expr["key"], key)
	}
	op, ok := expr["operator"].(string)
	if !ok {
		t.Fatalf("matchExpressions[0].operator wrong type: %#v", expr["operator"])
	}
	return op
}

func newTestEnvForIdentity(name string) *OrbitDevEnvironment {
	return &OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}
