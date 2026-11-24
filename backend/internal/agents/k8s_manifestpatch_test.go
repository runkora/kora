package agents

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
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
// added after an agent type was provisioned must reach the existing tenant
// pool Deployment and the agent's Service.
func TestEnsure_ExistingResourcesPickUpPatches(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	const ns = "kora-tenant-acme"
	existingPool := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: poolName, Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: sessionManagerContainerName, Image: "old"}},
				},
			},
		},
	}
	existingSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns},
		Spec:       corev1.ServiceSpec{ClusterIP: "None", Ports: []corev1.ServicePort{{Name: "http", Port: sessionManagerPort}}},
	}

	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(existingPool, existingSvc)
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
		"30-svc.yaml": `target: {kind: Service}
patch: |
  - {op: add, path: /metadata/annotations, value: {team: agents}}
  - {op: add, path: /spec/ports/-, value: {name: metrics, port: 9090, protocol: TCP}}
`,
	})

	ctx := context.Background()
	labels := agentTypeLabels(at)
	if err := p.ensureTenantPool(ctx, at.TenantID, ns, metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureTenantPool: %v", err)
	}
	if err := p.ensureHeadlessService(ctx, at.Slug, ns, labels, metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureHeadlessService: %v", err)
	}

	pool, err := p.client.AppsV1().Deployments(ns).Get(ctx, poolName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if tol := pool.Spec.Template.Spec.Tolerations; len(tol) != 1 || tol[0].Key != "dedicated" {
		t.Errorf("pool tolerations: got %v", tol)
	}
	if img := pool.Spec.Template.Spec.Containers[0].Image; img != p.cfg.SessionManagerImage {
		t.Errorf("pool image: got %q, want the rendered %q", img, p.cfg.SessionManagerImage)
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
