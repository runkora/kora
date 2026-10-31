// kora-session-runner applies Landlock to itself, then execs the agent binary,
// which inherits the restriction.
//
// Usage: kora-session-runner <workDir> <binary> [args...]
//
// The agent can write only its workspace, its TMPDIR and /dev, and read system
// paths, its config dir and a fixed set of system-wide /proc files. It can
// execute only the harness binary and the platform binaries it starts, so every
// other command has to go through kora-shell to the run's sandbox. No per-process
// /proc entry is readable, its own included, so a run cannot list /proc or read a
// sibling's command line, status or environment. It can still stat /proc/<pid>.
// Where the kernel supports them (6.12+), Landlock's IPC scopes stop it from
// signaling processes or connecting to abstract Unix sockets outside its domain.
//
// KORA_LANDLOCK selects the mode. In enforce, the default, the runner refuses to
// start the agent on a kernel below Landlock ABI v2. In bestEffort a kernel
// without Landlock runs the agent unrestricted.
//
// See docs/adr/2026-06-04-per-session-home-isolation.md.
package main

import (
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"github.com/runkora/kora/backend/internal/agents/sandbox/landlockmode"
	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
)

// Access rights without execute. go-landlock's RODirs and RWDirs include it.
const (
	readOnly  = landlock.AccessFSSet(llsyscall.AccessFSReadFile | llsyscall.AccessFSReadDir)
	readWrite = readOnly | landlock.AccessFSSet(llsyscall.AccessFSWriteFile|llsyscall.AccessFSTruncate|
		llsyscall.AccessFSRemoveDir|llsyscall.AccessFSRemoveFile|llsyscall.AccessFSMakeDir|llsyscall.AccessFSMakeReg|
		llsyscall.AccessFSMakeSym|llsyscall.AccessFSMakeSock|llsyscall.AccessFSMakeFifo|llsyscall.AccessFSMakeChar|llsyscall.AccessFSMakeBlock)
	runFile = landlock.AccessFSSet(llsyscall.AccessFSReadFile | llsyscall.AccessFSExecute)
)

// platformBinaries are the platform executables a harness starts: kora-shell is
// its shell and kora-runtime-mcp provisions the sandbox. They sit next to this
// binary in the image.
var platformBinaries = []string{"kora-shell", "kora-runtime-mcp"}

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
		fmt.Fprintln(os.Stderr, "usage: kora-session-runner <workDir> <binary> [args...]")
		os.Exit(1)
	}

	workDir := os.Args[1]
	binary := os.Args[2]
	argv := os.Args[2:]

	// Resolve the binary before Landlock: the PATH lookup may need directories
	// the agent cannot read.
	resolved, err := exec.LookPath(binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kora-session-runner: resolve %q: %v\n", binary, err)
		os.Exit(1)
	}

	if err = checkKernel(os.Getenv(landlockmode.EnvVar), landlockmode.KernelABI()); err != nil {
		fmt.Fprintf(os.Stderr, "kora-session-runner: %v\n", err)
		os.Exit(1)
	}
	applyLandlock(workDir, executables(resolved))
	if err = syscall.Exec(resolved, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "kora-session-runner: exec %q: %v\n", resolved, err)
		os.Exit(1)
	}
}

// checkKernel returns an error when the mode cannot run an agent on this kernel.
func checkKernel(mode string, abi int) error {
	m, err := landlockmode.Parse(mode)
	if err != nil {
		return err
	}
	return m.Check(abi)
}

// executables returns the files the agent may execute: the harness, the
// platform binaries beside this one, and the ELF interpreter each one needs.
// The run's PATH also holds directories the agent writes, so it is not searched.
func executables(harness string) []string {
	files := []string{harness}
	if self, err := os.Executable(); err == nil {
		for _, name := range platformBinaries {
			files = append(files, filepath.Join(filepath.Dir(self), name))
		}
	}
	for _, f := range files {
		if interp := elfInterpreter(f); interp != "" {
			files = append(files, interp)
		}
	}
	return files
}

// elfInterpreter returns the dynamic loader a binary names, or "" for a static
// binary or a file that is not ELF.
func elfInterpreter(path string) string {
	f, err := elf.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	for _, p := range f.Progs {
		if p.Type != elf.PT_INTERP {
			continue
		}
		b := make([]byte, p.Filesz)
		if _, err := p.ReadAt(b, 0); err != nil {
			return ""
		}
		return strings.TrimRight(string(b), "\x00")
	}
	return ""
}

// applyLandlock restricts the current process and everything it execs.
// BestEffort downgrades to what the kernel supports, down to nothing.
func applyLandlock(workDir string, execFiles []string) {
	// Only paths that exist: RestrictPaths fails on a missing one, which would
	// turn an image without /sbin into a failed run.
	rules := []landlock.Rule{landlock.PathAccess(readWrite, workDir)}
	// /tmp is left out. Every run on the pod shares it.
	// TMPDIR is runfs.TmpDir and caches sit under runfs.LocalDir.
	if rw := existing(true, runfs.TmpDir(workDir), runfs.LocalDir(workDir), "/dev"); len(rw) > 0 {
		rules = append(rules, landlock.PathAccess(readWrite, rw...))
	}
	// The run's config dir holds platform-written files the agent reads but must not change.
	if ro := existing(true, runfs.ConfigDir(workDir), "/usr", "/bin", "/sbin", "/lib", "/etc", "/run"); len(ro) > 0 {
		rules = append(rules, landlock.PathAccess(readOnly, ro...))
	}
	if ro := existing(false, procFiles...); len(ro) > 0 {
		rules = append(rules, landlock.PathAccess(landlock.AccessFSSet(llsyscall.AccessFSReadFile), ro...))
	}
	if x := existing(false, execFiles...); len(x) > 0 {
		rules = append(rules, landlock.PathAccess(runFile, x...))
	}
	if err := landlock.V3.BestEffort().RestrictPaths(rules...); err != nil {
		fmt.Fprintf(os.Stderr, "kora-session-runner: landlock restriction failed: %v\n", err)
		os.Exit(1)
	}
	if err := landlock.V6.BestEffort().RestrictScoped(); err != nil {
		fmt.Fprintf(os.Stderr, "kora-session-runner: landlock scope restriction failed: %v\n", err)
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
