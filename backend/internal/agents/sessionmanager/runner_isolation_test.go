package sessionmanager

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ── buildFlags ────────────────────────────────────────────────────────────────

func TestBuildFlags_BaseFlags(t *testing.T) {
	r := NewClaudeRunner(ClaudeRunnerConfig{
		Logger: logging.NewLoggerWithOutput(io.Discard),
		RunID:  "run-1",
	})
	flags := r.buildFlags()
	always := []string{
		"--print",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--dangerously-skip-permissions",
		"--tools",
	}
	for _, want := range always {
		if !containsFlag(flags, want) {
			t.Errorf("buildFlags() missing required flag %q; got %v", want, flags)
		}
	}
}

func TestBuildFlags_Options(t *testing.T) {
	cases := []struct {
		name   string
		cfg    ClaudeRunnerConfig
		want   []string // flags that must appear
		absent []string // flags that must not appear
	}{
		{
			name: "resume adds --resume with session ID",
			cfg:  ClaudeRunnerConfig{ResumeSessionID: "sess-abc"},
			want: []string{"--resume", "sess-abc"},
		},
		{
			name:   "no resume when empty",
			cfg:    ClaudeRunnerConfig{},
			absent: []string{"--resume"},
		},
		{
			name: "bare adds --bare",
			cfg:  ClaudeRunnerConfig{Bare: true},
			want: []string{"--bare"},
		},
		{
			name:   "no bare when false",
			cfg:    ClaudeRunnerConfig{Bare: false},
			absent: []string{"--bare"},
		},
		{
			name: "model adds --model with value",
			cfg:  ClaudeRunnerConfig{Model: "claude-opus-4"},
			want: []string{"--model", "claude-opus-4"},
		},
		{
			name:   "no model when empty",
			cfg:    ClaudeRunnerConfig{},
			absent: []string{"--model"},
		},
		{
			name: "system prompt adds --append-system-prompt",
			cfg:  ClaudeRunnerConfig{SystemPrompt: "you are helpful"},
			want: []string{"--append-system-prompt", "you are helpful"},
		},
		{
			name:   "no system prompt when empty",
			cfg:    ClaudeRunnerConfig{},
			absent: []string{"--append-system-prompt"},
		},
		{
			name: "all optional flags combined",
			cfg: ClaudeRunnerConfig{
				ResumeSessionID: "sess-xyz",
				Bare:            true,
				Model:           "claude-haiku",
				SystemPrompt:    "be concise",
			},
			want: []string{"--resume", "sess-xyz", "--bare", "--model", "claude-haiku", "--append-system-prompt", "be concise"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Logger = logging.NewLoggerWithOutput(io.Discard)
			tc.cfg.RunID = "run-1"
			r := NewClaudeRunner(tc.cfg)
			flags := r.buildFlags()

			for _, w := range tc.want {
				if !containsFlag(flags, w) {
					t.Errorf("buildFlags() missing %q; got %v", w, flags)
				}
			}
			for _, a := range tc.absent {
				if containsFlag(flags, a) {
					t.Errorf("buildFlags() should not contain %q; got %v", a, flags)
				}
			}
		})
	}
}

// ── run isolation (per-run directories) ──────────────────────────────────────

// stubBinary returns the absolute path to a binary that exits with code 0
// immediately (ignoring all arguments). Skips the calling test if not found.
func stubBinary(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("true")
	if err != nil {
		t.Skip("'true' not in PATH; skipping subprocess isolation test")
	}
	return p
}

// startIsolated starts a ClaudeRunner with the stub binary and returns it plus
// the workDir. A cleanup is registered to close the runner.
func startIsolated(t *testing.T, cfg ClaudeRunnerConfig) (*ClaudeRunner, string) {
	t.Helper()
	workDir := t.TempDir()
	cfg.BinaryPath = stubBinary(t)
	cfg.Logger = logging.NewLoggerWithOutput(io.Discard)
	if cfg.RunID == "" {
		cfg.RunID = "isolation-test-run"
	}
	r := NewClaudeRunner(cfg)
	if err := r.Start(context.Background(), workDir); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, workDir
}

func TestStart_RunHomeDirCreated(t *testing.T) {
	_, workDir := startIsolated(t, ClaudeRunnerConfig{})
	runHome := filepath.Join(workDir, ".runhome")
	if info, err := os.Stat(runHome); err != nil || !info.IsDir() {
		t.Errorf(".runhome not created as a directory at %q", runHome)
	}
}

