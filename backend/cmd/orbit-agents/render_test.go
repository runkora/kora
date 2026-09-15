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
	_, err := loadPatches(&agentsConfig.Config{}, dir)
	if err == nil || !strings.Contains(err.Error(), "90-bad.yaml") || !strings.Contains(err.Error(), "spec.template.spec.serviceAccountName") {
		t.Fatalf("got %v, want a protected-field error naming 90-bad.yaml", err)
	}
}

// TestLoadPatches_ValidPatchLoads checks a patch that applies cleanly passes
// startup validation.
func TestLoadPatches_ValidPatchLoads(t *testing.T) {
	dir := writePatch(t, "10-ok.yaml", `target: {kind: Pod, labelSelector: orbit.io/role=sandbox}
patch: |
  - {op: add, path: /spec/tolerations, value: [{key: dedicated, operator: Exists}]}
`)
	patches, err := loadPatches(&agentsConfig.Config{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("got %d patches, want 1", len(patches))
	}
}
