package manifestpatch

import (
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/yaml"
)

// patch parses one patch entry from YAML, as Load would.
func patch(t *testing.T, file, entryYAML string) Patch {
	t.Helper()
	var e types.Patch
	if err := yaml.UnmarshalStrict([]byte(entryYAML), &e); err != nil {
		t.Fatal(err)
	}
	return Patch{File: file, Entry: e}
}

func statefulSet() appsv1.StatefulSet {
	nonRoot := true
	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "coder", Namespace: "tenant-example", Labels: map[string]string{"app": "coder", "orbit.io/tenant": "example"}},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "coder",
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": "coder"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "coder", "spiffe.io/agent": "coder"}},
				Spec: corev1.PodSpec{
					ServiceAccountName:           "coder",
					AutomountServiceAccountToken: new(false),
					EnableServiceLinks:           new(false),
					SecurityContext:              &corev1.PodSecurityContext{RunAsNonRoot: &nonRoot},
					Containers: []corev1.Container{
						{Name: "session-manager", Image: "sm:1", SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: new(false)}},
						{Name: "helper", Image: "h:1"},
					},
				},
			},
		},
	}
}

func sandboxPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox-r1", Namespace: "tenant-example", Labels: map[string]string{"orbit.io/role": "sandbox", "orbit.io/run-id": "r1"}},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: new(false),
			EnableServiceLinks:           new(false),
			Volumes:                      []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "workspaces-coder-0"}}}},
			Containers: []corev1.Container{{
				Name:         "sandbox",
				Image:        "sb:1",
				VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces/r1", SubPath: "r1"}},
			}},
		},
	}
}

func TestApply_NoMatchingPatchReturnsObjectUnchanged(t *testing.T) {
	ss := statefulSet()
	got, err := Apply(ss, "StatefulSet", []Patch{patch(t, "10.yaml", "target: {kind: Pod}\npatch: |\n  - {op: add, path: /metadata/annotations, value: {a: b}}\n")})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, ss) {
		t.Fatalf("object changed without a matching patch:\n%+v", got)
	}
}

func TestApply(t *testing.T) {
	cases := []struct {
		name    string
		patches []string
		check   func(t *testing.T, got appsv1.StatefulSet)
	}{
		{
			name: "strategic merge adds a node selector",
			patches: []string{`target: {kind: StatefulSet}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      spec:
        nodeSelector: {disktype: ssd}
`},
			check: func(t *testing.T, got appsv1.StatefulSet) {
				if got.Spec.Template.Spec.NodeSelector["disktype"] != "ssd" {
					t.Errorf("nodeSelector: got %v", got.Spec.Template.Spec.NodeSelector)
				}
			},
		},
		{
			name: "json6902 with a matching label selector",
			patches: []string{`target: {kind: StatefulSet, labelSelector: orbit.io/tenant=example}
patch: |
  - op: add
    path: /spec/template/spec/containers/0/resources
    value: {limits: {cpu: "2"}}
`},
			check: func(t *testing.T, got appsv1.StatefulSet) {
				if got.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String() != "2" {
					t.Errorf("resources: got %v", got.Spec.Template.Spec.Containers[0].Resources)
				}
			},
		},
		{
			name: "non-matching label selector is a no-op",
			patches: []string{`target: {kind: StatefulSet, labelSelector: orbit.io/tenant=other}
patch: |
  - {op: add, path: /spec/template/spec/nodeSelector, value: {disktype: ssd}}
`},
			check: func(t *testing.T, got appsv1.StatefulSet) {
				if got.Spec.Template.Spec.NodeSelector != nil {
					t.Errorf("nodeSelector: got %v, want none", got.Spec.Template.Spec.NodeSelector)
				}
			},
		},
		{
			name: "strategic merge delete directive removes a container",
			patches: []string{`target: {kind: StatefulSet}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      spec:
        containers:
        - name: helper
          $patch: delete
`},
			check: func(t *testing.T, got appsv1.StatefulSet) {
				if len(got.Spec.Template.Spec.Containers) != 1 || got.Spec.Template.Spec.Containers[0].Name != "session-manager" {
					t.Errorf("containers: got %+v", got.Spec.Template.Spec.Containers)
				}
			},
		},
		{
			name: "a later patch sees a label an earlier patch added",
			patches: []string{
				"target: {kind: StatefulSet}\npatch: |\n  - {op: add, path: /metadata/labels/tier, value: gold}\n",
				"target: {kind: StatefulSet, labelSelector: tier=gold}\npatch: |\n  - {op: add, path: /spec/template/spec/priorityClassName, value: high}\n",
			},
			check: func(t *testing.T, got appsv1.StatefulSet) {
				if got.Spec.Template.Spec.PriorityClassName != "high" {
					t.Errorf("priorityClassName: got %q", got.Spec.Template.Spec.PriorityClassName)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var patches []Patch
			for _, p := range tc.patches {
				patches = append(patches, patch(t, "10.yaml", p))
			}
			got, err := Apply(statefulSet(), "StatefulSet", patches)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, got)
		})
	}
}