func TestStart_PerRunTmpDirCreated(t *testing.T) {
	// The per-run tmpdir must live inside the workspace at {workDir}/.tmp so that
	// the Landlock RWDirs(workDir) rule covers it automatically. See docs/adr/2026-06-04-per-session-home-isolation.md.
	_, workDir := startIsolated(t, ClaudeRunnerConfig{})
	tmpDir := filepath.Join(workDir, ".tmp")
	if info, err := os.Stat(tmpDir); err != nil || !info.IsDir() {
		t.Errorf("per-run tmpdir not created as a directory at %q", tmpDir)
	}
}

func TestStart_TmpDirInsideWorkDir(t *testing.T) {
	// TMPDIR must point inside the workspace, not under /tmp, so Landlock covers it.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{})
	wantTmp := filepath.Join(workDir, ".tmp")
	got := envValue(r.cmd.Env, "TMPDIR")
	if got != wantTmp {
		t.Errorf("TMPDIR = %q, want %q", got, wantTmp)
	}
}

// ── subprocess env isolation ──────────────────────────────────────────────────

func TestStart_HOMEPointsToRunHome(t *testing.T) {
	r, workDir := startIsolated(t, ClaudeRunnerConfig{})
	wantHome := filepath.Join(workDir, ".runhome")
	assertEnvExact(t, r.cmd.Env, "HOME", wantHome)
}

func TestStart_TMPDIRPointsToPerRunDir(t *testing.T) {
	r, workDir := startIsolated(t, ClaudeRunnerConfig{})
	assertEnvExact(t, r.cmd.Env, "TMPDIR", filepath.Join(workDir, ".tmp"))
}

func TestStart_InheritedHOMEInBaseEnvIsStripped(t *testing.T) {
	// The pod may have HOME= in its environment. It must not appear in the
	// subprocess env — only the per-run HOME should be present.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{})
	wantHome := filepath.Join(workDir, ".runhome")
	assertEnvExact(t, r.cmd.Env, "HOME", wantHome)
	assertEnvNoDuplicates(t, r.cmd.Env, "HOME")
}

func TestStart_HOMEInExtraEnvIsStripped(t *testing.T) {
	// If ExtraEnv contains HOME= it must be stripped so it cannot shadow the
	// isolation value. Without this, getenv("HOME") on Linux would return the
	// ExtraEnv value (first match wins) and the ~/.claude/ separation breaks.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{
		ExtraEnv: []string{"HOME=/attacker/home"},
	})
	wantHome := filepath.Join(workDir, ".runhome")
	assertEnvExact(t, r.cmd.Env, "HOME", wantHome)
	assertEnvNoDuplicates(t, r.cmd.Env, "HOME")
}

func TestStart_TMPDIRInExtraEnvIsStripped(t *testing.T) {
	// If ExtraEnv contains TMPDIR= it must be stripped so it cannot shadow the
	// per-run isolation value. Without this a rogue TMPDIR would point outside
	// the workspace and bypass the Landlock restriction.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{
		ExtraEnv: []string{"TMPDIR=/wrong/tmp"},
	})
	assertEnvExact(t, r.cmd.Env, "TMPDIR", filepath.Join(workDir, ".tmp"))
	assertEnvNoDuplicates(t, r.cmd.Env, "TMPDIR")
}

func TestStart_OtherExtraEnvPreserved(t *testing.T) {
	// Non-isolation ExtraEnv entries (e.g. WORKSPACE=) must still be present.
	r, _ := startIsolated(t, ClaudeRunnerConfig{
		ExtraEnv: []string{"WORKSPACE=/workspaces/run-abc", "CUSTOM_VAR=hello"},
	})
	assertEnvContains(t, r.cmd.Env, "WORKSPACE=/workspaces/run-abc")
	assertEnvContains(t, r.cmd.Env, "CUSTOM_VAR=hello")
}

// ── cross-run isolation ───────────────────────────────────────────────────────

func TestStart_SiblingRunsGetDistinctHOME(t *testing.T) {
	// Two concurrent runs must receive different HOME directories so that
	// ~/.claude/ session history is not shared between them.
	r1, workDir1 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-a"})
	r2, workDir2 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-b"})

	if workDir1 == workDir2 {
		t.Fatal("two runs received the same workDir — HOME isolation would be broken")
	}
	home1 := envValue(r1.cmd.Env, "HOME")
	home2 := envValue(r2.cmd.Env, "HOME")
	if home1 == home2 {
		t.Errorf("sibling runs share HOME=%q — ~/.claude/ leakage possible", home1)
	}
}

