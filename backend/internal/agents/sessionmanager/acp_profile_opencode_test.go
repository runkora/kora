package sessionmanager

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func testOpenCodeProfile() *openCodeProfile {
	return &openCodeProfile{cfg: OpenCodeRunnerConfig{
		InferenceGatewayURL: "http://inference-gateway.test",
		Model:               "anthropic/claude-sonnet-5",
		Logger:              logging.NewLoggerWithOutput(io.Discard),
		RunID:               "run-1",
		ShellPath:           "/usr/local/bin/kora-shell",
		SystemPrompt:        "Be brief.",
		SessionManagerURL:   "http://localhost:8090",
		ToolGatewayURL:      "http://tool-gateway.test/run/run-1/",
		MCPServers:          []string{"github"},
		ExtraEnv:            []string{"WORKSPACE=/w", "HOME=/evil"},
	}}
}

// The adapter runs through the sandbox wrapper with the run's environment, and
// session/new carries the MCP servers the config no longer declares.
func TestOpenCodeProfile_Launch(t *testing.T) {
	a := newFakeACPAgent(t)
	workDir := t.TempDir()
	cfg := testACPConfig(a, testOpenCodeProfile())
	cfg.WrapperPath = "/usr/local/bin/kora-session-runner"
	r := newACPRunner(cfg)
	if err := r.Start(t.Context(), workDir); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if want := []string{workDir, "opencode", "acp", "--cwd", workDir}; a.lastBin != cfg.WrapperPath || !slices.Equal(a.lastArgs, want) {
		t.Errorf("command = %s %v, want the wrapper and %v", a.lastBin, a.lastArgs, want)
	}
	env := map[string]string{}
	for _, e := range a.lastEnv {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	if env["OPENCODE_DISABLE_PROJECT_CONFIG"] != "1" || env["HOME"] != runfs.ConfigDir(workDir) || env["WORKSPACE"] != "/w" {
		t.Errorf("env PROJECT_CONFIG=%q HOME=%q WORKSPACE=%q", env["OPENCODE_DISABLE_PROJECT_CONFIG"], env["HOME"], env["WORKSPACE"])
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &config); err != nil {
		t.Fatalf("config: %v", err)
	}
	if _, ok := config["mcp"]; ok {
		t.Errorf("config declares MCP servers: %v", config["mcp"])
	}
	if config["shell"] != "/usr/local/bin/kora-shell" || config["model"] != "kora/anthropic/claude-sonnet-5" {
		t.Errorf("config shell = %v model = %v", config["shell"], config["model"])
	}
	instructions, _ := config["instructions"].([]any)
	if len(instructions) != 2 || instructions[0] != runfs.ConfigDir(workDir)+"/instructions.md" {
		t.Errorf("instructions = %v, want the system prompt file first", instructions)
	}

	var req acp.NewSessionRequest
	if err := json.Unmarshal(a.waitMethod("session/new").Params, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.McpServers) != 2 || req.Cwd != workDir {
		t.Fatalf("session/new = %+v, want the runtime and github servers", req)
	}
	rt, gh := req.McpServers[0].Stdio, req.McpServers[1].Http
	if rt == nil || rt.Name != "runtime" || rt.Command != "kora-runtime-mcp" {
		t.Fatalf("runtime server = %+v", rt)
	}
	rtEnv := map[string]string{}
	for _, e := range rt.Env {
		rtEnv[e.Name] = e.Value
	}
	if rtEnv["KORA_RUN_ID"] != "run-1" || rtEnv["KORA_SESSION_MANAGER_URL"] != "http://localhost:8090" {
		t.Errorf("runtime server env = %v", rtEnv)
	}
	if gh == nil || gh.Name != "github" || gh.Url != "http://tool-gateway.test/run/run-1/v1/mcp/github" {
		t.Errorf("github server = %+v", gh)
	}
	if a.count("session/set_config_option") != 0 {
		t.Error("the model is pinned in the config; no session option is set")
	}
}

func TestOpenCodeProfile_PrepareValidates(t *testing.T) {
	p := testOpenCodeProfile()
	p.cfg.Model = ""
	if _, err := p.Prepare(t.TempDir()); err == nil {
		t.Error("Prepare without a model succeeded")
	}
}

func TestOpenCodeProfile_ClassifyError(t *testing.T) {
	p := testOpenCodeProfile()
	tests := []struct {
		turn acpTurnEnd
		want string
	}{
		// The gateway's 403 as OpenCode 1.19.5 reports it (spike run ocO1).
		{acpTurnEnd{RPCErr: &acp.RequestError{Code: -32603, Message: "Internal error: Forbidden: budget exceeded: a spending ceiling for this principal or run has been reached"}}, "budget_exhausted"},
		{acpTurnEnd{RPCErr: &acp.RequestError{Code: -32603, Message: "Internal error: API Error: 500"}}, ""},
		{acpTurnEnd{RPCErr: &acp.RequestError{Code: -32000, Message: "budget exceeded"}}, ""},
		{acpTurnEnd{StopReason: acp.StopReasonEndTurn}, ""},
	}
	for _, tc := range tests {
		if got := p.ClassifyError(tc.turn); got != tc.want {
			t.Errorf("ClassifyError(%+v) = %q, want %q", tc.turn.RPCErr, got, tc.want)
		}
	}
}
