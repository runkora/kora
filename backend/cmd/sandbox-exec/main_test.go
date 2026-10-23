package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/agents/sandbox/execauth"
	execpb "github.com/runkora/kora/backend/internal/agents/sandbox/proto"
	"google.golang.org/grpc"
)

// TestFilterEnv_BlocksHijackVariables locks that env vars capable of hijacking
// subprocess behavior (dynamic linker, allocator, executable lookup, shell
// swap) are stripped from a request's env before being appended to the
// subprocess environment. Part of the fix for
// https://github.com/runkora/kora/issues/231.
func TestFilterEnv_BlocksHijackVariables(t *testing.T) {
	blocked := []string{
		"LD_PRELOAD=/tmp/evil.so",
		"LD_LIBRARY_PATH=/tmp/evil",
		"DYLD_INSERT_LIBRARIES=/tmp/evil.dylib",
		"MALLOC_CHECK_=0",
		"PATH=/tmp/evil:/usr/bin",
		"SHELL=/tmp/evil-shell",
		"IFS= ",
		"ENV=/tmp/evil.sh",
		"BASH_ENV=/tmp/evil.sh",
		"CDPATH=/tmp/evil",
		// case-insensitive match
		"ld_preload=/tmp/evil.so",
	}

	got := filterEnv(blocked)
	if len(got) != 0 {
		t.Errorf("filterEnv(%v) = %v, want empty (all entries should be blocked)", blocked, got)
	}
}

// TestFilterEnv_AllowsSafeVariables locks that ordinary configuration env vars
// (not on the hijack denylist) pass through unmodified.
func TestFilterEnv_AllowsSafeVariables(t *testing.T) {
	safe := []string{
		"WORKSPACE=/workspaces/run-abc",
		"ANTHROPIC_BASE_URL=https://api.example.com",
		"CUSTOM_VAR=hello",
		"HOME=/home/kora",
	}

	got := filterEnv(safe)
	if len(got) != len(safe) {
		t.Fatalf("filterEnv(%v) = %v, want all %d entries preserved", safe, got, len(safe))
	}
	for i, want := range safe {
		if got[i] != want {
			t.Errorf("filterEnv(...)[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// TestSetRunHome_PutsHomeOnLocalDisk locks that HOME, TMPDIR and the caches
// sit on the sandbox's local disk, off the shared workspace volume
// (docs/adr/2026-10-08-sandbox-workspace-mount.md).
func TestSetRunHome_PutsHomeOnLocalDisk(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "run-1")
	t.Setenv("WORKSPACE", workspace)
	for _, key := range []string{"HOME", "TMPDIR", "NPM_CONFIG_PREFIX", "NPM_CONFIG_CACHE", "PYTHONUSERBASE", "PIP_BREAK_SYSTEM_PACKAGES", "PIP_CACHE_DIR", "GOPATH", "GOMODCACHE", "GOCACHE", "CARGO_HOME", "PATH"} {
		t.Setenv(key, os.Getenv(key))
	}

	if err := setRunHome(); err != nil {
		t.Fatalf("setRunHome: %v", err)
	}

	local := filepath.Join(filepath.Dir(workspace), ".runlocal", "run-1")
	for key, want := range map[string]string{
		"HOME":    local,
		"TMPDIR":  "/tmp",
		"GOCACHE": filepath.Join(local, ".cache", "go-build"),
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if fi, err := os.Stat(local); err != nil || !fi.IsDir() {
		t.Errorf("home dir not created at %q: %v", local, err)
	}
	// Nothing is created in the workspace: a sandbox's HOME is not carried between sandboxes.
	if _, err := os.Stat(filepath.Join(workspace, ".runhome")); !os.IsNotExist(err) {
		t.Errorf("workspace .runhome exists or errored: %v", err)
	}
}

// TestSetRunHome_NoWorkspaceIsNoop locks that a missing WORKSPACE env var
// (e.g. local/dev invocation outside a sandbox pod) leaves HOME untouched
// instead of erroring.
func TestSetRunHome_NoWorkspaceIsNoop(t *testing.T) {
	t.Setenv("WORKSPACE", "")
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", originalHome) }) //nolint:errcheck

	if err := setRunHome(); err != nil {
		t.Fatalf("setRunHome: %v", err)
	}
	if got := os.Getenv("HOME"); got != originalHome {
		t.Errorf("HOME = %q, want unchanged %q", got, originalHome)
	}
}

// fakeExecStream collects a command's stdout from Execute.
type fakeExecStream struct {
	grpc.ServerStream
	stdout bytes.Buffer
}

func (f *fakeExecStream) Context() context.Context { return context.Background() }

func (f *fakeExecStream) Send(r *execpb.ExecuteResponse) error {
	f.stdout.Write(r.GetStdout())
	return nil
}

func TestExecute_CommandEnvOmitsServerSettings(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv(execauth.TokenEnv, "secret-exec-token")
	t.Setenv("SANDBOX_EXEC_PORT", "50051")
	t.Setenv("WORKSPACE_PROBE", "kept")

	stream := &fakeExecStream{}
	if err := (&execServer{}).Execute(&execpb.ExecuteRequest{Command: "env"}, stream); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	env := stream.stdout.String()
	for _, key := range []string{execauth.TokenEnv, "SANDBOX_EXEC_PORT"} {
		if strings.Contains(env, key+"=") {
			t.Errorf("command env contains %s", key)
		}
	}
	if !strings.Contains(env, "WORKSPACE_PROBE=kept") {
		t.Error("command env lost the pod environment")
	}
}