func TestStart_SiblingRunsGetDistinctTMPDIR(t *testing.T) {
	// Each run's workDir is a distinct t.TempDir(), so .tmp inside each is distinct.
	r1, workDir1 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-a"})
	r2, workDir2 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-b"})

	tmp1 := envValue(r1.cmd.Env, "TMPDIR")
	tmp2 := envValue(r2.cmd.Env, "TMPDIR")
	if tmp1 == tmp2 {
		t.Errorf("sibling runs share TMPDIR=%q — /tmp side-channel possible", tmp1)
	}
	// Each TMPDIR must be inside its own workDir (Landlock coverage guarantee).
	if !strings.HasPrefix(tmp1, workDir1) {
		t.Errorf("run-a TMPDIR=%q is not inside workDir=%q", tmp1, workDir1)
	}
	if !strings.HasPrefix(tmp2, workDir2) {
		t.Errorf("run-b TMPDIR=%q is not inside workDir=%q", tmp2, workDir2)
	}
}

// ── wrapper invocation ────────────────────────────────────────────────────────

func TestStart_WithoutWrapper_InvokesBinaryDirectly(t *testing.T) {
	truePath := stubBinary(t)
	r, _ := startIsolated(t, ClaudeRunnerConfig{})
	// Without WrapperPath the subprocess path must be the claude binary itself.
	if r.cmd.Path != truePath {
		t.Errorf("cmd.Path = %q, want %q (direct invocation, no wrapper)", r.cmd.Path, truePath)
	}
}

func TestStart_WithWrapper_CommandIsWrapper(t *testing.T) {
	truePath := stubBinary(t)
	r, _ := startIsolated(t, ClaudeRunnerConfig{
		WrapperPath: truePath, // use "true" as a stand-in wrapper
	})
	if r.cmd.Path != truePath {
		t.Errorf("cmd.Path = %q, want wrapper %q", r.cmd.Path, truePath)
	}
}

func TestStart_WithWrapper_ArgsLayout(t *testing.T) {
	// The wrapper must receive: <workDir> <claudeBinary> [claude-flags...]
	// so orbit-session-runner can apply Landlock and then exec claude.
	truePath := stubBinary(t)
	r, workDir := startIsolated(t, ClaudeRunnerConfig{
		WrapperPath: truePath,
	})

	// r.cmd.Args: [wrapperPath, workDir, claudeBinary, ...claude-flags...]
	args := r.cmd.Args
	if len(args) < 4 {
		t.Fatalf("wrapper args too short (len=%d): %v", len(args), args)
	}
	if args[1] != workDir {
		t.Errorf("args[1] (workDir) = %q, want %q", args[1], workDir)
	}
	if args[2] == "" {
		t.Error("args[2] (claude binary) must not be empty")
	}
	// The rest should be claude flags starting with --print.
	if args[3] != "--print" {
		t.Errorf("args[3] (first claude flag) = %q, want --print", safeIndex(args, 3))
	}
}

func TestStart_WithWrapper_TmpDirInsideWorkDir(t *testing.T) {
	// TMPDIR must be inside the workDir passed as args[1] to the wrapper so that
	// the Landlock RWDirs(workDir) rule covers the tmp directory. See docs/adr/2026-06-04-per-session-home-isolation.md.
	truePath := stubBinary(t)
	r, workDir := startIsolated(t, ClaudeRunnerConfig{
		WrapperPath: truePath,
	})

	wantHome := filepath.Join(workDir, ".runhome")
	wantTmp := filepath.Join(workDir, ".tmp")

	assertEnvExact(t, r.cmd.Env, "HOME", wantHome)
	assertEnvExact(t, r.cmd.Env, "TMPDIR", wantTmp)

	// TMPDIR must be inside the workDir that orbit-session-runner receives as argv[1].
	wrapperWorkDir := r.cmd.Args[1]
	if !strings.HasPrefix(wantTmp, wrapperWorkDir) {
		t.Errorf("TMPDIR %q is not inside wrapper argv[1] workDir %q — Landlock would not cover it", wantTmp, wrapperWorkDir)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func containsFlag(flags []string, want string) bool {
	return slices.Contains(flags, want)
}

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

func safeIndex(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "<out of bounds>"
}
