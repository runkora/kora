//go:build linux

package mount

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const nfsdFS = "/proc/fs/nfsd"

// Server exports sandbox folders with the kernel NFS server.
type Server struct {
	cfg ServerConfig
	// last is the table written by the previous Apply. Nil until the first
	// Apply, which always writes, so exports left by an earlier agent are dropped.
	last    []string
	applied bool
}

// StartServer starts nfsd and mountd. mountd runs until ctx ends, and its exit is reported on the returned channel.
// nfsd threads belong to the node's kernel and keep serving across agent restarts.
func StartServer(ctx context.Context, cfg ServerConfig) (*Server, <-chan error, error) {
	if !mounted(nfsdFS) {
		// Mounting the nfsd filesystem loads the nfsd module through the kernel's autoloader.
		if err := unix.Mount("nfsd", nfsdFS, "nfsd", 0, ""); err != nil {
			return nil, nil, fmt.Errorf("mount nfsd filesystem: %w", err)
		}
	}
	threads, err := os.ReadFile(filepath.Join(nfsdFS, "threads"))
	if err != nil {
		return nil, nil, fmt.Errorf("read nfsd threads: %w", err)
	}
	if strings.TrimSpace(string(threads)) == "0" {
		for _, f := range []string{"nfsv4gracetime", "nfsv4leasetime"} {
			if err := os.WriteFile(filepath.Join(nfsdFS, f), []byte(strconv.Itoa(cfg.GraceSecs)), 0o600); err != nil {
				return nil, nil, fmt.Errorf("set %s: %w", f, err)
			}
		}
		if out, err := exec.CommandContext(ctx, "rpc.nfsd", "-N", "3", "-V", "4.2", "--no-udp", strconv.Itoa(cfg.Threads)).CombinedOutput(); err != nil {
			return nil, nil, fmt.Errorf("start nfsd: %w: %s", err, out)
		}
	}
	if !mounted(cfg.ExportRoot) {
		if err := os.MkdirAll(cfg.ExportRoot, 0o750); err != nil {
			return nil, nil, fmt.Errorf("create export root: %w", err)
		}
		if err := unix.Mount("kora-exports", cfg.ExportRoot, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "size=1m,mode=0755"); err != nil {
			return nil, nil, fmt.Errorf("mount export root: %w", err)
		}
	}
	s := &Server{cfg: cfg}
	mountd := exec.CommandContext(ctx, "rpc.mountd", "-F", "-N", "2", "-N", "3")
	mountd.Stdout, mountd.Stderr = os.Stdout, os.Stderr
	if err := mountd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start mountd: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- mountd.Wait() }()
	return s, done, nil
}

// Apply binds each sandbox folder under its client's root and makes the export table exactly the given set.
func (s *Server) Apply(ctx context.Context, exports []export) error {
	lines := exportLines(s.cfg.ExportRoot, exports)
	if s.applied && slices.Equal(lines, s.last) {
		return nil
	}
	var errs []error
	keep := make(map[string]bool, len(exports))
	var ok []export
	for _, e := range exports {
		dst := filepath.Join(s.cfg.ExportRoot, e.client, e.name)
		keep[dst] = true
		if err := bindBeneath(s.cfg.KubeletDir, e.path, dst); err != nil {
			errs = append(errs, fmt.Errorf("bind %s: %w", e.path, err))
			continue
		}
		ok = append(ok, e)
	}
	if err := s.write(ctx, ok); err != nil {
		return errors.Join(append(errs, err)...)
	}
	clients, _ := os.ReadDir(s.cfg.ExportRoot)
	for _, c := range clients {
		clientDir := filepath.Join(s.cfg.ExportRoot, c.Name())
		names, _ := os.ReadDir(clientDir)
		for _, n := range names {
			dst := filepath.Join(clientDir, n.Name())
			if keep[dst] {
				continue
			}
			if err := unmountBeneath(s.cfg.ExportRoot, dst, unix.MNT_DETACH); err != nil {
				errs = append(errs, fmt.Errorf("unbind %s: %w", dst, err))
				continue
			}
			_ = os.Remove(dst)
		}
		// Removes the client's root once nothing is exported to it. A non-empty directory stays.
		_ = os.Remove(clientDir)
	}
	if len(errs) == 0 {
		s.last, s.applied = lines, true
	}
	return errors.Join(errs...)
}

// write replaces the exports table and syncs the kernel's export cache with it.
func (s *Server) write(ctx context.Context, exports []export) error {
	content := strings.Join(exportLines(s.cfg.ExportRoot, exports), "\n") + "\n"
	if err := os.WriteFile(s.cfg.ExportsFile, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write exports: %w", err)
	}
	// -r syncs the kernel's export cache with the table and drops removed exports.
	if out, err := exec.CommandContext(ctx, "exportfs", "-ra").CombinedOutput(); err != nil {
		return fmt.Errorf("exportfs: %w: %s", err, out)
	}
	return nil
}

