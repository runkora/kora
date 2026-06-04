// orbit-session-runner applies Landlock filesystem restrictions and PID namespace
// isolation, then exec()s the target binary.
//
// Usage: orbit-session-runner <workDir> <binary> [args...]
//
// Isolation applied (all best-effort — gracefully skipped on unsupported kernels):
//
//  1. CLONE_NEWUSER | CLONE_NEWPID | CLONE_NEWNS — the wrapper self-reinvokes
//     inside a fresh namespace. The child remounts /proc (scoping it to its own
//     PID namespace so sibling processes are invisible), then applies Landlock,
//     then execs claude. Landlock must be applied after the proc remount so the
//     whitelist targets the new mount's inodes rather than the host's.
//
//  2. Landlock V3 filesystem restrictions (kernel ≥ 5.13) — applied inside the
//     namespace child before exec'ing claude.
//
// When namespace creation fails (CLONE_NEWUSER blocked by seccomp / sysctl),
// falls back to direct exec with Landlock applied in the parent process.
//
// See ADR-0062 for the full isolation policy.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
)

func main() {
	// nsChildEnvKey is set when we are the self-reinvoked namespace child. In that
	// case hand off immediately — no Landlock applied yet, no LookPath, no normal
	// startup logic. nsChildExec calls os.Exit and never returns.
	if os.Getenv(nsChildEnvKey) != "" {
		nsChildExec(os.Args)
	}

	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: orbit-session-runner <workDir> <binary> [args...]")
		os.Exit(1)
	}

	workDir := os.Args[1]
	binary := os.Args[2]
	argv := os.Args[2:]

	// Resolve the binary before any Landlock is applied — PATH lookups need read
	// access to directories that Landlock would otherwise restrict.
	resolved, err := exec.LookPath(binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: resolve %q: %v\n", binary, err)
		os.Exit(1)
	}

	// Try to run claude in a new PID+mount namespace. The child (self-reinvoked)
	// remounts /proc and applies Landlock before exec'ing claude. runInPIDNamespace
	// calls os.Exit on success and only returns an error if the namespace could not
	// be created (CLONE_NEWUSER blocked). See ADR-0062 Phase 3.
	if err = runInPIDNamespace(resolved, argv); err == nil {
		return
	}

	// Fallback: apply Landlock in this process then exec claude directly.
	// Filesystem isolation still holds; proc-table leak remains in this path.
	applyLandlock(workDir)
	if err = syscall.Exec(resolved, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: exec %q: %v\n", resolved, err)
		os.Exit(1)
	}
}

// applyLandlock applies filesystem access restrictions to the current process.
// Called either by the namespace child (after /proc remount) or in the fallback
// path (before direct exec). BestEffort() silently no-ops on kernels < 5.13.
func applyLandlock(workDir string) {
	// Only restrict paths that actually exist. Landlock's RestrictPaths returns
	// ENOENT for a missing whitelisted path (BestEffort only downgrades the ABI
	// version, it does not tolerate missing paths), which would turn a benign "this
	// image lacks /sbin or /run" into a fatal os.Exit and kill the run with no
	// fallback — the opposite of the best-effort contract. Filtering keeps
	// enforcement intact for the paths that do exist.
	rules := []landlock.Rule{landlock.RWDirs(workDir)}
	if rw := existingDirs("/dev", "/tmp"); len(rw) > 0 {
		rules = append(rules, landlock.RWDirs(rw...))
	}
	if ro := existingDirs("/usr", "/bin", "/sbin", "/lib", "/etc", "/proc", "/run"); len(ro) > 0 {
		rules = append(rules, landlock.RODirs(ro...))
	}
	if err := landlock.V3.BestEffort().RestrictPaths(rules...); err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: landlock restriction failed: %v\n", err)
		os.Exit(1)
	}
}

// existingDirs returns the subset of paths that exist and are directories.
func existingDirs(paths ...string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			out = append(out, p)
		}
	}
	return out
}
