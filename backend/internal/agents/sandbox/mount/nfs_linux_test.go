//go:build linux

package mount

import (
	"os"
	"path/filepath"
	"testing"
)

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// A path component the agent can write and has replaced with a symlink is refused,
// so a mount or an export cannot land on, or read from, a host path outside the volume.
func TestOpenBeneath_RefusesPlantedSymlinks(t *testing.T) {
	kubelet := t.TempDir()
	outside := t.TempDir()
	vol := filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~local-volume", "pv")
	mkdirs(t, filepath.Join(vol, "run-1", "sandbox"), filepath.Join(vol, "run-2"), filepath.Join(outside, "sandbox"))

	fd, err := openBeneath(kubelet, filepath.Join(vol, "run-1", "sandbox"))
	if err != nil {
		t.Fatalf("openBeneath on a real directory: %v", err)
	}
	_ = os.NewFile(uintptr(fd), "").Close()

	// The run replaced its sandbox folder with a symlink to a host directory.
	symlink(t, "/etc", filepath.Join(vol, "run-2", "sandbox"))
	// A run directory that is itself a symlink to somewhere else.
	symlink(t, outside, filepath.Join(vol, "run-3"))
	for _, path := range []string{
		filepath.Join(vol, "run-2", "sandbox"),
		filepath.Join(vol, "run-3", "sandbox"),
		filepath.Join(kubelet, "..", filepath.Base(outside)),
		outside,
	} {
		if fd, err := openBeneath(kubelet, path); err == nil {
			_ = os.NewFile(uintptr(fd), "").Close()
			t.Errorf("openBeneath(%q) succeeded, want refusal", path)
		}
	}
}

// Resolve finds the run's directory in a CSI volume's mount subdirectory and in a plain volume directory.
func TestResolve(t *testing.T) {
	kubelet := t.TempDir()
	csi := filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~csi", "pv-1", "mount")
	local := filepath.Join(kubelet, "pods", "ws2", "volumes", "kubernetes.io~local-volume", "pv-2")
	mkdirs(t, filepath.Join(csi, "run-1"), filepath.Join(local, "run-2"), filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~empty-dir", "tmp"))
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

// A volume whose contents plant a "mount" symlink, or a run directory that is a symlink, does not resolve.
func TestResolve_RefusesPlantedSymlinks(t *testing.T) {
	kubelet := t.TempDir()
	outside := t.TempDir()
	vol := filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~local-volume", "pv")
	mkdirs(t, vol, filepath.Join(outside, "run-1"), filepath.Join(outside, "run-2"))
	symlink(t, outside, filepath.Join(vol, "mount"))
	symlink(t, filepath.Join(outside, "run-2"), filepath.Join(vol, "run-2"))
	c := Client{KubeletDir: kubelet}
	for _, run := range []string{"run-1", "run-2"} {
		if got, err := c.Resolve("ws", run); err == nil {
			t.Errorf("Resolve(ws, %s) = %q, want refusal", run, got)
		}
	}
}
