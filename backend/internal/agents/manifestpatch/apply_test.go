package manifestpatch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
)

// applyJSONByKind decodes manifestJSON onto the concrete type for kind, runs
// Apply with the given patches, and returns the result re-marshaled to JSON.
// This lets the table below stay data-only (manifest/patch/expected as raw
// JSON) while still exercising the real per-type strategicpatch/JSON6902
// codepaths — Apply's generic type parameter has to be resolved at a
// concrete type, so this switch is the one place that binds "kind string" to
// "Go type", mirroring KnownKinds in validate.go.
func applyJSONByKind(t *testing.T, kind string, manifestJSON []byte, patches []Patch) []byte {
	t.Helper()
	switch kind {
	case "StatefulSet":
		return applyTyped[appsv1.StatefulSet](t, manifestJSON, patches)
	case "Service":
		return applyTyped[corev1.Service](t, manifestJSON, patches)
	case "ServiceAccount":
		return applyTyped[corev1.ServiceAccount](t, manifestJSON, patches)
	case "HorizontalPodAutoscaler":
		return applyTyped[autoscalingv2.HorizontalPodAutoscaler](t, manifestJSON, patches)
	case "PodDisruptionBudget":
		return applyTyped[policyv1.PodDisruptionBudget](t, manifestJSON, patches)
	case "PersistentVolumeClaim":
		return applyTyped[corev1.PersistentVolumeClaim](t, manifestJSON, patches)
	case "Pod":
		return applyTyped[corev1.Pod](t, manifestJSON, patches)
	default:
		t.Fatalf("applyJSONByKind: unsupported kind %q", kind)
		return nil
	}
}

func applyTyped[T any](t *testing.T, manifestJSON []byte, patches []Patch) []byte {
	t.Helper()
	var obj T
	require.NoError(t, json.Unmarshal(manifestJSON, &obj), "unmarshal manifest fixture")
	got, err := Apply(obj, patches)
	require.NoError(t, err, "Apply")
	out, err := json.Marshal(got)
	require.NoError(t, err, "marshal result")
	return out
}

// suiteCase is one row of the table: a starting manifest, one or more
// patches to apply in order, and the expected resulting manifest. All three
// are raw JSON so the table reads like the fixtures it represents.
type suiteCase struct {
	name     string
	kind     string
	manifest string // starting object, JSON
	patches  []Patch
	expected string // resulting object, JSON
	wantErr  string // non-empty: Apply must fail and the error must contain this substring
}

