package agents

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/runkora/kora/backend/internal/agents/manifestpatch"
)

// loadTestPatches writes the given patch files to a temp dir and loads them
// the way kora-agents does at startup.
func loadTestPatches(t *testing.T, files map[string]string) []manifestpatch.Patch {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	patches, err := manifestpatch.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return patches
}

// TestEnsure_ExistingResourcesPickUpPatches covers the reconcile path: a patch
// added after an agent type was provisioned must reach its existing
// StatefulSet, HPA and Service.
func TestEnsure_ExistingResourcesPickUpPatches(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	const ns = "kora-tenant-acme"
	existingSS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns, Annotations: map[string]string{annotationImagePinned: "true"}},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: sessionManagerContainerName, Image: "pinned@sha256:abc"}},
				},
			},
		},
	}
	existingHPA := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns},
		Spec:       autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 10},
	}
	existingSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns},
		Spec:       corev1.ServiceSpec{ClusterIP: "None", Ports: []corev1.ServicePort{{Name: "http", Port: sessionManagerPort}}},
	}

	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(existingSS, existingHPA, existingSvc)
	p.cfg.Patches = loadTestPatches(t, map[string]string{
		"10-sts.yaml": `target: {kind: StatefulSet}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      spec:
        tolerations: [{key: dedicated, operator: Exists}]
`,
		"20-hpa.yaml": `target: {kind: HorizontalPodAutoscaler}
patch: |
  apiVersion: autoscaling/v2
  kind: HorizontalPodAutoscaler
  metadata: {name: any}
  spec:
    maxReplicas: 5
`,
		"30-svc.yaml": `target: {kind: Service}
patch: |
  - {op: add, path: /metadata/annotations, value: {team: agents}}
  - {op: add, path: /spec/ports/-, value: {name: metrics, port: 9090, protocol: TCP}}
`,
	})

	ctx := context.Background()
	labels := agentTypeLabels(at)
	if err := p.ensureStatefulSet(ctx, at, ns, labels, metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureStatefulSet: %v", err)
	}
	if err := p.ensureHPA(ctx, at.Slug, ns, labels, metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureHPA: %v", err)
	}
	if err := p.ensureHeadlessService(ctx, at.Slug, ns, labels, metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureHeadlessService: %v", err)
	}

	ss, err := p.client.AppsV1().StatefulSets(ns).Get(ctx, at.Slug, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if tol := ss.Spec.Template.Spec.Tolerations; len(tol) != 1 || tol[0].Key != "dedicated" {
		t.Errorf("StatefulSet tolerations: got %v", tol)
	}
	if img := ss.Spec.Template.Spec.Containers[0].Image; img != "pinned@sha256:abc" {
		t.Errorf("StatefulSet image: got %q, want the live pinned image kept", img)
	}

	hpa, err := p.client.AutoscalingV2().HorizontalPodAutoscalers(ns).Get(ctx, at.Slug, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if hpa.Spec.MaxReplicas != 5 {
		t.Errorf("HPA maxReplicas: got %d, want 5", hpa.Spec.MaxReplicas)
	}

	svc, err := p.client.CoreV1().Services(ns).Get(ctx, at.Slug, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Annotations["team"] != "agents" {
		t.Errorf("Service annotations: got %v", svc.Annotations)
	}
	if len(svc.Spec.Ports) != 2 || svc.Spec.Ports[1].Name != "metrics" {
		t.Errorf("Service ports: got %+v", svc.Spec.Ports)
	}
}
