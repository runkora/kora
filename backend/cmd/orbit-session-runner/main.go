// orbit-session-runner applies Landlock to itself, then execs the agent binary,
// which inherits the restriction.
//
// Usage: orbit-session-runner <workDir> <binary> [args...]
//
// The agent can write only its workspace, its TMPDIR and /dev, and read system
// paths, its config dir and a fixed set of system-wide /proc files. No per-process
// /proc entry is readable, its own included, so a run cannot list the pod's
// processes or read a sibling's command line. Landlock's IPC scopes stop it from
// signaling processes outside its domain where the kernel supports them (6.12+).
// All of it is best-effort: kernels without Landlock run the agent unrestricted.
//
// See docs/adr/2026-06-04-per-session-home-isolation.md.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
	"github.com/landlock-lsm/go-landlock/landlock"
)

// procFiles are the /proc files the agent may read. None describes a process:
// runtimes read them for CPU and memory totals and kernel settings.
var procFiles = []string{
	"/proc/cpuinfo",
	"/proc/meminfo",
	"/proc/stat",
	"/proc/version",
	"/proc/sys/vm/overcommit_memory",
	"/proc/sys/vm/mmap_min_addr",
	"/proc/sys/kernel/overflowuid",
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: orbit-session-runner <workDir> <binary> [args...]")
		os.Exit(1)
	}

	workDir := os.Args[1]
	binary := os.Args[2]
	argv := os.Args[2:]

	// Resolve the binary before Landlock: the PATH lookup may need directories
	// the agent cannot read.
	resolved, err := exec.LookPath(binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: resolve %q: %v\n", binary, err)
		os.Exit(1)
	}

	applyLandlock(workDir)
	if err = syscall.Exec(resolved, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: exec %q: %v\n", resolved, err)
		os.Exit(1)
	}
}

// applyLandlock restricts the current process and everything it execs.
// BestEffort downgrades to what the kernel supports, down to nothing.
func applyLandlock(workDir string) {
	// Only paths that exist: RestrictPaths fails on a missing one, which would
	// turn an image without /sbin into a failed run.
	rules := []landlock.Rule{landlock.RWDirs(workDir)}
	// /tmp is left out: the pod's /tmp is shared by every run on the pod, so the
	// agent writes only to the shared volume. Its TMPDIR is runfs.TmpDir.
	if rw := existing(true, runfs.TmpDir(workDir), "/dev"); len(rw) > 0 {
		rules = append(rules, landlock.RWDirs(rw...))
	}
	// The run's config dir holds platform-written files the agent reads but must not change.
	if ro := existing(true, runfs.ConfigDir(workDir), "/usr", "/bin", "/sbin", "/lib", "/etc", "/run"); len(ro) > 0 {
		rules = append(rules, landlock.RODirs(ro...))
	}
	if ro := existing(false, procFiles...); len(ro) > 0 {
		rules = append(rules, landlock.ROFiles(ro...))
	}
	if err := landlock.V3.BestEffort().RestrictPaths(rules...); err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: landlock restriction failed: %v\n", err)
		os.Exit(1)
	}
	if err := landlock.V6.BestEffort().RestrictScoped(); err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: landlock scope restriction failed: %v\n", err)
		os.Exit(1)
	}
}

// existing returns the paths that exist and are directories (dirs) or not (files).
func existing(dirs bool, paths ...string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() == dirs {
			out = append(out, p)
		}
	}
	return out
}