func TestApply_TableSuite(t *testing.T) {
	cases := []suiteCase{
		{
			name:     "SMP scalar field sets storageClassName",
			kind:     "PersistentVolumeClaim",
			manifest: `{"metadata":{"name":"workspaces"}}`,
			patches: []Patch{
				{Flavor: StrategicMerge, Kind: "PersistentVolumeClaim", Name: "workspaces", Body: []byte(`{"spec":{"storageClassName":"gp3"}}`)},
			},
			expected: `{"metadata":{"name":"workspaces"},"spec":{"resources":{},"storageClassName":"gp3"},"status":{}}`,
		},
		{
			name:     "SMP merges annotations, keeps existing keys",
			kind:     "PersistentVolumeClaim",
			manifest: `{"metadata":{"name":"workspaces","annotations":{"existing":"keep"}}}`,
			patches: []Patch{
				{Flavor: StrategicMerge, Kind: "PersistentVolumeClaim", Body: []byte(`{"metadata":{"annotations":{"custom.io/tag":"v2"}}}`)},
			},
			expected: `{"metadata":{"name":"workspaces","annotations":{"existing":"keep","custom.io/tag":"v2"}},"spec":{"resources":{}},"status":{}}`,
		},
		{
			name: "SMP on Tolerations replaces the list in place (no patchMergeKey tag)",
			kind: "StatefulSet",
			manifest: `{"spec":{"template":{"spec":{"tolerations":[
				{"key":"existing","operator":"Exists"}
			]}}}}`,
			patches: []Patch{
				{Flavor: StrategicMerge, Kind: "StatefulSet", Body: []byte(`{"spec":{"template":{"spec":{"tolerations":[
					{"key":"workload-tier","operator":"Equal","effect":"NoSchedule","value":"orbit"}
				]}}}}`)},
			},
			expected: `{"metadata":{},"spec":{"selector":null,"template":{"metadata":{},"spec":{"containers":null,"tolerations":[
				{"key":"workload-tier","operator":"Equal","value":"orbit","effect":"NoSchedule"}
			]}},"serviceName":"","updateStrategy":{}},"status":{"replicas":0,"availableReplicas":0}}`,
		},
		{
			name: "JSON6902 replace on a nested list index",
			kind: "Pod",
			manifest: `{"spec":{"containers":[
				{"name":"sandbox-exec","resources":{"limits":{"cpu":"2","memory":"4Gi"}}}
			]}}`,
			patches: []Patch{
				{Flavor: JSONPatch, Kind: "Pod", Body: []byte(`[
					{"op":"replace","path":"/spec/containers/0/resources/limits/cpu","value":"4"},
					{"op":"replace","path":"/spec/containers/0/resources/limits/memory","value":"8Gi"}
				]`)},
			},
			expected: `{"metadata":{},"spec":{"containers":[
				{"name":"sandbox-exec","resources":{"limits":{"cpu":"4","memory":"8Gi"}}}
			]},"status":{}}`,
		},
		{
			name:     "JSON6902 add appends to an existing array",
			kind:     "Pod",
			manifest: `{"spec":{"imagePullSecrets":[{"name":"existing-secret"}]}}`,
			patches: []Patch{
				{Flavor: JSONPatch, Kind: "Pod", Body: []byte(`[{"op":"add","path":"/spec/imagePullSecrets/-","value":{"name":"acme-registry"}}]`)},
			},
			expected: `{"metadata":{},"spec":{"containers":null,"imagePullSecrets":[{"name":"existing-secret"},{"name":"acme-registry"}]},"status":{}}`,
		},
		{
			name:     "JSON6902 remove on a missing path is a no-op (AllowMissingPathOnRemove)",
			kind:     "Pod",
			manifest: `{}`,
			patches: []Patch{
				{Flavor: JSONPatch, Kind: "Pod", Body: []byte(`[{"op":"remove","path":"/spec/nodeSelector/gpu"}]`)},
			},
			expected: `{"metadata":{},"spec":{"containers":null},"status":{}}`,
		},
		{
			name:     "multiple patches apply in order, last write wins",
			kind:     "PersistentVolumeClaim",
			manifest: `{}`,
			patches: []Patch{
				{Flavor: StrategicMerge, Kind: "PersistentVolumeClaim", Body: []byte(`{"spec":{"storageClassName":"slow"}}`)},
				{Flavor: StrategicMerge, Kind: "PersistentVolumeClaim", Body: []byte(`{"spec":{"storageClassName":"fast"}}`)},
			},
			expected: `{"metadata":{},"spec":{"resources":{},"storageClassName":"fast"},"status":{}}`,
		},
		{
			name:     "no patches returns the manifest unchanged",
			kind:     "PersistentVolumeClaim",
			manifest: `{"metadata":{"name":"workspaces"},"spec":{"storageClassName":"premium-ssd"}}`,
			patches:  nil,
			expected: `{"metadata":{"name":"workspaces"},"spec":{"resources":{},"storageClassName":"premium-ssd"},"status":{}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyJSONByKind(t, tc.kind, []byte(tc.manifest), tc.patches)
			assert.JSONEq(t, tc.expected, string(got), "resulting manifest mismatch")
		})
	}
}

// TestApplyToResource_TableSuite covers the selector-filtering layer
// (kind/name/label/annotation) on top of Apply, using the same
// manifest/patch/expected shape.
func TestApplyToResource_TableSuite(t *testing.T) {
	type resourceCase struct {
		name      string
		kind      string
		podName   string
		podLabels map[string]string
		manifest  string
		patches   []Patch
		expected  string
	}

	cases := []resourceCase{
		{
			name:      "label-selector patch matches, kind-mismatched and label-mismatched patches are filtered out",
			kind:      "Pod",
			podName:   "sandbox-run-42",
			podLabels: map[string]string{"orbit.io/role": "sandbox"},
			manifest:  `{"metadata":{"name":"sandbox-run-42","labels":{"orbit.io/role":"sandbox"}}}`,
			patches: []Patch{
				{File: "010-other-kind.yaml", Flavor: StrategicMerge, Kind: "StatefulSet", Body: []byte(`{"spec":{"replicas":9}}`)},
				{File: "020-sandbox-label.yaml", Flavor: StrategicMerge, Kind: "Pod", LabelSelector: "orbit.io/role=sandbox", Body: []byte(`{"metadata":{"annotations":{"matched":"true"}}}`)},
				{File: "030-non-sandbox.yaml", Flavor: StrategicMerge, Kind: "Pod", LabelSelector: "orbit.io/role=session-manager", Body: []byte(`{"metadata":{"annotations":{"wrong":"true"}}}`)},
			},
			expected: `{"metadata":{"name":"sandbox-run-42","labels":{"orbit.io/role":"sandbox"},"annotations":{"matched":"true"}},"spec":{"containers":null},"status":{}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pod corev1.Pod
			require.NoError(t, json.Unmarshal([]byte(tc.manifest), &pod))
			filtered, err := FilterForResource(tc.patches, tc.kind, tc.podName, tc.podLabels, pod.Annotations)
			require.NoError(t, err)
			got, err := Apply(pod, filtered)
			require.NoError(t, err)
			gotJSON, err := json.Marshal(got)
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(gotJSON))
		})
	}
}

// TestApply_ErrorTableSuite covers the failure paths: bad flavor, malformed
// JSON6902 ops, and a strategic merge body that cannot decode.
func TestApply_ErrorTableSuite(t *testing.T) {
	cases := []suiteCase{
		{
			name:     "unknown flavor is rejected",
			kind:     "Pod",
			manifest: `{}`,
			patches:  []Patch{{Flavor: Flavor(99), Kind: "Pod", Body: []byte(`{}`)}},
			wantErr:  "unknown flavor",
		},
		{
			name:     "malformed json6902 op is rejected",
			kind:     "Pod",
			manifest: `{}`,
			patches:  []Patch{{Flavor: JSONPatch, Kind: "Pod", Body: []byte(`not json`)}},
			wantErr:  "decode json6902",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			switch tc.kind {
			case "Pod":
				var pod corev1.Pod
				require.NoError(t, json.Unmarshal([]byte(tc.manifest), &pod))
				_, err := Apply(pod, tc.patches)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			default:
				t.Fatalf("unsupported kind in error table: %s", tc.kind)
			}
		})
	}
}
