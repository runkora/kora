package sessionmanager

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
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
	}
	for _, want := range always {
		if !containsFlag(flags, want) {
			t.Errorf("buildFlags() missing required flag %q; got %v", want, flags)
		}
	}
}

// bypassPermissions mode steers the model to Bash for file work, so agents skip
// Read, Edit and Write. Runs use dontAsk plus an allow-all hook instead.
func TestBuildFlags_DontAskMode(t *testing.T) {
	r := NewClaudeRunner(ClaudeRunnerConfig{
		Logger: logging.NewLoggerWithOutput(io.Discard),
		RunID:  "run-1",
	})
	flags := r.buildFlags()
	if containsFlag(flags, "--dangerously-skip-permissions") {
		t.Errorf("buildFlags() must not pass --dangerously-skip-permissions; got %v", flags)
	}
	i := slices.Index(flags, "--permission-mode")
	if i < 0 || i+1 >= len(flags) || flags[i+1] != "dontAsk" {
		t.Errorf("buildFlags() must pass --permission-mode dontAsk; got %v", flags)
	}
}

// dontAsk denies whatever is not approved, so the settings must carry a
// PreToolUse hook matching every tool that approves the call.
func TestBuildFlags_AllowAllHook(t *testing.T) {
	r := NewClaudeRunner(ClaudeRunnerConfig{
		Logger: logging.NewLoggerWithOutput(io.Discard),
		RunID:  "run-1",
	})
	flags := r.buildFlags()
	i := slices.Index(flags, "--settings")
	if i < 0 || i+1 >= len(flags) {
		t.Fatalf("buildFlags() missing --settings value; got %v", flags)
	}
	var settings struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(flags[i+1]), &settings); err != nil {
		t.Fatalf("--settings value is not valid JSON: %v (%q)", err, flags[i+1])
	}
	pre := settings.Hooks.PreToolUse
	if len(pre) != 1 || pre[0].Matcher != "*" || len(pre[0].Hooks) != 1 {
		t.Fatalf("hooks.PreToolUse = %+v, want one entry with matcher \"*\" and one hook", pre)
	}
	hook := pre[0].Hooks[0]
	if hook.Type != "command" {
		t.Errorf("hook type = %q, want command", hook.Type)
	}
	// Run the hook as Claude Code does, through the POSIX shell, and check what
	// it prints is the allow decision.
	out, err := exec.Command("/bin/sh", "-c", hook.Command).Output()
	if err != nil {
		t.Fatalf("running hook command %q: %v", hook.Command, err)
	}
	var decision struct {
		HookSpecificOutput struct {
			HookEventName      string `json:"hookEventName"`
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &decision); err != nil {
		t.Fatalf("hook output is not valid JSON: %v (%q)", err, out)
	}
	if got := decision.HookSpecificOutput; got.HookEventName != "PreToolUse" || got.PermissionDecision != "allow" {
		t.Errorf("hook output = %+v, want PreToolUse allow", got)
	}
}

// Every run must disable Claude Code's own commit trailer and PR footer:
// Orbit, not the subprocess's default settings, is the record of who ran an
// agent.
func TestBuildFlags_DisablesOwnAttribution(t *testing.T) {
	r := NewClaudeRunner(ClaudeRunnerConfig{
		Logger: logging.NewLoggerWithOutput(io.Discard),
		RunID:  "run-1",
	})
	flags := r.buildFlags()
	i := slices.Index(flags, "--settings")
	if i < 0 || i+1 >= len(flags) {
		t.Fatalf("buildFlags() missing --settings value; got %v", flags)
	}
	var settings struct {
		Attribution struct {
			Commit *string `json:"commit"`
			PR     *string `json:"pr"`
		} `json:"attribution"`
	}
	if err := json.Unmarshal([]byte(flags[i+1]), &settings); err != nil {
		t.Fatalf("--settings value is not valid JSON: %v (%q)", err, flags[i+1])
	}
	if settings.Attribution.Commit == nil || *settings.Attribution.Commit != "" {
		t.Errorf("attribution.commit = %v, want empty string", settings.Attribution.Commit)
	}
	if settings.Attribution.PR == nil || *settings.Attribution.PR != "" {
		t.Errorf("attribution.pr = %v, want empty string", settings.Attribution.PR)
	}
}

