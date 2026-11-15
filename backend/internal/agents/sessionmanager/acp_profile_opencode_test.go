package sessionmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
	"github.com/runkora/kora/backend/internal/platform/brand"
)

func testOpenCodeProfile() *openCodeProfile {
	return &openCodeProfile{cfg: OpenCodeRunnerConfig{
		InferenceGatewayURL: "http://inference-gateway.test",
		Model:               "anthropic/claude-sonnet-5",
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
	if _, err := p.Prepare(t.TempDir()); err == nil || !strings.Contains(err.Error(), "model") {
		t.Errorf("Prepare without a model = %v, want an error naming the model", err)
	}
	p = testOpenCodeProfile()
	p.cfg.InferenceGatewayURL = ""
	if _, err := p.Prepare(t.TempDir()); err == nil || !strings.Contains(err.Error(), "InferenceGatewayURL") {
		t.Errorf("Prepare without a gateway URL = %v, want an error naming InferenceGatewayURL", err)
	}
}

// The environment switches off OpenCode's calls home and workspace config,
// carries the run config, and puts HOME and the config home where the agent
// cannot write.
func TestOpenCodeProfile_PrepareEnv(t *testing.T) {
	p := testOpenCodeProfile()
	p.cfg.ExtraEnv = []string{"OPENCODE_DISABLE_MODELS_FETCH=0", "WORKSPACE=/w"}
	workDir := t.TempDir()

	env, err := p.Prepare(workDir)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	got := map[string]string{}
	for _, e := range withRunEnv(nil, env) {
		k, v, _ := strings.Cut(e, "=")
		got[k] = v
	}
	for _, e := range append(openCodeOfflineEnv, openCodeWorkspaceEnv...) {
		k, v, _ := strings.Cut(e, "=")
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if got["WORKSPACE"] != "/w" {
		t.Errorf("WORKSPACE = %q, want the ExtraEnv value", got["WORKSPACE"])
	}
	want, err := openCodeConfig(p.cfg, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if got["OPENCODE_CONFIG_CONTENT"] != string(want) {
		t.Errorf("OPENCODE_CONFIG_CONTENT = %q, want the run config", got["OPENCODE_CONFIG_CONTENT"])
	}
	// OpenCode loads code from HOME and its config home. The agent cannot write either.
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME"} {
		if got[k] != runfs.ConfigDir(workDir) {
			t.Errorf("%s = %q, want the run's config dir %q", k, got[k], runfs.ConfigDir(workDir))
		}
	}
	if _, err := os.Stat(filepath.Join(runfs.ConfigDir(workDir), "opencode")); err != nil {
		t.Errorf("config home not created: %v", err)
	}
	for _, k := range []string{"XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		if !strings.HasPrefix(got[k], runfs.HomeDir(workDir)) {
			t.Errorf("%s = %q, want it under the writable run home", k, got[k])
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, "opencode.json")); !os.IsNotExist(err) {
		t.Errorf("opencode.json in the workspace: %v; want none", err)
	}
}

// The config offers only the agent type's model through the gateway provider,
// named after the brand, with sharing, updates and web tools off.
func TestOpenCodeConfig(t *testing.T) {
	cfg := testOpenCodeProfile().cfg
	cfg.Brand = brand.MustNew("Example")
	data, err := openCodeConfig(cfg, t.TempDir())
	if err != nil {
		t.Fatalf("openCodeConfig: %v", err)
	}

	var got struct {
		EnabledProviders []string          `json:"enabled_providers"`
		Model            string            `json:"model"`
		SmallModel       string            `json:"small_model"`
		Share            string            `json:"share"`
		Autoupdate       *bool             `json:"autoupdate"`
		Permission       map[string]string `json:"permission"`
		Experimental     struct {
			MCPTimeout *float64 `json:"mcp_timeout"`
		} `json:"experimental"`
		Provider map[string]struct {
			Name    string            `json:"name"`
			NPM     string            `json:"npm"`
			Options map[string]string `json:"options"`
			Models  map[string]any    `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if got.Experimental.MCPTimeout == nil || *got.Experimental.MCPTimeout != 720000 {
		t.Errorf("experimental.mcp_timeout = %v, want 720000 (ms)", got.Experimental.MCPTimeout)
	}
	if len(got.EnabledProviders) != 1 || got.EnabledProviders[0] != "kora" {
		t.Errorf("enabled_providers = %v, want [kora]", got.EnabledProviders)
	}
	const want = "kora/anthropic/claude-sonnet-5"
	if got.Model != want || got.SmallModel != want {
		t.Errorf("model, small_model = %q, %q; want %q for both", got.Model, got.SmallModel, want)
	}
	if len(got.Provider) != 1 {
		t.Fatalf("providers = %v, want only kora", got.Provider)
	}
	kora := got.Provider["kora"]
	if kora.Name != "Example" {
		t.Errorf("provider name = %q, want Example", kora.Name)
	}
	if kora.NPM != "@ai-sdk/openai-compatible" {
		t.Errorf("npm = %q, want @ai-sdk/openai-compatible", kora.NPM)
	}
	if url := kora.Options["baseURL"]; url != "http://inference-gateway.test/run/run-1/v1" {
		t.Errorf("baseURL = %q, want http://inference-gateway.test/run/run-1/v1", url)
	}
	if _, ok := kora.Models["anthropic/claude-sonnet-5"]; !ok || len(kora.Models) != 1 {
		t.Errorf("models = %v, want only anthropic/claude-sonnet-5", kora.Models)
	}
	if got.Share != "disabled" || got.Autoupdate == nil || *got.Autoupdate {
		t.Errorf("share, autoupdate = %q, %v; want disabled, false", got.Share, got.Autoupdate)
	}
	for _, tool := range []string{"webfetch", "websearch"} {
		if got.Permission[tool] != "deny" {
			t.Errorf("permission[%s] = %q, want deny", tool, got.Permission[tool])
		}
	}
}

// The chart caps the provisioning budget at 600s; the egress canary adds up to
// egressCanaryMaxWait. OpenCode must wait at least that long for provision_sandbox.
func TestOpenCodeMCPTimeoutCoversProvisioning(t *testing.T) {
	const chartBudgetCap = 600 * time.Second
	floor := chartBudgetCap + egressCanaryMaxWait
	if got := time.Duration(openCodeMCPTimeoutMs) * time.Millisecond; got < floor {
		t.Errorf("openCodeMCPTimeoutMs = %v, want at least %v", got, floor)
	}
}

func TestOpenCodeConfig_SystemPromptAsInstructions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prompt string
	}{
		{name: "with a prompt", prompt: "# Runtime environment (kora)\n## Workspace"},
		{name: "without one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testOpenCodeProfile().cfg
			cfg.SystemPrompt = tc.prompt
			dir := t.TempDir()
			data, err := openCodeConfig(cfg, dir)
			if err != nil {
				t.Fatalf("openCodeConfig: %v", err)
			}
			var got struct {
				Instructions []string `json:"instructions"`
			}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			agentsMD := filepath.Join(dir, "AGENTS.md")
			if n := len(got.Instructions); n == 0 || got.Instructions[n-1] != agentsMD {
				t.Fatalf("instructions = %v, want %s last", got.Instructions, agentsMD)
			}
			if tc.prompt == "" {
				if len(got.Instructions) != 1 {
					t.Errorf("instructions = %v, want only %s", got.Instructions, agentsMD)
				}
				return
			}
			// Outside the workspace, in the config dir the agent can read but not change.
			if len(got.Instructions) != 2 || filepath.Dir(got.Instructions[0]) != runfs.ConfigDir(dir) {
				t.Fatalf("instructions = %v, want the prompt file in %s then AGENTS.md", got.Instructions, runfs.ConfigDir(dir))
			}
			body, err := os.ReadFile(got.Instructions[0])
			if err != nil || string(body) != tc.prompt {
				t.Errorf("instructions file = %q, %v; want the system prompt", body, err)
			}
		})
	}
}

// Without a ShellPath the config leaves OpenCode on the container's shell.
func TestOpenCodeConfig_NoShellPath(t *testing.T) {
	cfg := testOpenCodeProfile().cfg
	cfg.ShellPath = ""
	data, err := openCodeConfig(cfg, t.TempDir())
	if err != nil {
		t.Fatalf("openCodeConfig: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got["shell"]; present {
		t.Error("expected no shell key when ShellPath is empty")
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
		if got := p.ClassifyError(tc.turn).Code; got != tc.want {
			t.Errorf("ClassifyError(%+v) = %q, want %q", tc.turn.RPCErr, got, tc.want)
		}
	}
}
