package manifestpatch

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApply_Empty(t *testing.T) {
	sc := "premium-ssd"
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "workspaces"},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sc,
		},
	}
	got, err := Apply(pvc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "workspaces" || *got.Spec.StorageClassName != "premium-ssd" {
		t.Errorf("expected round trip, got %+v", got)
	}
}

func TestApply_StrategicMerge_ScalarField(t *testing.T) {
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "workspaces"},
	}
	p := Patch{
		Flavor: StrategicMerge,
		Kind:   "PersistentVolumeClaim",
		Name:   "workspaces",
		Body:   []byte(`{"spec":{"storageClassName":"gp3"}}`),
	}
	got, err := Apply(pvc, []Patch{p})
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.StorageClassName == nil || *got.Spec.StorageClassName != "gp3" {
		t.Errorf("storageClassName = %v, want gp3", got.Spec.StorageClassName)
	}
}

func TestApply_StrategicMerge_MergesAnnotations(t *testing.T) {
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "workspaces",
			Annotations: map[string]string{"existing": "keep"},
		},
	}
	p := Patch{
		Flavor: StrategicMerge,
		Kind:   "PersistentVolumeClaim",
		Body:   []byte(`{"metadata":{"annotations":{"custom.io/tag":"v2"}}}`),
	}
	got, err := Apply(pvc, []Patch{p})
	if err != nil {
		t.Fatal(err)
	}
	if got.Annotations["existing"] != "keep" {
		t.Errorf("existing annotation lost: %v", got.Annotations)
	}
	if got.Annotations["custom.io/tag"] != "v2" {
		t.Errorf("new annotation missing: %v", got.Annotations)
	}
}

func TestApply_StrategicMerge_ReplacesTolerations(t *testing.T) {
	// Tolerations don't have a patchStrategy=merge tag on the Go type after JSON
	// round-tripping via strategicpatch on typed structs, so SMP behaves as a
	// replace-in-place here. This test pins that behaviour so operators know
	// what to expect (use JSON6902 for surgical list edits).
	ss := appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{Key: "existing", Operator: corev1.TolerationOpExists},
					},
				},
			},
		},
	}
	p := Patch{
		Flavor: StrategicMerge,
		Kind:   "StatefulSet",
		Body: []byte(`{"spec":{"template":{"spec":{"tolerations":[
			{"key":"workload-tier","operator":"Equal","effect":"NoSchedule","value":"orbit"}
		]}}}}`),
	}
	got, err := Apply(ss, []Patch{p})
	if err != nil {
		t.Fatal(err)
	}
	tols := got.Spec.Template.Spec.Tolerations
	if len(tols) != 1 || tols[0].Key != "workload-tier" {
		t.Errorf("expected replace-in-place, got %+v", tols)
	}
}

func TestApply_JSON6902_ReplaceListIndex(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "sandbox-exec",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
			},
		},
	}
	p := Patch{
		Flavor: JSONPatch,
		Kind:   "Pod",
		Body: []byte(`[
			{"op":"replace","path":"/spec/containers/0/resources/limits/cpu","value":"4"},
			{"op":"replace","path":"/spec/containers/0/resources/limits/memory","value":"8Gi"}
		]`),
	}
	got, err := Apply(pod, []Patch{p})
	if err != nil {
		t.Fatal(err)
	}
	cpu := got.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU]
	mem := got.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]
	if cpu.String() != "4" || mem.String() != "8Gi" {
		t.Errorf("resources: cpu=%s mem=%s", cpu.String(), mem.String())
	}
}

func TestApply_JSON6902_AddToArray(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{
				{Name: "existing-secret"},
			},
		},
	}
	p := Patch{
		Flavor: JSONPatch,
		Kind:   "Pod",
		Body:   []byte(`[{"op":"add","path":"/spec/imagePullSecrets/-","value":{"name":"acme-registry"}}]`),
	}
	got, err := Apply(pod, []Patch{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spec.ImagePullSecrets) != 2 {
		t.Fatalf("expected 2 pull secrets, got %d", len(got.Spec.ImagePullSecrets))
	}
	if got.Spec.ImagePullSecrets[1].Name != "acme-registry" {
		t.Errorf("second pull secret: %+v", got.Spec.ImagePullSecrets)
	}
}

func TestApply_JSON6902_RemoveMissingPathAllowed(t *testing.T) {
	pod := corev1.Pod{}
	p := Patch{
		Flavor: JSONPatch,
		Kind:   "Pod",
		Body:   []byte(`[{"op":"remove","path":"/spec/nodeSelector/gpu"}]`),
	}
	if _, err := Apply(pod, []Patch{p}); err != nil {
		t.Fatalf("expected AllowMissingPathOnRemove to swallow, got %v", err)
	}
}

func TestApply_OrderMattersLastWrites(t *testing.T) {
	pvc := corev1.PersistentVolumeClaim{}
	patches := []Patch{
		{Flavor: StrategicMerge, Kind: "PersistentVolumeClaim", Body: []byte(`{"spec":{"storageClassName":"slow"}}`)},
		{Flavor: StrategicMerge, Kind: "PersistentVolumeClaim", Body: []byte(`{"spec":{"storageClassName":"fast"}}`)},
	}
	got, err := Apply(pvc, patches)
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.StorageClassName == nil || *got.Spec.StorageClassName != "fast" {
		t.Errorf("expected last-write-wins fast, got %v", got.Spec.StorageClassName)
	}
}

func TestApplyToResource_FiltersByKindAndLabel(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "sandbox-run-42",
			Labels: map[string]string{"orbit.io/role": "sandbox"},
		},
	}
	patches := []Patch{
		{
			File:   "010-other-kind.yaml",
			Flavor: StrategicMerge,
			Kind:   "StatefulSet",
			Body:   []byte(`{"spec":{"replicas":9}}`),
		},
		{
			File:          "020-sandbox-label.yaml",
			Flavor:        StrategicMerge,
			Kind:          "Pod",
			LabelSelector: "orbit.io/role=sandbox",
			Body:          []byte(`{"metadata":{"annotations":{"matched":"true"}}}`),
		},
		{
			File:          "030-non-sandbox.yaml",
			Flavor:        StrategicMerge,
			Kind:          "Pod",
			LabelSelector: "orbit.io/role=session-manager",
			Body:          []byte(`{"metadata":{"annotations":{"wrong":"true"}}}`),
		},
	}
	got, err := ApplyToResource(pod, patches, "Pod", pod.Name, pod.Labels, pod.Annotations)
	if err != nil {
		t.Fatal(err)
	}
	if got.Annotations["matched"] != "true" {
		t.Errorf("expected label-matched patch applied, got %v", got.Annotations)
	}
	if _, bad := got.Annotations["wrong"]; bad {
		t.Errorf("non-matching label patch leaked: %v", got.Annotations)
	}
}
