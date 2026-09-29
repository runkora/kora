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

// ConfigDir holds files the platform writes for the run's agent. It is beside the
// workspace and read-only to the agent, and the sandbox never mounts it.
func ConfigDir(workspace string) string {
	return filepath.Join(filepath.Dir(workspace), ".runconfig", filepath.Base(workspace))
}

// toolDirs are the sandbox image's toolchains that a non-login shell misses.
var toolDirs = []string{"/usr/local/go/bin", "/usr/local/cargo/bin"}

// InstallEnv points package managers and their caches at user-level locations
// under home, so installs need no root and persist on the volume with or without
// a sandbox. PATH is basePath, then the sandbox toolchains, then home's bin
// directories last, so nothing installed shadows a system binary.
//
// PIP_BREAK_SYSTEM_PACKAGES lets pip use the user site of Debian's managed
// Python; PIP_USER stays unset because it breaks pip inside a virtualenv.
func InstallEnv(home, basePath string) []string {
	local := filepath.Join(home, ".local")
	gopath := filepath.Join(home, "go")
	cargoHome := filepath.Join(home, ".cargo")
	cache := filepath.Join(home, ".cache")
	path := append([]string{basePath}, toolDirs...)
	path = append(path, filepath.Join(local, "bin"), filepath.Join(gopath, "bin"), filepath.Join(cargoHome, "bin"))
	return []string{
		"NPM_CONFIG_PREFIX=" + local,
		"NPM_CONFIG_CACHE=" + filepath.Join(home, ".npm"),
		"PYTHONUSERBASE=" + local,
		"PIP_BREAK_SYSTEM_PACKAGES=1",
		"PIP_CACHE_DIR=" + filepath.Join(cache, "pip"),
		"GOPATH=" + gopath,
		"GOCACHE=" + filepath.Join(cache, "go-build"),
		"CARGO_HOME=" + cargoHome,
		"PATH=" + strings.Join(path, string(os.PathListSeparator)),
	}
}