// bindBeneath bind-mounts src on dst. src is resolved beneath the kubelet
// directory without following any symlink, so a pod cannot point it elsewhere.
func bindBeneath(kubeletDir, src, dst string) error {
	if mounted(dst) {
		return nil
	}
	fd, err := openBeneath(kubeletDir, src)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if derr := makeExportDir(dst); derr != nil {
		return derr
	}
	target, err := openBeneath(filepath.Dir(filepath.Dir(dst)), dst)
	if err != nil {
		return err
	}
	defer unix.Close(target)
	return unix.Mount(threadFdPath(fd), threadFdPath(target), "", unix.MS_BIND, "")
}

// makeExportDir creates a bind mount point and its client root. Both are 0755: an
// NFS client walks them as the squashed user.
func makeExportDir(dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return os.Chmod(filepath.Dir(dst), 0o755)
}

// Mounts returns the sandbox folder mounts under the kubelet's pod directory.
func (c Client) Mounts() (map[string]string, error) {
	infos, err := mountInfo()
	if err != nil {
		return nil, err
	}
	pods := filepath.Join(c.KubeletDir, "pods") + "/"
	out := map[string]string{}
	for _, m := range infos {
		if strings.HasPrefix(m.point, pods) && filepath.Base(m.point) == Dir {
			out[m.point] = m.source
		}
	}
	return out, nil
}

// Resolve finds the volume of the session-manager pod that holds the run's directory.
// CSI volumes keep their files under a mount subdirectory. Each candidate is checked
// beneath the kubelet directory without following symlinks.
func (c Client) Resolve(workspaceUID, runID string) (string, error) {
	vols, err := filepath.Glob(filepath.Join(c.KubeletDir, "pods", workspaceUID, "volumes", "*", "*"))
	if err != nil {
		return "", err
	}
	for _, v := range vols {
		if !persistentPlugin(filepath.Base(filepath.Dir(v))) {
			continue
		}
		for _, dir := range []string{filepath.Join(v, "mount"), v} {
			if fd, err := openBeneath(c.KubeletDir, filepath.Join(dir, runID)); err == nil {
				_ = unix.Close(fd)
				return dir, nil
			}
		}
	}
	return "", fmt.Errorf("no volume of pod %s holds run %s", workspaceUID, runID)
}

// persistentPlugin reports a volume plugin directory that can hold the workspace
// claim. The pod's scratch and projected volumes are skipped, so a run directory
// planted in the pod's /tmp cannot take the mount.
func persistentPlugin(dir string) bool {
	switch dir {
	case "kubernetes.io~empty-dir", "kubernetes.io~projected", "kubernetes.io~configmap", "kubernetes.io~secret", "kubernetes.io~downward-api":
		return false
	}
	return true
}

// Mount mounts the NFS source on rel beneath volumeDir. The mount point is resolved
// beneath the kubelet directory without following symlinks, so the agent cannot redirect it.
func (c Client) Mount(source, volumeDir, rel string) error {
	fd, err := openBeneath(c.KubeletDir, filepath.Join(volumeDir, rel))
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	host := strings.Trim(source[:strings.LastIndex(source, ":")], "[]")
	// nosuid and nodev keep setuid files and device nodes in a sandbox folder inert in the session-manager.
	return unix.Mount(source, threadFdPath(fd), "nfs4", unix.MS_NOSUID|unix.MS_NODEV, c.Options+",addr="+host)
}

// Unmount force-detaches the mount. Readers blocked on a dead server get an error.
func (c Client) Unmount(path string) error {
	return unmountBeneath(c.KubeletDir, path, unix.MNT_FORCE|unix.MNT_DETACH)
}

// unmountBeneath detaches the mount at path, which must lie under root. The parent
// directory is opened from a descriptor on root with RESOLVE_BENEATH and
// RESOLVE_NO_SYMLINKS, and umount2 names the last component through that
// descriptor with UMOUNT_NOFOLLOW. No component of path is followed as a symlink,
// and umount2 does not revalidate the mount point, so a dead NFS server does not
// block it.
func unmountBeneath(root, path string, flags int) error {
	parent, err := openBeneath(root, filepath.Dir(path))
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	return ignoreGone(unix.Unmount(threadFdPath(parent)+"/"+filepath.Base(path), flags|unix.UMOUNT_NOFOLLOW))
}

// ignoreGone treats a mount that is already gone as unmounted.
func ignoreGone(err error) error {
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
		return nil
	}
	return err
}

// Release detaches the sandbox folder copies that the host propagated below the
// given pod directories, without touching the originals. Unmount propagation
// follows the parent mount's peers, so each parent is made private first. The
// work runs in the node's mount namespace, where kubelet sees the copies.
func (c Client) Release(podUIDs []string, sources map[string]bool) error {
	if len(podUIDs) == 0 {
		return nil
	}
	dirs := make([]string, 0, len(podUIDs))
	for _, uid := range podUIDs {
		dirs = append(dirs, filepath.Join(c.HostKubeletDir, "pods", uid)+"/")
	}
	return inHostMountNS(func() error {
		f, err := os.Open("/proc/thread-self/mountinfo")
		if err != nil {
			return err
		}
		var entries []mountEntry
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if e, ok := parseMountInfo(sc.Text()); ok {
				entries = append(entries, e)
			}
		}
		_ = f.Close()
		var errs []error
		for _, dir := range dirs {
			for _, p := range copiesUnder(entries, []string{dir}, sources) {
				if err := releaseCopy(c.HostKubeletDir, dir, p); err != nil {
					errs = append(errs, err)
				}
			}
		}

		return errors.Join(errs...)
	})
}

