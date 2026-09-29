//go:build linux

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
)

// ── stripNsChildEnv ──────────────────────────────────────────────────────────

func TestStripNsChildEnv_RemovesMarker(t *testing.T) {
	env := []string{
		"HOME=/workspaces/run-abc/.runhome",
		nsChildEnvKey + "=1",
		"PATH=/usr/local/bin:/usr/bin",
		"CUSTOM_VAR=hello",
	}
	got := stripNsChildEnv(env)
	for _, e := range got {
		if strings.HasPrefix(e, nsChildEnvKey+"=") {
			t.Errorf("stripped env still contains marker %q", e)
		}
	}
}

func TestStripNsChildEnv_PreservesOtherVars(t *testing.T) {
	env := []string{
		"HOME=/workspaces/run-abc/.runhome",
		nsChildEnvKey + "=1",
		"CUSTOM_VAR=hello",
		"WORKSPACE=/workspaces/run-abc",
	}
	got := stripNsChildEnv(env)
	for _, want := range []string{"HOME=/workspaces/run-abc/.runhome", "CUSTOM_VAR=hello", "WORKSPACE=/workspaces/run-abc"} {
		if !slices.Contains(got, want) {
			t.Errorf("stripped env missing %q", want)
		}
	}
}

func TestStripNsChildEnv_NoMarkerIsNoop(t *testing.T) {
	env := []string{"FOO=1", "BAR=2"}
	got := stripNsChildEnv(env)
	if len(got) != len(env) {
		t.Errorf("len = %d, want %d", len(got), len(env))
	}
}

// ── proc isolation integration tests ────────────────────────────────────────
//
// These tests build the orbit-session-runner binary and run it with a /bin/sh
// payload to verify that the proc mount is correctly scoped to the new PID
// namespace. They are skipped in short mode and when user namespaces are
// unavailable (e.g. some CI environments).

// buildRunnerBinary returns the path to the orbit-session-runner binary.
// Prefers the already-installed binary in PATH (production pods), then falls
// back to building from source (dev machines). Skips the calling test if
// neither is available.
func buildRunnerBinary(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("orbit-session-runner"); err == nil {
		return p
	}
	// Fall back to building from source.
	bin := filepath.Join(t.TempDir(), "orbit-session-runner")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("orbit-session-runner not in PATH and could not build from source: %v\n%s", err, out)
	}
	return bin
}

// userNamespacesAvailable returns false if CLONE_NEWUSER is blocked in this
// environment (e.g. hardened seccomp profile or unprivileged_userns_clone=0).
func userNamespacesAvailable() bool {
	return exec.Command("unshare", "--user", "--pid", "true").Run() == nil
}

// runnerScript runs orbit-session-runner with a /bin/sh payload, returning
// stdout and stderr separately. Fails the test immediately if the runner exits
// non-zero (i.e. applyLandlock or exec itself failed — not the shell script).
func runnerScript(t *testing.T, bin, workDir, script string) (stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(bin, workDir, "/bin/sh", "-c", script)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("orbit-session-runner exited with error: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}
	return strings.TrimSpace(outBuf.String()), errBuf.String()
}

// TestProcIsolation_HostProcessInvisible verifies that a sibling process
// running on the host is NOT visible via /proc from inside the namespace.
// This is the core guarantee: /proc is scoped to the agent's own PID namespace.
//
// Skips when proc remount is blocked by the environment's seccomp/AppArmor
// policy — degraded mode is logged to stderr by the runner itself.
func TestProcIsolation_HostProcessInvisible(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping proc isolation integration test in short mode")
	}
	if !userNamespacesAvailable() {
		t.Skip("user namespaces not available or blocked in this environment")
	}

	bin := buildRunnerBinary(t)
	workDir := t.TempDir()

	// /proc/<ourPID> should be invisible from inside the namespace — we are
	// running in the host PID namespace, the agent cannot see us.
	ourPID := strconv.Itoa(os.Getpid())
	script := fmt.Sprintf(`[ ! -e /proc/%s ] && echo ISOLATED || echo LEAKED`, ourPID)

	stdout, stderr := runnerScript(t, bin, workDir, script)
	if strings.Contains(stderr, "remount /proc: permission denied") {
		t.Skip("proc remount blocked by this environment's security policy (proc isolation degraded); run on a bare-metal or less-restricted host to verify full isolation")
	}
	if stdout != "ISOLATED" {
		t.Errorf("proc isolation failed: host PID %s is visible from inside the namespace (stdout: %q)", ourPID, stdout)
	}
}

