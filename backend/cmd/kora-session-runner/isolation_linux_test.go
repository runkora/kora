//go:build linux

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
)

// Integration tests: they run the kora-session-runner binary with a /bin/sh
// payload and skip in short mode or where Landlock is not enabled.

// buildRunnerBinary builds kora-session-runner from this package's source, so
// the tests exercise the code under test rather than an installed binary.
func buildRunnerBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kora-session-runner")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not build kora-session-runner: %v\n%s", err, out)
	}
	return bin
}

// requireLandlock skips the test unless the kernel enforces Landlock at the given ABI.
func requireLandlock(t *testing.T, minABI int) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Landlock integration test in short mode")
	}
	if abi, err := llsyscall.LandlockGetABIVersion(); err != nil || abi < minABI {
		t.Skipf("Landlock ABI %d not available on this kernel", minABI)
	}
}

// runnerScript runs kora-session-runner with a /bin/sh payload, returning
// stdout and stderr separately. Fails the test immediately if the runner exits
// non-zero (i.e. applyLandlock or exec itself failed — not the shell script).
func runnerScript(t *testing.T, bin, workDir, script string) (stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(bin, workDir, "/bin/sh", "-c", script)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("kora-session-runner exited with error: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}
	return strings.TrimSpace(outBuf.String()), errBuf.String()
}

// The allow-list must name only system-wide files; a per-process path would
// reopen a run's view of its siblings.
func TestProcFilesAreSystemWide(t *testing.T) {
	for _, p := range procFiles {
		if !strings.HasPrefix(p, "/proc/") {
			t.Errorf("%s is outside /proc", p)
		}
		first := strings.SplitN(strings.TrimPrefix(p, "/proc/"), "/", 2)[0]
		if _, err := strconv.Atoi(first); err == nil || first == "self" || first == "thread-self" {
			t.Errorf("%s is a per-process path", p)
		}
	}
}

// startSibling runs a second agent-like process through the runner, with a
// secret in its arguments and environment, and returns its PID.
func startSibling(t *testing.T, bin string) int {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sibling")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, dir, "/bin/sh", "-c", "sleep 30; true", "--append-system-prompt", "SIBLING-SECRET")
	cmd.Env = append(os.Environ(), "KORA_RUN_TOKEN=SIBLING-TOKEN")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sibling: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	// The runner execs in place, so the sibling keeps this PID; wait until it has.
	for range 50 {
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", cmd.Process.Pid)); err == nil && bytes.HasPrefix(b, []byte("/bin/sh\x00")) {
			return cmd.Process.Pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("sibling did not exec")
	return 0
}

// A run sees nothing of another run's process: not in a listing, and not its
// command line, status, stat or environment.
func TestSiblingProcessHidden(t *testing.T) {
	requireLandlock(t, 1)
	bin := buildRunnerBinary(t)
	pid := startSibling(t, bin)

	script := fmt.Sprintf(`ls /proc >/dev/null 2>&1 && echo LIST || echo NOLIST
for f in cmdline status stat environ; do cat /proc/%d/$f >/dev/null 2>&1 && echo "$f" || echo "no-$f"; done`, pid)
	stdout, stderr := runnerScript(t, bin, t.TempDir(), script)
	if want := "NOLIST\nno-cmdline\nno-status\nno-stat\nno-environ"; stdout != want {
		t.Errorf("stdout = %q, want %q (stderr: %s)", stdout, want, stderr)
	}
}

// The allow-listed system-wide files stay readable; the run's own /proc entry does not.
func TestProcAllowList(t *testing.T) {
	requireLandlock(t, 1)
	bin := buildRunnerBinary(t)

	stdout, stderr := runnerScript(t, bin, t.TempDir(), `cat /proc/cpuinfo /proc/meminfo /proc/stat >/dev/null && echo SYSTEM
cat /proc/self/status >/dev/null 2>&1 && echo SELF || echo NOSELF`)
	if want := "SYSTEM\nNOSELF"; stdout != want {
		t.Errorf("stdout = %q, want %q (stderr: %s)", stdout, want, stderr)
	}
}

// Where the kernel scopes signals, a run cannot signal a process outside its domain.
func TestSignalScoped(t *testing.T) {
	requireLandlock(t, 6)
	bin := buildRunnerBinary(t)
	pid := startSibling(t, bin)

	stdout, stderr := runnerScript(t, bin, t.TempDir(), fmt.Sprintf(`kill -0 %d 2>/dev/null && echo SIGNALLED || echo DENIED`, pid))
	if stdout != "DENIED" {
		t.Errorf("stdout = %q, want DENIED (stderr: %s)", stdout, stderr)
	}
}

// TestPodTmpDenied verifies what a run can reach outside its workspace: not the
// pod's /tmp, which every run on the pod shares; its own TMPDIR read-write; and
// its config dir read-only.
func TestPodTmpDenied(t *testing.T) {
	requireLandlock(t, 1)

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