// releaseCopy makes the copy's parent private and detaches the copy. Both are
// opened beneath the kubelet directory without following symlinks and changed
// through their descriptors.
func releaseCopy(kubeletDir, podDir string, p propagated) error {
	// Only a mount inside the dying pod's own directory is made private.
	if !strings.HasPrefix(p.parentPoint, podDir) {
		return fmt.Errorf("copy %s sits on %s, outside %s", p.point, p.parentPoint, podDir)
	}
	parent, err := openBeneath(kubeletDir, p.parentPoint)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	if perr := unix.Mount("", threadFdPath(parent), "", unix.MS_PRIVATE, ""); perr != nil {
		return fmt.Errorf("make %s private: %w", p.parentPoint, perr)
	}
	return unmountBeneath(kubeletDir, p.point, unix.MNT_DETACH)
}

// threadFdPath is the magic link the kernel resolves to the directory fd refers to.
// It names the calling thread, which also holds in the node's mount namespace.
func threadFdPath(fd int) string { return "/proc/thread-self/fd/" + strconv.Itoa(fd) }

// inHostMountNS runs fn on an OS thread that has joined the mount namespace of
// the node's init process. The thread is never returned to the Go scheduler,
// so it ends with the goroutine and no other goroutine runs in that namespace.
func inHostMountNS(fn func() error) error {
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		// setns into a mount namespace needs a thread that does not share its filesystem attributes.
		if err := unix.Unshare(unix.CLONE_FS); err != nil {
			errc <- fmt.Errorf("unshare filesystem attributes: %w", err)
			return
		}
		fd, err := unix.Open("/proc/1/ns/mnt", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			errc <- fmt.Errorf("open node mount namespace: %w", err)
			return
		}
		defer unix.Close(fd)
		if err := unix.Setns(fd, unix.CLONE_NEWNS); err != nil {
			errc <- fmt.Errorf("join node mount namespace: %w", err)
			return
		}
		errc <- fn()
	}()
	return <-errc
}

// openBeneath opens path, which must lie under root, as an O_PATH directory. Every
// component below root is resolved without following symlinks and cannot escape root.
func openBeneath(root, path string) (int, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return -1, fmt.Errorf("%s is not beneath %s", path, root)
	}
	dir, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open %s: %w", root, err)
	}
	defer unix.Close(dir)
	fd, err := unix.Openat2(dir, rel, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return -1, fmt.Errorf("open %s beneath %s: %w", rel, root, err)
	}
	return fd, nil
}

func mountInfo() ([]mountEntry, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []mountEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m, ok := parseMountInfo(sc.Text()); ok {
			out = append(out, m)
		}
	}
	return out, sc.Err()
}

func mounted(path string) bool {
	infos, err := mountInfo()
	if err != nil {
		return false
	}
	return slices.ContainsFunc(infos, func(m mountEntry) bool { return m.point == path })
}

// exportLines renders the exports table. Each client node gets its own NFSv4
// root, read-only and holding only the folders exported to it, so a node never
// sees another node's exports. Each folder is exported to its session-manager's
// node only, with every client user mapped to the sandbox's user.
func exportLines(root string, exports []export) []string {
	var lines []string
	roots := map[string]bool{}
	for _, e := range exports {
		if !roots[e.client] {
			roots[e.client] = true
			lines = append(lines, fmt.Sprintf("%s/%s %s(ro,secure,no_subtree_check,all_squash,fsid=0)", root, e.client, e.client))
		}
		lines = append(lines, fmt.Sprintf("%s/%s/%s %s(rw,async,secure,no_subtree_check,all_squash,anonuid=%d,anongid=%d,fsid=%s)",
			root, e.client, e.name, e.client, e.uid, e.gid, e.name))
	}
	slices.Sort(lines)
	return lines
}

// propagated is a copy of a sandbox folder mount, and the mount it sits on.
type propagated struct{ point, parentPoint string }

// copiesUnder returns the sandbox folder mounts below the given pod directories,
// with the mounts they sit on. These are copies the host propagated into other
// binds of a session-manager's volume, such as a co-located sandbox pod's.
// Only copies of the given sources, which are the node agent's own mounts, count.
func copiesUnder(entries []mountEntry, podDirs []string, sources map[string]bool) []propagated {
	points := make(map[string]string, len(entries))
	for _, e := range entries {
		points[e.id] = e.point
	}
	var out []propagated
	for _, e := range entries {
		if e.fstype != "nfs4" || filepath.Base(e.point) != Dir || !sources[e.source] {
			continue
		}
		for _, dir := range podDirs {
			if strings.HasPrefix(e.point, dir) && points[e.parent] != "" {
				out = append(out, propagated{point: e.point, parentPoint: points[e.parent]})
				break
			}
		}
	}
	return out
}
