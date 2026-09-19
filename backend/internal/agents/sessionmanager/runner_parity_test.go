package sessionmanager

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Every framework goes through orbit-session-runner, so Codex and OpenCode runs
// are confined the same way a Claude run is.
// See https://github.com/clear-route/orbit/issues/217.
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

// OpenCode takes the shell its tool commands run through from opencode.json.
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

// orbit-shell reads all three from its environment: WORKSPACE to find the
// sandbox descriptor, the others to ask this pod for a replacement.
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

// Without the runtime MCP server a run cannot ask for a sandbox, and without an
// entry per governed server its mcp_tools grants do nothing.
// See https://github.com/clear-route/orbit/issues/217.
func TestCodexConfig_MCPServers(t *testing.T) {
	workDir := t.TempDir()
	r := NewCodexRunner(CodexRunnerConfig{
		Logger:              logging.NewLoggerWithOutput(io.Discard),
		RunID:               "run-1",
		Model:               "anthropic/claude-sonnet-4-6",
		InferenceGatewayURL: "http://orbit-inference-gateway",
		SessionManagerURL:   "http://localhost:8090",
		ToolGatewayURL:      "http://orbit-tool-gateway/run/run-1",
		MCPServers:          []string{"github"},
	})
	codexHome, err := r.writeCodexConfig(workDir)
	if err != nil {
		t.Fatalf("writeCodexConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	got := string(data)

	// sandbox_mode is a top-level key: TOML puts a key after a table header inside
	// that table, which silently disabled it and brought the panic back.
	if idx := strings.Index(got, "["); idx >= 0 {
		if strings.Index(got, "sandbox_mode") > idx {
			t.Error("sandbox_mode must appear before the first table header, or it is nested in that table")
		}
	}

	for _, want := range []string{
		// Without this Codex panics on a missing bubblewrap and runs nothing.
		`sandbox_mode = "danger-full-access"`,
		// The one-shot shell tool, which is the shape orbit-shell implements.
		"unified_exec = false",
		"[mcp_servers.runtime]",
		`command = "orbit-runtime-mcp"`,
		`ORBIT_SESSION_MANAGER_URL = "http://localhost:8090"`,
		`ORBIT_RUN_ID              = "run-1"`,
		"[mcp_servers.github]",
		`url = "http://orbit-tool-gateway/run/run-1/v1/mcp/github"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config.toml is missing %q:\n%s", want, got)
		}
	}
}

func TestOpenCodeConfig_MCPServers(t *testing.T) {
	workDir := t.TempDir()
	r := NewOpenCodeRunner(OpenCodeRunnerConfig{
		Logger:              logging.NewLoggerWithOutput(io.Discard),
		RunID:               "run-1",
		InferenceGatewayURL: "http://orbit-inference-gateway",
		SessionManagerURL:   "http://localhost:8090",
		ToolGatewayURL:      "http://orbit-tool-gateway/run/run-1",
		MCPServers:          []string{"github"},
	})
	if err := r.writeOpenCodeConfig(workDir); err != nil {
		t.Fatalf("writeOpenCodeConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	var cfg struct {
		MCP map[string]struct {
			Type        string            `json:"type"`
			Command     []string          `json:"command"`
			URL         string            `json:"url"`
			Enabled     bool              `json:"enabled"`
			Environment map[string]string `json:"environment"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal opencode.json: %v", err)
	}

	rt, ok := cfg.MCP["runtime"]
	if !ok {
		t.Fatalf("no runtime MCP server in %v", cfg.MCP)
	}
	if rt.Type != "local" || len(rt.Command) != 1 || rt.Command[0] != "orbit-runtime-mcp" || !rt.Enabled {
		t.Errorf("runtime entry = %+v", rt)
	}
	if rt.Environment["ORBIT_RUN_ID"] != "run-1" || rt.Environment["ORBIT_SESSION_MANAGER_URL"] != "http://localhost:8090" {
		t.Errorf("runtime environment = %v", rt.Environment)
	}

	gh, ok := cfg.MCP["github"]
	if !ok {
		t.Fatalf("no github MCP server in %v", cfg.MCP)
	}
	if gh.Type != "remote" || gh.URL != "http://orbit-tool-gateway/run/run-1/v1/mcp/github" || !gh.Enabled {
		t.Errorf("github entry = %+v", gh)
	}
}
