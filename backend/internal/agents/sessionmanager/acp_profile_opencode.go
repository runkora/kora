package sessionmanager

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// openCodeProfile runs `opencode acp` on the acpRunner. It shares the legacy
// runner's config, environment, shell and instructions file, and declares the
// MCP servers on session/new instead of in the config.
type openCodeProfile struct {
	cfg OpenCodeRunnerConfig
}

// Command is `opencode acp --cwd workDir`; BinaryPath defaults to opencode.
func (p *openCodeProfile) Command(workDir string) (string, []string) {
	bin := p.cfg.BinaryPath
	if bin == "" {
		bin = "opencode"
	}
	return bin, []string{"acp", "--cwd", workDir}
}

// Prepare leaves the "mcp" block out of OPENCODE_CONFIG_CONTENT, so
// session/new.mcpServers is the only MCP source. OPENCODE_DISABLE_PROJECT_CONFIG
// keeps a workspace opencode.json from adding servers.
func (p *openCodeProfile) Prepare(workDir string) ([]string, error) {
	return openCodePrepare(p.cfg, workDir, nil)
}

// ClientCapabilities is empty: OpenCode has no subagent sessions.
func (p *openCodeProfile) ClientCapabilities() map[string]any { return nil }

// SessionParams declares the runtime server the agent provisions its sandbox
// through and one HTTP server per MCP server it holds tool grants on. The model
// is pinned in the config, so no mode or model is set on the session.
func (p *openCodeProfile) SessionParams(workDir string) acpSessionParams {
	servers := []acp.McpServer{{Stdio: &acp.McpServerStdio{
		Name:    "runtime",
		Command: "kora-runtime-mcp",
		Args:    []string{},
		Env: []acp.EnvVariable{
			{Name: "KORA_SESSION_MANAGER_URL", Value: p.cfg.SessionManagerURL},
			{Name: "KORA_RUN_ID", Value: p.cfg.RunID},
		},
	}}}
	// The agent calls the gateway unauthenticated; the identity-proxy sidecar
	// injects the platform token on the way out (docs/adr/2026-06-29-mcp-tool-gateway.md).
	if url := strings.TrimRight(p.cfg.ToolGatewayURL, "/"); url != "" {
		for _, server := range p.cfg.MCPServers {
			servers = append(servers, acp.McpServer{Http: &acp.McpServerHttpInline{
				Type: "http", Name: server, Url: url + "/v1/mcp/" + server, Headers: []acp.HttpHeader{},
			}})
		}
	}
	return acpSessionParams{CWD: workDir, MCPServers: servers}
}

// ClassifyError reads the budget denial from the text of OpenCode's internal
// error, e.g. "Internal error: Forbidden: budget exceeded: ...".
func (p *openCodeProfile) ClassifyError(turn acpTurnEnd) string {
	if e := turn.RPCErr; e != nil && e.Code == acpInternalErrorCode && isBudgetExceededMessage(e.Message) {
		return "budget_exhausted"
	}
	return ""
}

// IsQuestion is false: every OpenCode permission request is auto-allowed.
func (p *openCodeProfile) IsQuestion(acp.ToolCallUpdate) bool { return false }

// AdapterSignal reads nothing: OpenCode sends no retry or background-work signals.
func (p *openCodeProfile) AdapterSignal(acpAdapterMessage) acpSignal { return acpSignal{} }

var _ acpProfile = (*openCodeProfile)(nil)
