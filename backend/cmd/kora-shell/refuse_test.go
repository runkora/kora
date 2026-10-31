package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain runs main in a child process when asked, so a test can see its exit.
func TestMain(m *testing.M) {
	if os.Getenv("KORA_SHELL_TEST_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// runShell runs kora-shell with args against a session-manager that reports no
// sandbox, and returns its exit code and stderr.
func runShell(t *testing.T, workspace string, args ...string) (int, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(),
		"KORA_SHELL_TEST_MAIN=1",
		"WORKSPACE="+workspace,
		"KORA_SESSION_MANAGER_URL="+srv.URL,
		"KORA_RUN_ID=run-1",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("run kora-shell: %v", err)
	}
	return cmd.ProcessState.ExitCode(), stderr.String()
}

// Without a sandbox the command is refused and nothing runs in this container.
func TestNoSandboxRefusesCommand(t *testing.T) {
	ws := t.TempDir()
	marker := filepath.Join(ws, "ran")

	code, stderr := runShell(t, ws, "-c", "-l", "touch "+marker)
	if code == 0 {
		t.Fatalf("exit code 0, want a refusal (stderr: %s)", stderr)
	}
	if !strings.Contains(stderr, errNoSandbox) {
		t.Errorf("stderr = %q, want %q", stderr, errNoSandbox)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the command ran in this container")
	}
}

// An interactive shell is refused even when the run has a sandbox.
func TestInteractiveShellRefused(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".sandbox-exec")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"endpoint": "127.0.0.1:1", "exec-token": "tok-1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code, stderr := runShell(t, ws, "-l")
	if code == 0 || !strings.Contains(stderr, errInteractive) {
		t.Errorf("exit %d, stderr %q, want a refusal with %q", code, stderr, errInteractive)
	}
}
