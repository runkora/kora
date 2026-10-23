package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	agentsConfig "github.com/runkora/kora/backend/cmd/kora-agents/config"
	"github.com/runkora/kora/backend/internal/agents"
	"github.com/runkora/kora/backend/internal/platform/brand"
)

func writePatch(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLoadPatches_RejectsProtectedField checks startup validation stops on a
// patch that changes a protected field, naming the file.
func TestLoadPatches_RejectsProtectedField(t *testing.T) {
	dir := writePatch(t, "90-bad.yaml", `target: {kind: StatefulSet}
patch: |
  - {op: replace, path: /spec/template/spec/serviceAccountName, value: default}
`)
	_, _, err := loadProvisioning(testConfig(), dir, brand.MustNew("kora"))
	if err == nil || !strings.Contains(err.Error(), "90-bad.yaml") || !strings.Contains(err.Error(), "spec.template.spec.serviceAccountName") {
		t.Fatalf("got %v, want a protected-field error naming 90-bad.yaml", err)
	}
}

// TestLoadWorkloads_RejectsIdentityLabel checks a pod label that would move the
// pod off its attested identity fails startup, naming the key.
func TestLoadWorkloads_RejectsIdentityLabel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workloads.yaml")
	body := `workloads:
  sandbox:
    podLabels:
      runkora.io/role: not-a-sandbox
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadWorkloads(path)
	if err == nil || !strings.Contains(err.Error(), "runkora.io/role") {
		t.Fatalf("got %v, want an error naming runkora.io/role", err)
	}
}

// TestLoadWorkloads_MissingFileIsNotAnError covers local development and any
// install that renders no customization.
func TestLoadWorkloads_MissingFileIsNotAnError(t *testing.T) {
	got, err := loadWorkloads(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("missing file should load empty: %v", err)
	}
	if got.Workloads.Sandbox.NodeSelector != nil || got.Workspace.StorageClassName != "" {
		t.Errorf("expected zero customization, got %+v", got)
	}
}

// TestLoadWorkloads_ParsesPlacementAndWorkspace pins the file shape the chart
// renders.
func TestLoadWorkloads_ParsesPlacementAndWorkspace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workloads.yaml")
	body := `workloads:
  common:
    podLabels:
      cost-centre: platform
  sandbox:
    nodeSelector:
      workload-tier: kora-sandbox
workspace:
  storageClassName: efs
  accessModes: [ReadWriteMany]
  size: 20Gi
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadWorkloads(path)
	if err != nil {
		t.Fatal(err)
	}
	sandbox := got.Workloads.ForSandbox()
	if sandbox.NodeSelector["workload-tier"] != "kora-sandbox" {
		t.Errorf("sandbox nodeSelector: got %v", sandbox.NodeSelector)
	}
	if sandbox.PodLabels["cost-centre"] != "platform" {
		t.Errorf("common podLabels should merge into the sandbox: got %v", sandbox.PodLabels)
	}
	if got.Workspace.Size == nil || got.Workspace.Size.String() != "20Gi" {
		t.Errorf("workspace size: got %v", got.Workspace.Size)
	}
}

// TestLoadWorkloads_RejectsUnparseableSize checks a bad quantity fails at
// startup rather than when an agent is created.
func TestLoadWorkloads_RejectsUnparseableSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workloads.yaml")
	if err := os.WriteFile(path, []byte("workspace:\n  size: twenty-gigs\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadWorkloads(path); err == nil {
		t.Fatal("expected an error for an unparseable workspace size")
	}
}

// defaultChartWorkloads is what charts/kora renders with no values set. Every
// key is present and empty, which is the case most likely to leak an empty
// struct into a provisioned manifest.
const defaultChartWorkloads = `workloads:
  common:
    podAnnotations: {}
    podLabels: {}
  sandbox:
    affinity: {}
    nodeSelector: {}
    podAnnotations: {}
    podLabels: {}
    priorityClassName: ""
    tolerations: []
    topologySpreadConstraints: []
  sessionManager:
    affinity: {}
    nodeSelector: {}
    podAnnotations: {}
    podLabels: {}
    priorityClassName: ""
    serviceAccount:
      annotations: {}
    tolerations: []
    topologySpreadConstraints: []
workspace:
  accessModes:
  - ReadWriteOnce
  size: 10Gi
  storageClassName: ""
`

