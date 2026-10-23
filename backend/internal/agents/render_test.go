package agents

import (
	"slices"
	"strings"
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
		SessionManagerImage:    "kora-session-manager:render",
		SandboxImage:           "kora-sandbox-base:render",
		DefaultEnvironmentSize: testSizes.Default,
		SandboxGRPCPort:        50051,
		SystemNamespace:        "kora-system",
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
	if pod.Labels["runkora.io/role"] != "sandbox" {
		t.Errorf("sandbox Pod missing runkora.io/role=sandbox label: %v", pod.Labels)
	}
}

// TestRenderResources_AppliesPatches checks the preview carries both patch
// flavours and leaves the sandbox identity labels in place.
func TestRenderResources_AppliesPatches(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SessionManagerImage:    "kora-session-manager:render",
		SandboxImage:           "kora-sandbox-base:render",
		DefaultEnvironmentSize: testSizes.Default,
		SandboxGRPCPort:        50051,
		SystemNamespace:        "kora-system",
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
			"20-sandbox.yaml": `target: {kind: Pod, labelSelector: runkora.io/role=sandbox}
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
	for k, want := range map[string]string{"runkora.io/role": "sandbox", "runkora.io/run-id": "sample-run", "runkora.io/agent-slug": "coder"} {
		if pod.Labels[k] != want {
			t.Errorf("sandbox Pod label %s: got %q, want %q", k, pod.Labels[k], want)
		}
	}
}

// splitPoolPatches pins agents and sandboxes to separate node pools, the
// topology a dedicated sandbox pool needs. workspace is the PersistentVolumeClaim
// patch under test.
func splitPoolPatches(workspace string) map[string]string {
	return map[string]string{
		"10-agent-placement.yaml": `target: {kind: StatefulSet, labelSelector: runkora.io/agent-type}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      spec:
        nodeSelector: {workload-tier: orbit}
`,
		"11-sandbox-placement.yaml": `target: {kind: Pod, labelSelector: runkora.io/role=sandbox}
patch: |
  apiVersion: v1
  kind: Pod
  metadata: {name: any}
  spec:
    nodeSelector: {workload-tier: kora-sandbox}
`,
		"20-workspace.yaml": workspace,
	}
}

// A sandbox on its own node pool mounts the session-manager's workspace across
// nodes, so ReadWriteOnce cannot work. Catching it here turns a Multi-Attach
// error on the first run into a startup failure.
func TestRenderResources_SplitPoolRejectsReadWriteOnceWorkspace(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SessionManagerImage:    "kora-session-manager:render",
		SandboxImage:           "kora-sandbox-base:render",
		DefaultEnvironmentSize: testSizes.Default,
		SandboxGRPCPort:        50051,
		SystemNamespace:        "kora-system",
		Patches: loadTestPatches(t, splitPoolPatches(`target: {kind: PersistentVolumeClaim}
patch: |
  apiVersion: v1
  kind: PersistentVolumeClaim
  metadata: {name: any}
  spec:
    storageClassName: gp2
`)),
	}
	at := AgentType{ID: "render-coder", Slug: "coder", TenantID: "example"}

	_, err := RenderResources(cfg, at, "tenant-example", true)
	if err == nil {
		t.Fatal("expected an error for a split node pool with a ReadWriteOnce workspace")
	}
	if !strings.Contains(err.Error(), "ReadWriteMany") {
		t.Errorf("error should name the fix, got: %v", err)
	}
}

func TestRenderResources_SplitPoolAcceptsReadWriteManyWorkspace(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SessionManagerImage:    "kora-session-manager:render",
		SandboxImage:           "kora-sandbox-base:render",
		DefaultEnvironmentSize: testSizes.Default,
		SandboxGRPCPort:        50051,
		SystemNamespace:        "kora-system",
		Patches: loadTestPatches(t, splitPoolPatches(`target: {kind: PersistentVolumeClaim}
patch: |
  apiVersion: v1
  kind: PersistentVolumeClaim
  metadata: {name: any}
  spec:
    storageClassName: efs
    accessModes: [ReadWriteMany]
`)),
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
	if got := ss.Spec.Template.Spec.NodeSelector["workload-tier"]; got != "orbit" {
		t.Errorf("StatefulSet workload-tier: got %q, want orbit", got)
	}
	claim := ss.Spec.VolumeClaimTemplates[0]
	if !slices.Contains(claim.Spec.AccessModes, corev1.ReadWriteMany) {
		t.Errorf("workspace accessModes: got %v, want ReadWriteMany", claim.Spec.AccessModes)
	}

	pod, ok := objs[5].(*corev1.Pod)
	if !ok {
		t.Fatalf("objs[5] not *corev1.Pod: %T", objs[5])
	}
	if got := pod.Spec.NodeSelector["workload-tier"]; got != "kora-sandbox" {
		t.Errorf("sandbox Pod workload-tier: got %q, want kora-sandbox", got)
	}
}

// Required node affinity separates pools just as a nodeSelector does.
func TestRenderResources_SplitByNodeAffinityRejectsReadWriteOnceWorkspace(t *testing.T) {
	nodeAffinity := func(tier string) string {
		return `
  spec:
    affinity:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
            - matchExpressions:
                - {key: workload-tier, operator: In, values: [` + tier + `]}
`
	}
	cfg := K8sProvisionerConfig{
		SessionManagerImage:    "kora-session-manager:render",
		SandboxImage:           "kora-sandbox-base:render",
		DefaultEnvironmentSize: testSizes.Default,
		SandboxGRPCPort:        50051,
		SystemNamespace:        "kora-system",
		Patches: loadTestPatches(t, map[string]string{
			"10-agent.yaml": `target: {kind: StatefulSet}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:` + strings.ReplaceAll(nodeAffinity("orbit"), "\n  ", "\n      ") + `
`,
			"11-sandbox.yaml": `target: {kind: Pod, labelSelector: runkora.io/role=sandbox}
patch: |
  apiVersion: v1
  kind: Pod
  metadata: {name: any}` + nodeAffinity("kora-sandbox"),
		}),
	}
	at := AgentType{ID: "render-coder", Slug: "coder", TenantID: "example"}

	_, err := RenderResources(cfg, at, "tenant-example", true)
	if err == nil {
		t.Fatal("expected an error for pools split by required node affinity")
	}
	if !strings.Contains(err.Error(), "ReadWriteMany") {
		t.Errorf("error should name the fix, got: %v", err)
	}
}

// A patch selected by an agent type's own labels matches nothing when the
// startup validation renders its stand-in, so the same check runs per agent
// type at provisioning time.
func TestCheckSandboxPlacement_CatchesPatchesScopedToOneAgentType(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SessionManagerImage:    "kora-session-manager:render",
		SandboxImage:           "kora-sandbox-base:render",
		DefaultEnvironmentSize: testSizes.Default,
		SandboxGRPCPort:        50051,
		SystemNamespace:        "kora-system",
		Patches: loadTestPatches(t, map[string]string{
			"10-agent.yaml": `target: {kind: StatefulSet, labelSelector: "runkora.io/agent-type=render-coder"}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      spec:
        nodeSelector: {workload-tier: orbit}
`,
			"11-sandbox.yaml": `target: {kind: Pod, labelSelector: "runkora.io/agent-slug=coder"}
patch: |
  apiVersion: v1
  kind: Pod
  metadata: {name: any}
  spec:
    nodeSelector: {workload-tier: kora-sandbox}
`,
		}),
	}

	// The stand-in the startup validation renders carries neither label.
	standIn := AgentType{ID: "validate", Slug: "validate", TenantID: "validate"}
	if _, err := RenderResources(cfg, standIn, "tenant-validate", true); err != nil {
		t.Fatalf("scoped patches should be a no-op for the stand-in: %v", err)
	}

	p := &K8sProvisioner{cfg: cfg}
	at := AgentType{ID: "render-coder", Slug: "coder", TenantID: "example"}
	err := p.checkSandboxPlacement(at, "tenant-example")
	if err == nil {
		t.Fatal("expected an error for the agent type the patches select")
	}
	if !strings.Contains(err.Error(), "ReadWriteMany") {
		t.Errorf("error should name the fix, got: %v", err)
	}
}

// One side unconstrained is not conclusive: the scheduler may still co-locate
// the pods, so a ReadWriteOnce workspace stays valid.
func TestRenderResources_OneSidedPlacementKeepsReadWriteOnce(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SessionManagerImage:    "kora-session-manager:render",
		SandboxImage:           "kora-sandbox-base:render",
		DefaultEnvironmentSize: testSizes.Default,
		SandboxGRPCPort:        50051,
		SystemNamespace:        "kora-system",
		Patches: loadTestPatches(t, map[string]string{
			"10-sts.yaml": `target: {kind: StatefulSet}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      spec:
        nodeSelector: {workload-tier: orbit}
`,
		}),
	}
	at := AgentType{ID: "render-coder", Slug: "coder", TenantID: "example"}

	if _, err := RenderResources(cfg, at, "tenant-example", true); err != nil {
		t.Fatalf("unconstrained sandbox should not be rejected: %v", err)
	}
}
