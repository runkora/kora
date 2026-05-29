//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// stripNsChildEnv returns a copy of env with the namespace-child marker removed
// so it does not propagate into the exec'd claude process.
func stripNsChildEnv(env []string) []string {
	prefix := nsChildEnvKey + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// nsChildEnvKey is set on the child self-invocation so it knows to do
// namespace setup (proc remount + Landlock) then exec the target binary.
const nsChildEnvKey = "ORBIT_RUNNER_NS_CHILD"

// nsChildExec is called at the very start of main() when nsChildEnvKey is set,
// meaning we are running inside a fresh CLONE_NEWUSER|CLONE_NEWPID|CLONE_NEWNS
// namespace created by runInPIDNamespace below.
//
// Execution order (inside the namespace):
//  1. Remount /proc — creates a new procfs scoped to this PID namespace so the
//     agent cannot enumerate sibling processes. Must happen before Landlock so
//     the whitelist targets the new mount's inodes, not the host proc's.
//  2. Apply Landlock — restricts filesystem access before handing control to claude.
//  3. Exec claude — replace this process with the target binary.
func nsChildExec(args []string) {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "orbit-session-runner (ns-child): too few arguments")
		os.Exit(1)
	}
	workDir := args[1]
	binary := args[2]
	argv := args[2:]

	// Remount /proc for this PID namespace. The MNT_DETACH first lazily unmounts
	// the inherited host proc; the fresh mount is scoped to our namespace only.
	// Non-fatal: if the remount fails the agent still runs but proc isolation is
	// degraded (sibling processes remain visible). Log and continue.
	_ = syscall.Unmount("/proc", syscall.MNT_DETACH)
	if err := syscall.Mount("proc", "/proc", "proc",
		syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, ""); err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: remount /proc: %v (proc isolation degraded)\n", err)
	}

	// Apply Landlock. Because /proc was just remounted above, the RODirs("/proc")
	// rule whitelists the new namespace-scoped proc mount — not the host's.
	applyLandlock(workDir)

	// Strip the namespace marker so it does not propagate to claude.
	env := stripNsChildEnv(os.Environ())

	// Resolve and exec the target binary. LookPath is allowed because Landlock
	// whitelists RODirs on /usr, /bin, /sbin, /lib, /etc — all PATH components.
	resolved, err := exec.LookPath(binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: resolve %q: %v\n", binary, err)
		os.Exit(1)
	}
	if err = syscall.Exec(resolved, argv, env); err != nil {
		fmt.Fprintf(os.Stderr, "orbit-session-runner: exec %q: %v\n", resolved, err)
		os.Exit(1)
	}
}

// runInPIDNamespace self-reinvokes orbit-session-runner inside a new
// CLONE_NEWUSER | CLONE_NEWPID | CLONE_NEWNS namespace. The child detects
// nsChildEnvKey, remounts /proc, applies Landlock, then execs the target binary.
//
// Returns an error only if cmd.Start() fails (e.g. CLONE_NEWUSER blocked by
// seccomp or kernel.unprivileged_userns_clone=0). The caller should fall back
// to direct exec with Landlock in that case.
func runInPIDNamespace(_ string, _ []string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable: %w", err)
	}

	// Re-invoke ourselves with the same arguments. The child detects nsChildEnvKey
	// and takes the setup-then-exec path rather than normal startup.
	cmd := exec.Command(self, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), nsChildEnvKey+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		// Identity UID/GID mapping: the process has the same UID inside and outside
		// the namespace, avoiding root-detection behaviour in Node.js / claude.
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: os.Getuid(), HostID: os.Getuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: os.Getgid(), HostID: os.Getgid(), Size: 1},
		},
		// Kill claude if orbit-session-runner exits unexpectedly (e.g. OOM kill).
		Pdeathsig: syscall.SIGKILL,
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Forward SIGINT and SIGTERM to the child. The session-manager sends SIGINT to
	// this wrapper's PID to interrupt an in-progress turn; it must reach claude.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigs {
			_ = cmd.Process.Signal(sig)
		}
	}()

	_ = cmd.Wait()
	os.Exit(cmd.ProcessState.ExitCode())
	return nil // unreachable; required for compiler
}