// TestProcIsolation_CanSeeSelf verifies that the agent CAN see its own process
// in /proc. This confirms /proc works normally for self-introspection (ps, etc.)
// and that we haven't broken the proc mount entirely.
func TestProcIsolation_CanSeeSelf(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping proc isolation integration test in short mode")
	}
	if !userNamespacesAvailable() {
		t.Skip("user namespaces not available or blocked in this environment")
	}

	bin := buildRunnerBinary(t)
	workDir := t.TempDir()

	// PID 1 is always the agent itself inside the namespace. /proc/1 must exist.
	stdout, _ := runnerScript(t, bin, workDir, `[ -e /proc/1 ] && echo VISIBLE || echo MISSING`)
	if stdout != "VISIBLE" {
		t.Errorf("proc self-visibility broken: /proc/1 not visible inside the namespace (stdout: %q)", stdout)
	}
}

// TestProcIsolation_NsChildEnvNotPropagated verifies that ORBIT_RUNNER_NS_CHILD
// is stripped from the environment before the target binary is exec'd.
func TestProcIsolation_NsChildEnvNotPropagated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping proc isolation integration test in short mode")
	}
	if !userNamespacesAvailable() {
		t.Skip("user namespaces not available or blocked in this environment")
	}

	bin := buildRunnerBinary(t)
	workDir := t.TempDir()

	// If ORBIT_RUNNER_NS_CHILD reaches claude it would trigger a second round
	// of nsChildExec and remount /proc again — a clear bug.
	script := fmt.Sprintf(`[ -z "$%s" ] && echo STRIPPED || echo LEAKED`, nsChildEnvKey)

	stdout, _ := runnerScript(t, bin, workDir, script)
	if stdout != "STRIPPED" {
		t.Errorf("%s was not stripped from the subprocess environment", nsChildEnvKey)
	}
}

// TestPodTmpDenied verifies what a run can reach outside its workspace: not the
// pod's /tmp, which every run on the pod shares; its own TMPDIR read-write; and
// its config dir read-only.
func TestPodTmpDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping /tmp isolation integration test in short mode")
	}
	if abi, err := llsyscall.LandlockGetABIVersion(); err != nil || abi < 1 {
		t.Skip("landlock not enabled on this kernel")
	}

	bin := buildRunnerBinary(t)
	workDir := filepath.Join(t.TempDir(), "run-1")
	runTmp, configDir := runfs.TmpDir(workDir), runfs.ConfigDir(workDir)
	for _, dir := range []string{workDir, runTmp, configDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions.md"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	podFile, err := os.CreateTemp("/tmp", "pod-shared-")
	if err != nil {
		t.Fatalf("create pod /tmp file: %v", err)
	}
	_ = podFile.Close()
	t.Cleanup(func() { _ = os.Remove(podFile.Name()) })

	script := fmt.Sprintf(`cat %s >/dev/null 2>&1 && echo SHARED || echo DENIED
echo x > %s/f && echo RUNTMP
cat %s/instructions.md >/dev/null && echo CONFIG_READ
echo x > %s/instructions.md 2>/dev/null && echo CONFIG_WRITTEN || echo CONFIG_READONLY`, podFile.Name(), runTmp, configDir, configDir)
	stdout, stderr := runnerScript(t, bin, workDir, script)
	if want := "DENIED\nRUNTMP\nCONFIG_READ\nCONFIG_READONLY"; stdout != want {
		t.Errorf("stdout = %q, want %q (stderr: %s)", stdout, want, stderr)
	}
}
