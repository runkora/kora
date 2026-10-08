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
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const nfsdFS = "/proc/fs/nfsd"

// Server exports sandbox folders with the kernel NFS server.
type Server struct {
	cfg  ServerConfig
	last []string
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
			if err := os.WriteFile(filepath.Join(nfsdFS, f), []byte(strconv.Itoa(cfg.GraceSecs)), 0o644); err != nil {
				return nil, nil, fmt.Errorf("set %s: %w", f, err)
			}
		}
		if out, err := exec.CommandContext(ctx, "rpc.nfsd", "-N", "3", "-V", "4.2", "--no-udp", strconv.Itoa(cfg.Threads)).CombinedOutput(); err != nil {
			return nil, nil, fmt.Errorf("start nfsd: %w: %s", err, out)
		}
	}
	if !mounted(cfg.ExportRoot) {
		if err := os.MkdirAll(cfg.ExportRoot, 0o755); err != nil {
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

// Apply binds each sandbox folder under the export root and makes the export table exactly the given set.
func (s *Server) Apply(exports []export) error {
	lines := exportLines(s.cfg.ExportRoot, exports)
	if slices.Equal(lines, s.last) {
		return nil
	}
	var errs []error
	keep := make(map[string]bool, len(exports))
	var ok []export
	for _, e := range exports {
		dst := filepath.Join(s.cfg.ExportRoot, e.name)
		keep[dst] = true
		if err := bindBeneath(s.cfg.KubeletDir, e.path, dst); err != nil {
			errs = append(errs, fmt.Errorf("bind %s: %w", e.path, err))
			continue
		}
		ok = append(ok, e)
	}
	if err := s.write(ok); err != nil {
		return errors.Join(append(errs, err)...)
	}
	entries, _ := os.ReadDir(s.cfg.ExportRoot)
	for _, ent := range entries {
		dst := filepath.Join(s.cfg.ExportRoot, ent.Name())
		if keep[dst] {
			continue
		}
		if err := unix.Unmount(dst, unix.MNT_DETACH); err != nil && !errors.Is(err, unix.EINVAL) {
			errs = append(errs, fmt.Errorf("unbind %s: %w", dst, err))
			continue
		}
		_ = os.Remove(dst)
	}
	if len(errs) == 0 {
		s.last = lines
	}
	return errors.Join(errs...)
}

// write replaces the exports table and syncs the kernel's export cache with it.
func (s *Server) write(exports []export) error {
	content := strings.Join(exportLines(s.cfg.ExportRoot, exports), "\n") + "\n"
	if err := os.WriteFile(s.cfg.ExportsFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write exports: %w", err)
	}
	// -r syncs the kernel's export cache with the table and drops removed exports.
	if out, err := exec.Command("exportfs", "-ra").CombinedOutput(); err != nil {
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
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return unix.Mount(fdPath(fd), dst, "", unix.MS_BIND, "")
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
		if m.fstype == "nfs4" && strings.HasPrefix(m.point, pods) && filepath.Base(m.point) == Dir {
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
		for _, dir := range []string{filepath.Join(v, "mount"), v} {
			if fd, err := openBeneath(c.KubeletDir, filepath.Join(dir, runID)); err == nil {
				_ = unix.Close(fd)
				return dir, nil
			}
		}
	}
	return "", fmt.Errorf("no volume of pod %s holds run %s", workspaceUID, runID)
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
	return unix.Mount(source, fdPath(fd), "nfs4", 0, c.Options+",addr="+host)
}

// Unmount force-detaches the mount. Readers blocked on a dead server get an error.
func (c Client) Unmount(path string) error {
	err := unix.Unmount(path, unix.MNT_FORCE|unix.MNT_DETACH)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
		return nil
	}
	return err
}

// openBeneath opens path, which must lie under root, as an O_PATH directory. Every
// component below root is resolved without following symlinks and cannot escape root.
func openBeneath(root, path string) (int, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
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

// fdPath is the magic link mount(2) resolves to the directory fd refers to.
func fdPath(fd int) string { return "/proc/self/fd/" + strconv.Itoa(fd) }

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
