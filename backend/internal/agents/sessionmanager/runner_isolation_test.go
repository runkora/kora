package sessionmanager

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
)

// ── run isolation (per-run directories) ──────────────────────────────────────

// isolatedRun is the command and environment a started run's adapter got.
type isolatedRun struct {
	path string
	args []string
	env  []string
}

// startIsolated starts the Claude profile on the acpRunner against the fake
// agent and returns what it spawned, plus the workDir. A cleanup is registered
// to close the runner.
func startIsolated(t *testing.T, cfg ClaudeRunnerConfig, wrapperPath string) (isolatedRun, string) {
	t.Helper()
	workDir := t.TempDir()
	if cfg.RunID == "" {
		cfg.RunID = "isolation-test-run"
	}
	a := newFakeACPAgent(t)
	rc := testACPConfig(a, &claudeProfile{cfg: cfg})
	rc.WrapperPath = wrapperPath
	r := newACPRunner(rc)
	if err := r.Start(t.Context(), workDir); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return isolatedRun{path: a.lastBin, args: append([]string{a.lastBin}, a.lastArgs...), env: a.lastEnv}, workDir
}

func TestStart_RunHomeDirCreated(t *testing.T) {
	_, workDir := startIsolated(t, ClaudeRunnerConfig{}, "")
	runHome := filepath.Join(workDir, ".runhome")
	if info, err := os.Stat(runHome); err != nil || !info.IsDir() {
		t.Errorf(".runhome not created as a directory at %q", runHome)
	}
}

func TestStart_PerRunTmpDirCreated(t *testing.T) {
	// The per-run tmpdir is .tmp/{runID} beside the workspace, which kora-session-runner
	// grants and the sandbox mounts at /tmp. See docs/adr/2026-06-04-per-session-home-isolation.md.
	_, workDir := startIsolated(t, ClaudeRunnerConfig{}, "")
	tmpDir := runfs.TmpDir(workDir)
	if info, err := os.Stat(tmpDir); err != nil || !info.IsDir() {
		t.Errorf("per-run tmpdir not created as a directory at %q", tmpDir)
	}
}

func TestStart_TmpDirIsRunsOwnBesideWorkDir(t *testing.T) {
	// TMPDIR must point at the run's own tmp dir, not the pod's shared /tmp.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{}, "")
	wantTmp := filepath.Join(filepath.Dir(workDir), ".tmp", filepath.Base(workDir))
	got := envValue(r.env, "TMPDIR")
	if got != wantTmp {
		t.Errorf("TMPDIR = %q, want %q", got, wantTmp)
	}
}

// ── subprocess env isolation ──────────────────────────────────────────────────

func TestStart_HOMEPointsToRunHome(t *testing.T) {
	r, workDir := startIsolated(t, ClaudeRunnerConfig{}, "")
	wantHome := filepath.Join(workDir, ".runhome")
	assertEnvExact(t, r.env, "HOME", wantHome)
}

func TestStart_TMPDIRPointsToPerRunDir(t *testing.T) {
	r, workDir := startIsolated(t, ClaudeRunnerConfig{}, "")
	assertEnvExact(t, r.env, "TMPDIR", runfs.TmpDir(workDir))
}

func TestStart_InheritedHOMEInBaseEnvIsStripped(t *testing.T) {
	// The pod may have HOME= in its environment. It must not appear in the
	// subprocess env — only the per-run HOME should be present.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{}, "")
	wantHome := filepath.Join(workDir, ".runhome")
	assertEnvExact(t, r.env, "HOME", wantHome)
	assertEnvNoDuplicates(t, r.env, "HOME")
}

func TestStart_HOMEInExtraEnvIsStripped(t *testing.T) {
	// If ExtraEnv contains HOME= it must be stripped so it cannot shadow the
	// isolation value. Without this, getenv("HOME") on Linux would return the
	// ExtraEnv value (first match wins) and the ~/.claude/ separation breaks.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{
		ExtraEnv: []string{"HOME=/attacker/home"},
	}, "")
	wantHome := filepath.Join(workDir, ".runhome")
	assertEnvExact(t, r.env, "HOME", wantHome)
	assertEnvNoDuplicates(t, r.env, "HOME")
}

func TestStart_TMPDIRInExtraEnvIsStripped(t *testing.T) {
	// If ExtraEnv contains TMPDIR= it must be stripped so it cannot shadow the
	// per-run isolation value. Without this a rogue TMPDIR would point outside
	// the workspace and bypass the Landlock restriction.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{
		ExtraEnv: []string{"TMPDIR=/wrong/tmp"},
	}, "")
	assertEnvExact(t, r.env, "TMPDIR", runfs.TmpDir(workDir))
	assertEnvNoDuplicates(t, r.env, "TMPDIR")
}

func TestStart_OtherExtraEnvPreserved(t *testing.T) {
	// Non-isolation ExtraEnv entries (e.g. WORKSPACE=) must still be present.
	r, _ := startIsolated(t, ClaudeRunnerConfig{
		ExtraEnv: []string{"WORKSPACE=/workspaces/run-abc", "CUSTOM_VAR=hello"},
	}, "")
	assertEnvContains(t, r.env, "WORKSPACE=/workspaces/run-abc")
	assertEnvContains(t, r.env, "CUSTOM_VAR=hello")
}