func TestApply_RejectsFieldTypo(t *testing.T) {
	p := patch(t, "20-typo.yaml", `target: {kind: StatefulSet}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      spec:
        nodeSeletor: {disktype: ssd}
`)
	_, err := Apply(statefulSet(), "StatefulSet", []Patch{p})
	if err == nil || !strings.Contains(err.Error(), "20-typo.yaml") || !strings.Contains(err.Error(), "nodeSeletor") {
		t.Fatalf("got %v, want a strict-decode error naming the file and field", err)
	}
}

func TestApply_RejectsProtectedFieldChanges(t *testing.T) {
	cases := []struct{ name, kind, ops, wantPath string }{
		{"rename", "StatefulSet", "- {op: replace, path: /metadata/name, value: evil}", "metadata.name"},
		{"remove all labels", "StatefulSet", "- {op: remove, path: /metadata/labels}", "metadata.labels.orbit.io/tenant"},
		{"change pod identity label", "StatefulSet", "- {op: replace, path: /spec/template/metadata/labels/spiffe.io~1agent, value: other}", "spec.template.metadata.labels.spiffe.io/agent"},
		{"change service account", "StatefulSet", "- {op: replace, path: /spec/template/spec/serviceAccountName, value: default}", "spec.template.spec.serviceAccountName"},
		{"mount the service account token", "StatefulSet", "- {op: replace, path: /spec/template/spec/automountServiceAccountToken, value: true}", "spec.template.spec.automountServiceAccountToken"},
		{"enable service links", "StatefulSet", "- {op: remove, path: /spec/template/spec/enableServiceLinks}", "spec.template.spec.enableServiceLinks"},
		{"mount the sandbox service account token", "Pod", "- {op: replace, path: /spec/automountServiceAccountToken, value: true}", "spec.automountServiceAccountToken"},
		{"enable sandbox service links", "Pod", "- {op: replace, path: /spec/enableServiceLinks, value: true}", "spec.enableServiceLinks"},
		{"drop pod security context", "StatefulSet", "- {op: remove, path: /spec/template/spec/securityContext}", "spec.template.spec.securityContext.runAsNonRoot"},
		{"weaken container security context", "StatefulSet", "- {op: replace, path: /spec/template/spec/containers/0/securityContext/allowPrivilegeEscalation, value: true}", "spec.template.spec.containers.*.securityContext"},
		{"change selector", "StatefulSet", "- {op: replace, path: /spec/selector/matchLabels/app, value: x}", "spec.selector"},
		{"drop workspace subPath", "Pod", "- {op: remove, path: /spec/containers/0/volumeMounts/0/subPath}", "spec.containers.*.volumeMounts"},
		{"add an unscoped workspace mount", "Pod", "- {op: add, path: /spec/containers/0/volumeMounts/-, value: {name: workspaces, mountPath: /all}}", "spec.containers.*.volumeMounts"},
		{"point at another claim", "Pod", "- {op: replace, path: /spec/volumes/0/persistentVolumeClaim/claimName, value: other}", "spec.volumes.*.persistentVolumeClaim.claimName"},
		{"init container mounting the workspace", "Pod", "- {op: add, path: /spec/initContainers, value: [{name: x, image: x, volumeMounts: [{name: workspaces, mountPath: /w}]}]}", "spec.initContainers.*.volumeMounts"},
		{"change run id label", "Pod", "- {op: replace, path: /metadata/labels/orbit.io~1run-id, value: r2}", "metadata.labels.orbit.io/run-id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			patches := []Patch{patch(t, "30-bad.yaml", "target: {kind: "+tc.kind+"}\npatch: |\n  "+tc.ops+"\n")}
			var err error
			switch tc.kind {
			case "StatefulSet":
				_, err = Apply(statefulSet(), tc.kind, patches)
			case "Pod":
				_, err = Apply(sandboxPod(), tc.kind, patches)
			}
			if err == nil || !strings.Contains(err.Error(), "30-bad.yaml") || !strings.Contains(err.Error(), tc.wantPath) {
				t.Fatalf("got %v, want protected-field error on %s", err, tc.wantPath)
			}
		})
	}
}

func TestApply_AllowsUnprotectedPodChanges(t *testing.T) {
	p := patch(t, "40-ok.yaml", `target: {kind: Pod, labelSelector: orbit.io/role=sandbox}
patch: |
  - op: add
    path: /spec/volumes/-
    value: {name: extra, emptyDir: {}}
  - op: add
    path: /spec/tolerations
    value: [{key: dedicated, operator: Exists}]
`)
	got, err := Apply(sandboxPod(), "Pod", []Patch{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spec.Volumes) != 2 || len(got.Spec.Tolerations) != 1 {
		t.Fatalf("got volumes %v tolerations %v", got.Spec.Volumes, got.Spec.Tolerations)
	}
}
