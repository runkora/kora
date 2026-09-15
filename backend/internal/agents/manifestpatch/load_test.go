package manifestpatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePatches(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoad_MissingDirIsNoPatches(t *testing.T) {
	patches, err := Load(filepath.Join(t.TempDir(), "absent"))
	if err != nil || patches != nil {
		t.Fatalf("got %v, %v; want nil, nil", patches, err)
	}
}

func TestLoad_FilenameOrderAndExtensions(t *testing.T) {
	entry := "target: {kind: Pod}\npatch: |\n  - {op: add, path: /metadata/annotations/a, value: b}\n"
	dir := writePatches(t, map[string]string{
		"20-b.yml":   entry,
		"10-a.yaml":  entry,
		"notes.txt":  "ignored",
		"30-c.json5": "ignored",
	})
	patches, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range patches {
		got = append(got, p.File)
	}
	if strings.Join(got, ",") != "10-a.yaml,20-b.yml" {
		t.Fatalf("files: got %v", got)
	}
}

func TestLoad_Rejects(t *testing.T) {
	cases := []struct {
		name, body, wantErr string
	}{
		{"missing target", "patch: |\n  - {op: remove, path: /spec/x}\n", "target.kind is required"},
		{"unknown kind", "target: {kind: Deployement}\npatch: |\n  - {op: remove, path: /spec/x}\n", `unknown target kind "Deployement"`},
		{"empty patch", "target: {kind: Pod}\npatch: \"\"\n", "patch is empty"},
		{"file path", "target: {kind: Pod}\npath: other.yaml\n", "path is not supported"},
		{"entry typo", "targt: {kind: Pod}\npatch: x\n", "unknown field"},
		{"bad label selector", "target: {kind: Pod, labelSelector: 'a in (b'}\npatch: x\n", "target.labelSelector"},
		{"bad annotation selector", "target: {kind: Pod, annotationSelector: 'a in (b'}\npatch: x\n", "target.annotationSelector"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writePatches(t, map[string]string{"01-bad.yaml": tc.body}))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), "01-bad.yaml") {
				t.Fatalf("got %v, want error naming 01-bad.yaml containing %q", err, tc.wantErr)
			}
		})
	}
}
