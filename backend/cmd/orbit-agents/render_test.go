package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentsConfig "github.com/clear-route/orbit/cmd/orbit-agents/config"
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
	_, _, err := loadProvisioning(&agentsConfig.Config{}, dir)
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
      orbit.io/role: not-a-sandbox
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadWorkloads(path)
	if err == nil || !strings.Contains(err.Error(), "orbit.io/role") {
		t.Fatalf("got %v, want an error naming orbit.io/role", err)
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
      workload-tier: orbit-sandbox
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
	if sandbox.NodeSelector["workload-tier"] != "orbit-sandbox" {
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

// TestLoadPatches_ValidPatchLoads checks a patch that applies cleanly passes
// startup validation.
func TestLoadPatches_ValidPatchLoads(t *testing.T) {
	dir := writePatch(t, "10-ok.yaml", `target: {kind: Pod, labelSelector: orbit.io/role=sandbox}
patch: |
  - {op: add, path: /spec/tolerations, value: [{key: dedicated, operator: Exists}]}
`)
	patches, _, err := loadProvisioning(&agentsConfig.Config{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("got %d patches, want 1", len(patches))
	}
}
