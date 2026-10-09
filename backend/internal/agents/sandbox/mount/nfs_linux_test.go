//go:build linux

package mount

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/clear-route/orbit/internal/agents/sandbox/folder"
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

// Resolve finds the run's directory in a CSI volume's mount subdirectory and in a plain volume directory,
// and passes over a scratch volume that holds a directory named for the run.
func TestResolve(t *testing.T) {
	kubelet := t.TempDir()
	csi := filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~csi", "pv-1", "mount")
	local := filepath.Join(kubelet, "pods", "ws2", "volumes", "kubernetes.io~local-volume", "pv-2")
	mkdirs(t, filepath.Join(csi, "run-1"), filepath.Join(local, "run-2"), filepath.Join(kubelet, "pods", "ws2", "volumes", "kubernetes.io~empty-dir", "tmp", "run-2"))
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

// Each client node has its own read-only root, and each folder is exported to its own client only.
func TestExportLines(t *testing.T) {
	got := exportLines("/exports", []export{
		{name: "b", client: "10.0.0.2", uid: 100, gid: 100},
		{name: "a", client: "10.0.0.3", uid: 100, gid: 101},
		{name: "c", client: "10.0.0.2", uid: 100, gid: 100},
	})
	want := []string{
		"/exports/10.0.0.2 10.0.0.2(ro,secure,no_subtree_check,all_squash,fsid=0)",
		"/exports/10.0.0.2/b 10.0.0.2(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=100,fsid=b)",
		"/exports/10.0.0.2/c 10.0.0.2(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=100,fsid=c)",
		"/exports/10.0.0.3 10.0.0.3(ro,secure,no_subtree_check,all_squash,fsid=0)",
		"/exports/10.0.0.3/a 10.0.0.3(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=101,fsid=a)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("exportLines =\n%v\nwant\n%v", got, want)
	}
	// No line grants a client anything under another client's root.
	for _, l := range got {
		dir, acl, _ := strings.Cut(l, " ")
		client := strings.Split(strings.TrimPrefix(dir, "/exports/"), "/")[0]
		if !strings.HasPrefix(acl, client+"(") || strings.Contains(acl, " ") {
			t.Errorf("line %q grants a client other than %s", l, client)
		}
	}
	if lines := exportLines("/exports", nil); lines != nil {
		t.Errorf("empty exportLines = %v, want nil", lines)
	}
}

// The client root and the mount point are searchable by the squashed user the NFS client acts as.
func TestMakeExportDir_SearchableByAll(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "10.0.0.2", "pod-uid")
	if err := makeExportDir(dst); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Dir(dst), dst} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o005 != 0o005 {
			t.Errorf("%s mode %v, want world read and search", d, fi.Mode().Perm())
		}
	}
}

// Every unmount resolves the parent of its target from a descriptor on the root
// without following symlinks. A run that swaps a path component for a symlink to
// a host directory, between the agent reading mountinfo and unmounting, gets a
// refusal: the agent never unmounts anything outside the volume.
func TestUnmountBeneath_RefusesSwappedSymlink(t *testing.T) {
	kubelet := t.TempDir()
	outside := t.TempDir()
	vol := filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~local-volume", "pv")
	mkdirs(t, filepath.Join(vol, "run-1", "sandbox"), filepath.Join(outside, "sandbox"))
	// The run directory the agent mounted on is replaced by a symlink to a host directory.
	if err := os.Rename(filepath.Join(vol, "run-1"), filepath.Join(vol, "run-1.old")); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(vol, "run-1"))

	err := unmountBeneath(kubelet, filepath.Join(vol, "run-1", "sandbox"), unix.MNT_DETACH)
	if !errors.Is(err, unix.ELOOP) {
		t.Errorf("unmountBeneath through a swapped symlink = %v, want ELOOP from openat2", err)
	}
	// The client's unmount resolves the same way.
	if err := (Client{KubeletDir: kubelet}).Unmount(filepath.Join(vol, "run-1", "sandbox")); !errors.Is(err, unix.ELOOP) {
		t.Errorf("Client.Unmount through a swapped symlink = %v, want ELOOP", err)
	}
}

// Only sandbox folder mounts below the given pod directories are copies to release, each with the mount it sits on.
func TestCopiesUnder(t *testing.T) {
	info := strings.Join([]string{
		"10 1 0:1 / /k/pods/sb-a/volumes/kubernetes.io~local-volume/pv rw shared:5 - ext4 /dev/vdb1 rw",
		"11 10 0:2 / /k/pods/sb-a/volumes/kubernetes.io~local-volume/pv/run-b/sandbox rw shared:9 - nfs4 10.0.0.1:/sb-b rw",
		"20 1 0:1 / /k/pods/ws/volumes/kubernetes.io~local-volume/pv rw shared:5 - ext4 /dev/vdb1 rw",
		"21 20 0:2 / /k/pods/ws/volumes/kubernetes.io~local-volume/pv/run-b/sandbox rw shared:9 - nfs4 10.0.0.1:/sb-b rw",
		"30 1 0:3 / /k/pods/sb-a/volumes/kubernetes.io~nfs/data rw - nfs4 nas:/data rw",
		// A lookalike the agent did not make: right place and name, a source not in its ledger.
		"31 10 0:4 / /k/pods/sb-a/volumes/kubernetes.io~local-volume/pv/run-c/sandbox rw - nfs4 10.0.0.1:/forged rw",
	}, "\n")
	entries, err := folder.ReadMountInfo(strings.NewReader(info))
	if err != nil {
		t.Fatal(err)
	}
	got := copiesUnder(entries, []string{"/k/pods/sb-a/"}, map[string]bool{"10.0.0.1:/sb-b": true})
	want := []propagated{{point: "/k/pods/sb-a/volumes/kubernetes.io~local-volume/pv/run-b/sandbox", parentPoint: "/k/pods/sb-a/volumes/kubernetes.io~local-volume/pv"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("copiesUnder = %+v, want %+v", got, want)
	}
}

// The report lands in the volume's report directory, is replaced by later writes, and is removed by an empty one.
// A report directory the run replaced with a symlink is refused.
func TestReport(t *testing.T) {
	kubelet := t.TempDir()
	vol := filepath.Join(kubelet, "pods", "ws", "volumes", "kubernetes.io~local-volume", "pv")
	mkdirs(t, vol)
	c := Client{KubeletDir: kubelet}
	for _, content := range []string{"uid-1\nfirst\n", "uid-2\nsecond\n"} {
		if err := c.Report(vol, "run-1", content); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Join(vol, folder.ReportDir, "run-1"))
	if err != nil || string(got) != "uid-2\nsecond\n" {
		t.Errorf("report = %q, %v", got, err)
	}
	if err := c.Report(vol, "run-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vol, folder.ReportDir, "run-1")); !os.IsNotExist(err) {
		t.Errorf("report still present: %v", err)
	}

	outside := t.TempDir()
	vol2 := filepath.Join(kubelet, "pods", "ws2", "volumes", "kubernetes.io~local-volume", "pv")
	mkdirs(t, vol2)
	symlink(t, outside, filepath.Join(vol2, folder.ReportDir))
	if err := c.Report(vol2, "run-1", "uid\nx\n"); err == nil {
		t.Error("Report wrote through a symlinked report directory")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("files written outside: %v", entries)
	}
}
