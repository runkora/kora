// Package runfs is where a run's files live on the shared workspace volume. The
// agent pod and the sandbox use the same absolute paths, so a path one side
// writes is the path the other side reads (docs/adr/2026-06-04-per-session-home-isolation.md).
package runfs

import (
	"os"
	"path/filepath"
	"strings"
)

// HomeDir is the run's $HOME, inside its workspace.
func HomeDir(workspace string) string {
	return filepath.Join(workspace, ".runhome")
}

// TmpDir is the run's $TMPDIR. It sits beside the workspace, not in it: the agent
// can rename anything in its workspace, and a sandbox subPath it could swap for a
// symlink to ".." would mount the whole volume.
func TmpDir(workspace string) string {
	return filepath.Join(filepath.Dir(workspace), ".tmp", filepath.Base(workspace))
}

// LocalDir holds the run's build caches and tool installs, beside the workspace like TmpDir.
// The sandbox mounts a node-local volume there; in the agent pod it is on the shared volume.
func LocalDir(workspace string) string {
	return filepath.Join(filepath.Dir(workspace), ".runlocal", filepath.Base(workspace))
}

// ConfigDir holds files the platform writes for the run's agent. It is beside the
// workspace and read-only to the agent, and the sandbox never mounts it.
func ConfigDir(workspace string) string {
	return filepath.Join(filepath.Dir(workspace), ".runconfig", filepath.Base(workspace))
}

// toolDirs are the sandbox image's toolchains that a non-login shell misses.
var toolDirs = []string{"/usr/local/go/bin", "/usr/local/cargo/bin"}

// InstallEnv points package managers, caches and user-level installs at dir, so installs work without root.
// Installed tools come last on PATH to keep them from shadowing a system binary.
//
// PIP_BREAK_SYSTEM_PACKAGES lets pip use the user site of Debian's managed
// Python; PIP_USER stays unset because it breaks pip inside a virtualenv.
func InstallEnv(dir, basePath string) []string {
	local := filepath.Join(dir, ".local")
	gopath := filepath.Join(dir, "go")
	cargoHome := filepath.Join(dir, ".cargo")
	cache := filepath.Join(dir, ".cache")
	path := append([]string{basePath}, toolDirs...)
	path = append(path, filepath.Join(local, "bin"), filepath.Join(gopath, "bin"), filepath.Join(cargoHome, "bin"))
	return []string{
		"NPM_CONFIG_PREFIX=" + local,
		"NPM_CONFIG_CACHE=" + filepath.Join(dir, ".npm"),
		"PYTHONUSERBASE=" + local,
		"PIP_BREAK_SYSTEM_PACKAGES=1",
		"PIP_CACHE_DIR=" + filepath.Join(cache, "pip"),
		"GOPATH=" + gopath,
		"GOMODCACHE=" + filepath.Join(gopath, "pkg", "mod"),
		"GOCACHE=" + filepath.Join(cache, "go-build"),
		"CARGO_HOME=" + cargoHome,
		"PATH=" + strings.Join(path, string(os.PathListSeparator)),
	}
}
