package manifestpatch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_MissingDirIsZero(t *testing.T) {
	patches, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("expected nil err on missing dir, got %v", err)
	}
	if len(patches) != 0 {
		t.Fatalf("expected 0 patches, got %d", len(patches))
	}
}

func TestLoad_EmptyDirIsZero(t *testing.T) {
	patches, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 0 {
		t.Fatalf("expected 0 patches, got %d", len(patches))
	}
}

func TestLoad_LexOrderAndFiltersNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "010-pvc.yaml", `
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: workspaces
spec:
  storageClassName: gp3
`)
	writeFile(t, dir, "020-svc.yml", `
apiVersion: v1
kind: Service
metadata:
  name: coder
spec:
  clusterIP: None
`)
	writeFile(t, dir, "README.md", `not a patch`)
	writeFile(t, dir, "001-noise.txt", `also not`)

	patches, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("expected 2 patches, got %d", len(patches))
	}
	if patches[0].File != "010-pvc.yaml" || patches[1].File != "020-svc.yml" {
		t.Fatalf("wrong order: %+v", patches)
	}
	if patches[0].Kind != "PersistentVolumeClaim" || patches[0].Name != "workspaces" {
		t.Errorf("bad SMP head: %+v", patches[0])
	}
	if patches[0].Flavor != StrategicMerge {
		t.Errorf("expected SMP, got %v", patches[0].Flavor)
	}
}

func TestLoad_DetectsJSON6902(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "030-sandbox-resources.yaml", `
target:
  kind: Pod
  labelSelector: orbit.io/role=sandbox
patch:
  - op: replace
    path: /spec/containers/0/resources
    value:
      limits: { cpu: "4", memory: 8Gi }
`)
	patches, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(patches))
	}
	p := patches[0]
	if p.Flavor != JSONPatch {
		t.Errorf("flavor: got %v, want JSONPatch", p.Flavor)
	}
	if p.Kind != "Pod" {
		t.Errorf("kind: got %q, want Pod", p.Kind)
	}
	if p.LabelSelector != "orbit.io/role=sandbox" {
		t.Errorf("labelSelector: got %q", p.LabelSelector)
	}
}

func TestLoad_MalformedIsFatal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "010-bad.yaml", "::: not: valid yaml\n  ::")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error on malformed YAML, got nil")
	}
}

func TestLoad_MissingKindInSMPIsFatal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "010-no-kind.yaml", `
apiVersion: v1
metadata:
  name: foo
`)
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error on missing kind, got nil")
	}
}

func TestLoad_SMPTakesPrecedenceOverPatchKey(t *testing.T) {
	// A Kubernetes object that legitimately has a top-level `patch` field
	// (unusual but valid — e.g. a ConfigMap.data entry) must not be misdetected
	// as JSON6902. SMP head detection wins when apiVersion+kind are present.
	dir := t.TempDir()
	writeFile(t, dir, "010-configmap-with-patch-key.yaml", `
apiVersion: v1
kind: ConfigMap
metadata:
  name: foo
data:
  patch: some literal value
`)
	// ConfigMap isn't in KnownKinds for Validate, but Load must parse it as SMP.
	patches, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(patches))
	}
	if patches[0].Flavor != StrategicMerge || patches[0].Kind != "ConfigMap" {
		t.Errorf("expected SMP ConfigMap, got flavor=%v kind=%q", patches[0].Flavor, patches[0].Kind)
	}
}

func TestLoad_UnrecognisedShapeIsFatal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "010-random.yaml", `
some_random: content
without_kind_or_target: true
`)
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error on unrecognized shape, got nil")
	}
}

func TestLoad_JSON6902RequiresTargetKind(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "010-no-target-kind.yaml", `
target:
  name: coder
patch:
  - op: replace
    path: /spec/replicas
    value: 3
`)
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoad_JSON6902PatchMustBeArray(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "010-obj-patch.yaml", `
target:
  kind: Pod
patch:
  op: replace
`)
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error, got nil")
	}
}