// A deployment that customized nothing must provision exactly what it did
// before these values existed, empty structs included.
func TestLoadWorkloads_ChartDefaultsChangeNoManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workloads.yaml")
	if err := os.WriteFile(path, []byte(defaultChartWorkloads), 0o600); err != nil {
		t.Fatal(err)
	}
	workloads, err := loadWorkloads(path)
	if err != nil {
		t.Fatal(err)
	}

	cfg := provisionerConfig(testConfig(), nil, workloads, brand.MustNew("kora"))
	at := agents.AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}
	objs, err := agents.RenderResources(cfg, at, "tenant-example", true)
	if err != nil {
		t.Fatal(err)
	}

	sa, ok := objs[0].(*corev1.ServiceAccount)
	if !ok {
		t.Fatalf("objs[0] not *corev1.ServiceAccount: %T", objs[0])
	}
	if sa.Annotations != nil {
		t.Errorf("ServiceAccount annotations: got %v, want none", sa.Annotations)
	}

	ss, ok := objs[1].(*appsv1.StatefulSet)
	if !ok {
		t.Fatalf("objs[1] not *appsv1.StatefulSet: %T", objs[1])
	}
	pod, ok := objs[5].(*corev1.Pod)
	if !ok {
		t.Fatalf("objs[5] not *corev1.Pod: %T", objs[5])
	}
	for name, spec := range map[string]corev1.PodSpec{"StatefulSet": ss.Spec.Template.Spec, "sandbox Pod": pod.Spec} {
		if spec.NodeSelector != nil {
			t.Errorf("%s nodeSelector: got %v, want none", name, spec.NodeSelector)
		}
		if spec.Tolerations != nil {
			t.Errorf("%s tolerations: got %v, want none", name, spec.Tolerations)
		}
		if spec.PriorityClassName != "" {
			t.Errorf("%s priorityClassName: got %q, want empty", name, spec.PriorityClassName)
		}
	}
	if ss.Spec.Template.Spec.Affinity != nil {
		t.Errorf("StatefulSet affinity: got %+v, want none", ss.Spec.Template.Spec.Affinity)
	}
	// The default workspace is ReadWriteOnce, so the sandbox is pinned to the
	// node holding it.
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAffinity == nil {
		t.Error("sandbox Pod: expected co-location with its session-manager")
	}

	claim := ss.Spec.VolumeClaimTemplates[0]
	if claim.Spec.StorageClassName != nil {
		t.Errorf("storageClassName: got %q, want the cluster default", *claim.Spec.StorageClassName)
	}
	if len(claim.Spec.AccessModes) != 1 || claim.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("accessModes: got %v", claim.Spec.AccessModes)
	}
	if got := claim.Spec.Resources.Requests[corev1.ResourceStorage]; got.String() != "10Gi" {
		t.Errorf("workspace size: got %s, want 10Gi", got.String())
	}
}

// TestLoadPatches_ValidPatchLoads checks a patch that applies cleanly passes
// startup validation.
func TestLoadPatches_ValidPatchLoads(t *testing.T) {
	dir := writePatch(t, "10-ok.yaml", `target: {kind: Pod, labelSelector: runkora.io/role=sandbox}
patch: |
  - {op: add, path: /spec/tolerations, value: [{key: dedicated, operator: Exists}]}
`)
	patches, _, err := loadProvisioning(testConfig(), dir, brand.MustNew("kora"))
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("got %d patches, want 1", len(patches))
	}
}

// testConfig is an empty config with the default environment size every sandbox render needs.
func testConfig() *agentsConfig.Config {
	cfg := &agentsConfig.Config{}
	cfg.Agents.Environments.DefaultSize = agentsConfig.EnvironmentSize{CPU: "2", Memory: "4Gi", Disk: "10Gi"}
	return cfg
}
