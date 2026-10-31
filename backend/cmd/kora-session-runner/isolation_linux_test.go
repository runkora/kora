//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"github.com/runkora/kora/backend/internal/agents/sandbox/landlockmode"
	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
)

// Integration tests: the test binary runs kora-session-runner, which execs the
// test binary again as the agent. That probe reports what it may read, write,
// list, signal and execute. They skip in short mode or where Landlock is not enabled.

const probeEnv = "KORA_RUNNER_PROBE"

func TestMain(m *testing.M) {
	if os.Getenv(probeEnv) == "1" {
		probe(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

// probe runs each action and prints "<verb>-ok" or "<verb>-denied" for it.
// Actions are read:<path>, write:<path>, list:<dir>, signal:<pid>, exec:<path>
// and sleep:<seconds>. Anything else is ignored.
func probe(actions []string) {
	for _, a := range actions {
		verb, arg, _ := strings.Cut(a, ":")
		var err error
		switch verb {
		case "read":
			_, err = os.ReadFile(arg)
		case "write":
			err = os.WriteFile(arg, []byte("x"), 0o600)
		case "list":
			_, err = os.ReadDir(arg)
		case "signal":
			pid, _ := strconv.Atoi(arg)
			err = syscall.Kill(pid, 0)
		case "exec":
			// A started process that exits non-zero still ran.
			var exitErr *exec.ExitError
			if err = exec.Command(arg).Run(); errors.As(err, &exitErr) {
				err = nil
			}
		case "sleep":
			secs, _ := strconv.Atoi(arg)
			time.Sleep(time.Duration(secs) * time.Second)
			continue
		default:
			continue
		}
		if err != nil {
			fmt.Println(verb + "-denied")
		} else {
			fmt.Println(verb + "-ok")
		}
	}
}

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

// probeCmd is kora-session-runner starting the probe in workDir with the given actions.
func probeCmd(bin, workDir string, actions ...string) *exec.Cmd {
	cmd := exec.Command(bin, append([]string{workDir, os.Args[0]}, actions...)...)
	cmd.Env = append(os.Environ(), probeEnv+"=1", landlockmode.EnvVar+"="+string(landlockmode.BestEffort))
	return cmd
}

// runProbe runs the probe and returns its output. It fails the test if the
// runner itself fails.
func runProbe(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("kora-session-runner exited with error: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}
	return strings.TrimSpace(outBuf.String())
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
	cmd := probeCmd(bin, dir, "sleep:30", "--append-system-prompt", "SIBLING-SECRET")
	cmd.Env = append(cmd.Env, "KORA_RUN_TOKEN=SIBLING-TOKEN")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sibling: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	// The runner execs in place, so the sibling keeps this PID; wait until it has.
	want := []byte(os.Args[0] + "\x00")
	for range 50 {
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", cmd.Process.Pid)); err == nil && bytes.HasPrefix(b, want) {
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

	actions := []string{"list:/proc"}
	for _, f := range []string{"cmdline", "status", "stat", "environ"} {
		actions = append(actions, fmt.Sprintf("read:/proc/%d/%s", pid, f))
	}
	got := runProbe(t, probeCmd(bin, t.TempDir(), actions...))
	if want := "list-denied\nread-denied\nread-denied\nread-denied\nread-denied"; got != want {
		t.Errorf("probe = %q, want %q", got, want)
	}
}

// The allow-listed system-wide files stay readable; the run's own /proc entry does not.
func TestProcAllowList(t *testing.T) {
	requireLandlock(t, 1)
	bin := buildRunnerBinary(t)

	got := runProbe(t, probeCmd(bin, t.TempDir(), "read:/proc/cpuinfo", "read:/proc/meminfo", "read:/proc/stat", "read:/proc/self/status"))
	if want := "read-ok\nread-ok\nread-ok\nread-denied"; got != want {
		t.Errorf("probe = %q, want %q", got, want)
	}
}

// Where the kernel scopes signals, a run cannot signal a process outside its domain.
func TestSignalScoped(t *testing.T) {
	requireLandlock(t, 6)
	bin := buildRunnerBinary(t)
	pid := startSibling(t, bin)

	if got := runProbe(t, probeCmd(bin, t.TempDir(), fmt.Sprintf("signal:%d", pid))); got != "signal-denied" {
		t.Errorf("probe = %q, want signal-denied", got)
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
	instructions := filepath.Join(configDir, "instructions.md")
	if err := os.WriteFile(instructions, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	podFile, err := os.CreateTemp("/tmp", "pod-shared-")
	if err != nil {
		t.Fatalf("create pod /tmp file: %v", err)
	}
	_ = podFile.Close()
	t.Cleanup(func() { _ = os.Remove(podFile.Name()) })

	got := runProbe(t, probeCmd(bin, workDir, "read:"+podFile.Name(), "write:"+filepath.Join(runTmp, "f"), "read:"+instructions, "write:"+instructions))
	if want := "read-denied\nwrite-ok\nread-ok\nwrite-denied"; got != want {
		t.Errorf("probe = %q, want %q", got, want)
	}
}

// A run executes only the harness and the platform binaries beside the runner.
// A system binary and copies in its workspace, TMPDIR or PATH are all refused.
func TestExecAllowList(t *testing.T) {
	requireLandlock(t, 1)
	bin := buildRunnerBinary(t)

	system, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true binary on this host")
	}
	workDir := filepath.Join(t.TempDir(), "run-1")
	runTmp := runfs.TmpDir(workDir)
	// A platform binary sits next to the runner.
	platformDir := filepath.Dir(bin)
	for _, dir := range []string{workDir, runTmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(system)
	if err != nil {
		t.Fatal(err)
	}
	copies := []string{filepath.Join(workDir, "tool"), filepath.Join(runTmp, "tool"), filepath.Join(platformDir, "kora-shell")}
	for _, p := range copies {
		if err := os.WriteFile(p, data, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	// A kora-runtime-mcp the agent puts on PATH is not the platform's.
	agentBin := filepath.Join(runTmp, "bin")
	if err := os.MkdirAll(agentBin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentBin, "kora-runtime-mcp"), data, 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := probeCmd(bin, workDir, "exec:"+system, "exec:"+copies[0], "exec:"+copies[1], "exec:"+filepath.Join(agentBin, "kora-runtime-mcp"), "exec:"+copies[2], "exec:"+os.Args[0])
	cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH")+string(os.PathListSeparator)+agentBin)
	got := runProbe(t, cmd)
	if want := "exec-denied\nexec-denied\nexec-denied\nexec-denied\nexec-ok\nexec-ok"; got != want {
		t.Errorf("probe = %q, want %q (system, workspace, TMPDIR, agent PATH, kora-shell, harness)", got, want)
	}
}

// An unknown mode is refused before the agent starts.
func TestUnknownModeRefused(t *testing.T) {
	requireLandlock(t, 1)
	bin := buildRunnerBinary(t)

	cmd := probeCmd(bin, t.TempDir(), "list:/")
	cmd.Env = append(cmd.Env, landlockmode.EnvVar+"=off")
	out, err := cmd.CombinedOutput()
	if err == nil || strings.Contains(string(out), "list-") || !strings.Contains(string(out), "agents.landlock") {
		t.Errorf("want a refusal naming agents.landlock before the agent starts, got: %s", out)
	}
}