// Every run gets the full built-in tool set minus the platform exclusions: no
// --tools allow-list, and the exclusions passed as one --disallowedTools value.
func TestBuildFlags_ExcludedTools(t *testing.T) {
	r := NewClaudeRunner(ClaudeRunnerConfig{
		Logger: logging.NewLoggerWithOutput(io.Discard),
		RunID:  "run-1",
	})
	flags := r.buildFlags()
	if containsFlag(flags, "--tools") {
		t.Errorf("buildFlags() must not restrict tools with --tools; got %v", flags)
	}
	i := slices.Index(flags, "--disallowedTools")
	if i < 0 || i+1 >= len(flags) {
		t.Fatalf("buildFlags() missing --disallowedTools value; got %v", flags)
	}
	if got, want := flags[i+1], "WebSearch,WebFetch,AskUserQuestion,PushNotification,DesignSync"; got != want {
		t.Errorf("--disallowedTools = %q, want %q", got, want)
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
	// The per-run tmpdir is .tmp/{runID} beside the workspace, which orbit-session-runner
	// grants and the sandbox mounts at /tmp. See docs/adr/2026-06-04-per-session-home-isolation.md.
	_, workDir := startIsolated(t, ClaudeRunnerConfig{})
	tmpDir := runfs.TmpDir(workDir)
	if info, err := os.Stat(tmpDir); err != nil || !info.IsDir() {
		t.Errorf("per-run tmpdir not created as a directory at %q", tmpDir)
	}
}

func TestStart_TmpDirIsRunsOwnBesideWorkDir(t *testing.T) {
	// TMPDIR must point at the run's own tmp dir, not the pod's shared /tmp.
	r, workDir := startIsolated(t, ClaudeRunnerConfig{})
	wantTmp := filepath.Join(filepath.Dir(workDir), ".tmp", filepath.Base(workDir))
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
	assertEnvExact(t, r.cmd.Env, "TMPDIR", runfs.TmpDir(workDir))
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
	assertEnvExact(t, r.cmd.Env, "TMPDIR", runfs.TmpDir(workDir))
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

func TestBuildRunEnv_RunNodeOptionsReplacesInherited(t *testing.T) {
	// The per-run heap cap must win over a NODE_OPTIONS the pod carries, or
	// Node would read whichever came first.
	r := &ClaudeRunner{cfg: ClaudeRunnerConfig{ExtraEnv: []string{"NODE_OPTIONS=--max-old-space-size=512"}}}
	env := r.buildRunEnv([]string{"NODE_OPTIONS=--max-old-space-size=8192"}, "/home", "/tmp")
	assertEnvExact(t, env, "NODE_OPTIONS", "--max-old-space-size=512")
	assertEnvNoDuplicates(t, env, "NODE_OPTIONS")
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
	// Each run's workDir is a distinct t.TempDir(), so each gets its own .tmp/{runID}.
	r1, workDir1 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-a"})
	r2, workDir2 := startIsolated(t, ClaudeRunnerConfig{RunID: "run-b"})

	tmp1 := envValue(r1.cmd.Env, "TMPDIR")
	tmp2 := envValue(r2.cmd.Env, "TMPDIR")
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

func TestStart_WithWrapper_TmpDirMatchesRunnerGrant(t *testing.T) {
	// orbit-session-runner grants .tmp/{base} beside the workDir it receives as
	// argv[1]; TMPDIR must be exactly that directory or Landlock denies it.
	truePath := stubBinary(t)
	r, workDir := startIsolated(t, ClaudeRunnerConfig{
		WrapperPath: truePath,
	})

	assertEnvExact(t, r.cmd.Env, "HOME", filepath.Join(workDir, ".runhome"))
	wrapperWorkDir := r.cmd.Args[1]
	assertEnvExact(t, r.cmd.Env, "TMPDIR", filepath.Join(filepath.Dir(wrapperWorkDir), ".tmp", filepath.Base(wrapperWorkDir)))
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
