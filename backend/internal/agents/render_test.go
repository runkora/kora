package agents

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TestRenderResources_ParityWithBuilders is a smoke test that pins the shape
// of the rendered manifests to what the runtime `ensure*` code builds. If a
// runtime builder gains a field (e.g. Service.TargetPort), this test fails
// unless RenderResources is updated to match.
func TestRenderResources_ParityWithBuilders(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SessionManagerImage: "orbit-session-manager:render",
		SandboxImage:        "orbit-sandbox-exec:render",
		SandboxGRPCPort:     50051,
		SystemNamespace:     "orbit-system",
	}
	at := AgentType{ID: "render-coder", Slug: "coder", TenantID: "example"}
	objs, err := RenderResources(cfg, at, "tenant-example", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 6 {
		t.Fatalf("expected 6 objects (SA, STS, Service, HPA, PDB, sandbox Pod), got %d", len(objs))
	}

	// Service must include TargetPort (matches ensureHeadlessService).
	svc, ok := objs[2].(*corev1.Service)
	if !ok {
		t.Fatalf("objs[2] not *corev1.Service: %T", objs[2])
	}
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("Service.Ports: got %d, want 1", len(svc.Spec.Ports))
	}
	port := svc.Spec.Ports[0]
	if port.Name != "http" || port.Port != sessionManagerPort {
		t.Errorf("Service.Ports[0]: got %+v", port)
	}
	if port.TargetPort != intstr.FromString("http") {
		t.Errorf("Service.TargetPort: got %v, want IntOrString(\"http\")", port.TargetPort)
	}
	if svc.Spec.ClusterIP != "None" {
		t.Errorf("Service.ClusterIP: got %q, want None (headless)", svc.Spec.ClusterIP)
	}

	// Sandbox Pod name derived from run ID.
	pod, ok := objs[5].(*corev1.Pod)
	if !ok {
		t.Fatalf("objs[5] not *corev1.Pod: %T", objs[5])
	}
	if pod.Name != "sandbox-sample-run" {
		t.Errorf("sandbox Pod.Name: got %q, want %q", pod.Name, "sandbox-sample-run")
	}
	if pod.Labels["orbit.io/role"] != "sandbox" {
		t.Errorf("sandbox Pod missing orbit.io/role=sandbox label: %v", pod.Labels)
	}
}

// TestRenderResources_AppliesPatches checks the preview carries both patch
// flavours and leaves the sandbox identity labels in place.
func TestRenderResources_AppliesPatches(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SessionManagerImage: "orbit-session-manager:render",
		SandboxImage:        "orbit-sandbox-exec:render",
		SandboxGRPCPort:     50051,
		SystemNamespace:     "orbit-system",
		Patches: loadTestPatches(t, map[string]string{
			"10-sts.yaml": `target: {kind: StatefulSet}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      spec:
        nodeSelector: {disktype: ssd}
`,
			"20-sandbox.yaml": `target: {kind: Pod, labelSelector: orbit.io/role=sandbox}
patch: |
  - {op: add, path: /spec/tolerations, value: [{key: dedicated, operator: Exists}]}
`,
		}),
	}
	at := AgentType{ID: "render-coder", Slug: "coder", TenantID: "example"}
	objs, err := RenderResources(cfg, at, "tenant-example", true)
	if err != nil {
		t.Fatal(err)
	}

	ss, ok := objs[1].(*appsv1.StatefulSet)
	if !ok {
		t.Fatalf("objs[1] not *appsv1.StatefulSet: %T", objs[1])
	}
	if ss.Spec.Template.Spec.NodeSelector["disktype"] != "ssd" {
		t.Errorf("StatefulSet nodeSelector: got %v", ss.Spec.Template.Spec.NodeSelector)
	}

	pod, ok := objs[5].(*corev1.Pod)
	if !ok {
		t.Fatalf("objs[5] not *corev1.Pod: %T", objs[5])
	}
	if len(pod.Spec.Tolerations) != 1 || pod.Spec.Tolerations[0].Key != "dedicated" {
		t.Errorf("sandbox Pod tolerations: got %v", pod.Spec.Tolerations)
	}
	for k, want := range map[string]string{"orbit.io/role": "sandbox", "orbit.io/run-id": "sample-run", "orbit.io/agent-slug": "coder"} {
		if pod.Labels[k] != want {
			t.Errorf("sandbox Pod label %s: got %q, want %q", k, pod.Labels[k], want)
		}
	}
}
