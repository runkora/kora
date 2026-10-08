//go:build linux

package mount

import (
	"os"
	"path/filepath"
	"testing"
)

// A mount point the agent replaced with a symlink is refused, so a mount cannot land outside the workspace.
func TestOpenBeneath_RefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "run-1", "sandbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	fd, err := openBeneath(dir, "run-1/sandbox")
	if err != nil {
		t.Fatalf("openBeneath on a real directory: %v", err)
	}
	_ = os.NewFile(uintptr(fd), "").Close()

	if err := os.Symlink("/etc", filepath.Join(dir, "run-1", "link")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"run-1/link", "../" + filepath.Base(dir) + "/run-1/sandbox"} {
		if _, err := openBeneath(dir, rel); err == nil {
			t.Errorf("openBeneath(%q) succeeded, want refusal", rel)
		}
	}
}

// Resolve finds the run's directory in a CSI volume's mount subdirectory and in a plain volume directory.
func TestResolve(t *testing.T) {
	kubelet := t.TempDir()
	csi := filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~csi", "pv-1", "mount")
	local := filepath.Join(kubelet, "pods", "ws2", "volumes", "kubernetes.io~local-volume", "pv-2")
	for _, d := range []string{filepath.Join(csi, "run-1"), filepath.Join(local, "run-2"), filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~empty-dir", "tmp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c := Client{KubeletDir: kubelet}
	if got, err := c.Resolve("ws", "run-1"); err != nil || got != csi {
		t.Errorf("Resolve(ws, run-1) = %q, %v, want %q", got, err, csi)
	}
	if got, err := c.Resolve("ws2", "run-2"); err != nil || got != local {
		t.Errorf("Resolve(ws2, run-2) = %q, %v, want %q", got, err, local)
	}
	if _, err := c.Resolve("ws", "run-9"); err == nil {
		t.Error("Resolve found a run that has no directory")
	}
}