// ── cross-run isolation ───────────────────────────────────────────────────────

func TestStart_SiblingRunsGetDistinctHOME(t *testing.T) {
	// Two concurrent runs must receive different HOME directories so that
	// ~/.claude/ session history is not shared between them.
	r1, workDir1 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-a"}, "")
	r2, workDir2 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-b"}, "")

	if workDir1 == workDir2 {
		t.Fatal("two runs received the same workDir — HOME isolation would be broken")
	}
	home1 := envValue(r1.env, "HOME")
	home2 := envValue(r2.env, "HOME")
	if home1 == home2 {
		t.Errorf("sibling runs share HOME=%q — ~/.claude/ leakage possible", home1)
	}
}

func TestStart_SiblingRunsGetDistinctTMPDIR(t *testing.T) {
	// Each run's workDir is a distinct t.TempDir(), so each gets its own .tmp/{runID}.
	r1, workDir1 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-a"}, "")
	r2, workDir2 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-b"}, "")

	tmp1 := envValue(r1.env, "TMPDIR")
	tmp2 := envValue(r2.env, "TMPDIR")
	if tmp1 == tmp2 {
		t.Errorf("sibling runs share TMPDIR=%q — /tmp side-channel possible", tmp1)
	}
	// Each TMPDIR must be its own run's tmp dir (the one Landlock grants that run).
	if tmp1 != runfs.TmpDir(workDir1) {
		t.Errorf("run-a TMPDIR=%q, want %q", tmp1, runfs.TmpDir(workDir1))
	}
	if tmp2 != runfs.TmpDir(workDir2) {
		t.Errorf("run-b TMPDIR=%q, want %q", tmp2, runfs.TmpDir(workDir2))
	}
}

// ── wrapper invocation ────────────────────────────────────────────────────────

func TestStart_WithoutWrapper_InvokesBinaryDirectly(t *testing.T) {
	r, _ := startIsolated(t, ClaudeRunnerConfig{}, "")
	// Without a wrapper the subprocess path must be the adapter itself.
	if r.path != claudeACPBinary {
		t.Errorf("path = %q, want %q (direct invocation, no wrapper)", r.path, claudeACPBinary)
	}
}

const testWrapper = "/usr/local/bin/kora-session-runner"

func TestStart_WithWrapper_CommandIsWrapper(t *testing.T) {
	r, _ := startIsolated(t, ClaudeRunnerConfig{}, testWrapper)
	if r.path != testWrapper {
		t.Errorf("path = %q, want wrapper %q", r.path, testWrapper)
	}
}

func TestStart_WithWrapper_ArgsLayout(t *testing.T) {
	// The wrapper must receive: <workDir> <adapter> [args...]
	// so kora-session-runner can apply Landlock and then exec the adapter.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{}, testWrapper)

	// r.args: [wrapperPath, workDir, adapter, ...]
	want := []string{testWrapper, workDir, claudeACPBinary}
	if !slices.Equal(r.args, want) {
		t.Errorf("wrapper args = %v, want %v", r.args, want)
	}
}

func TestStart_WithWrapper_TmpDirMatchesRunnerGrant(t *testing.T) {
	// kora-session-runner grants .tmp/{base} beside the workDir it receives as
	// argv[1]; TMPDIR must be exactly that directory or Landlock denies it.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{}, testWrapper)

	assertEnvExact(t, r.env, "HOME", filepath.Join(workDir, ".runhome"))
	wrapperWorkDir := r.args[1]
	assertEnvExact(t, r.env, "TMPDIR", filepath.Join(filepath.Dir(wrapperWorkDir), ".tmp", filepath.Base(wrapperWorkDir)))
}

// ── helpers ───────────────────────────────────────────────────────────────────

// assertEnvExact checks that key appears exactly once in env with the given value.
func assertEnvExact(t *testing.T, env []string, key, want string) {
	t.Helper()
	var found []string
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			found = append(found, v)
		}
	}
	switch len(found) {
	case 0:
		t.Errorf("env %s= not found in subprocess env", key)
	case 1:
		if found[0] != want {
			t.Errorf("env %s=%q, want %q", key, found[0], want)
		}
	default:
		t.Errorf("env %s= appears %d times: %v (want exactly one entry = %q)", key, len(found), found, want)
	}
}

// assertEnvNoDuplicates checks that key appears at most once in env.
func assertEnvNoDuplicates(t *testing.T, env []string, key string) {
	t.Helper()
	prefix := key + "="
	count := 0
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			count++
		}
	}
	if count > 1 {
		t.Errorf("env %s= appears %d times — duplicate would let an earlier value shadow the isolation entry", key, count)
	}
}

// assertEnvContains checks that the exact entry appears in env.
func assertEnvContains(t *testing.T, env []string, entry string) {
	t.Helper()
	if slices.Contains(env, entry) {
		return
	}
	t.Errorf("env entry %q not found in subprocess env", entry)
}

// envValue returns the value of key in env, or "" if absent.
func envValue(env []string, key string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}
