package sessionmanager

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Every framework goes through orbit-session-runner, so a run under Codex or
// OpenCode is confined the same way a Claude run is. Before this, only the Claude
// runner was wrapped and the other two executed tool commands in the
// session-manager container with no Landlock and no PID namespace.
// See docs/adr/2026-09-18-sandbox-exec-trust-boundary.md and
// https://github.com/clear-route/orbit/issues/217.
func TestWrapExec(t *testing.T) {
	const (
		wrapper = "/usr/local/bin/orbit-session-runner"
		workDir = "/workspaces/run-1"
		binary  = "codex"
	)

	t.Run("wrapper set puts workDir then binary ahead of the args", func(t *testing.T) {
		cmd, args := wrapExec(wrapper, workDir, binary, "app-server", "--listen", "stdio://")
		if cmd != wrapper {
			t.Errorf("cmd = %q, want the wrapper %q", cmd, wrapper)
		}
		want := []string{workDir, binary, "app-server", "--listen", "stdio://"}
		if !slices.Equal(args, want) {
			t.Errorf("args = %v, want %v", args, want)
		}
	})

	t.Run("wrapper unset runs the binary directly", func(t *testing.T) {
		cmd, args := wrapExec("", workDir, binary, "app-server")
		if cmd != binary {
			t.Errorf("cmd = %q, want %q", cmd, binary)
		}
		if !slices.Equal(args, []string{"app-server"}) {
			t.Errorf("args = %v, want [app-server]", args)
		}
	})
}

// OpenCode takes the shell its tool commands run through from opencode.json, so
// this is what routes an OpenCode run's commands into the sandbox rather than the
// session-manager container.
func TestWriteOpenCodeConfig_ShellPath(t *testing.T) {
	const shellPath = "/opt/orbit/shell/bash"

	read := func(t *testing.T, cfg OpenCodeRunnerConfig) map[string]any {
		t.Helper()
		workDir := t.TempDir()
		cfg.Logger = logging.NewLoggerWithOutput(io.Discard)
		cfg.RunID = "run-1"
		cfg.InferenceGatewayURL = "http://orbit-inference-gateway"
		r := NewOpenCodeRunner(cfg)
		if err := r.writeOpenCodeConfig(workDir); err != nil {
			t.Fatalf("writeOpenCodeConfig: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(workDir, "opencode.json"))
		if err != nil {
			t.Fatalf("read opencode.json: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("unmarshal opencode.json: %v", err)
		}
		return out
	}

	t.Run("shell path set", func(t *testing.T) {
		cfg := read(t, OpenCodeRunnerConfig{ShellPath: shellPath})
		if got := cfg["shell"]; got != shellPath {
			t.Errorf("shell = %v, want %q", got, shellPath)
		}
	})

	t.Run("shell path unset leaves the key out", func(t *testing.T) {
		cfg := read(t, OpenCodeRunnerConfig{})
		if _, present := cfg["shell"]; present {
			t.Error("expected no shell key when ShellPath is empty")
		}
	})
}

// orbit-shell is a child of the agent subprocess and reads all three of these
// from its environment: WORKSPACE to find the sandbox descriptor, and the other
// two to ask this pod for a replacement when the sandbox pod has gone.
func TestRunEnv(t *testing.T) {
	c := &Controller{cfg: ControllerConfig{WorkspacesBasePath: "/workspaces", SelfPort: 8090}}

	got := c.runEnv("run-42")
	want := []string{
		"WORKSPACE=/workspaces/run-42",
		"ORBIT_SESSION_MANAGER_URL=http://localhost:8090",
		"ORBIT_RUN_ID=run-42",
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("run env %v is missing %q", got, w)
		}
	}
}
