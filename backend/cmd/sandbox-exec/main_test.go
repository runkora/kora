package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSetRunHome_ScopesHomeUnderWorkspace locks that HOME is overridden to
// {WORKSPACE}/.runhome, mirroring orbit-session-runner's per-run home
// (docs/adr/2026-06-04-per-session-home-isolation.md) so kubeconfig and tool
// caches never land in the sandbox pod's shared default home. Part of the
// fix for https://github.com/clear-route/orbit/issues/134.
func TestSetRunHome_ScopesHomeUnderWorkspace(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("WORKSPACE", workspace)
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", originalHome) }) //nolint:errcheck

	if err := setRunHome(); err != nil {
		t.Fatalf("setRunHome: %v", err)
	}

	wantHome := filepath.Join(workspace, ".runhome")
	if got := os.Getenv("HOME"); got != wantHome {
		t.Errorf("HOME = %q, want %q", got, wantHome)
	}
	if fi, err := os.Stat(wantHome); err != nil || !fi.IsDir() {
		t.Errorf("run home dir not created at %q: %v", wantHome, err)
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
