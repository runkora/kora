package agents

// ToolScopeDefinition describes an Orbit agent tool scope and the Claude Code
// built-in tool names it enables via the --tools flag.
type ToolScopeDefinition struct {
	Scope       string   `json:"scope"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
}

// SelectableToolScopes are the scopes shown in the UI — the operator chooses
// which of these to grant to an agent type.
//
// Excluded: web (WebFetch, WebSearch) — these are Anthropic server-side tools
// that are not available when routing through a non-Anthropic inference gateway.
var SelectableToolScopes = []ToolScopeDefinition{
	{
		Scope:       "shell",
		Description: "Execute shell commands",
		Tools:       []string{"Bash"},
	},
	{
		Scope:       "filesystem",
		Description: "Read and write files",
		Tools:       []string{"Read", "Write", "Edit", "Glob", "Grep"},
	},
	{
		Scope:       "agents",
		Description: "Spawn and communicate with sub-agents",
		Tools:       []string{"Agent", "SendMessage"},
	},
	{
		Scope:       "tasks",
		Description: "Create and manage tasks",
		Tools:       []string{"TaskCreate", "TaskGet", "TaskList", "TaskStop", "TaskUpdate"},
	},
	{
		Scope:       "mcp",
		Description: "Access MCP server resources",
		Tools:       []string{"ListMcpResourcesTool", "ReadMcpResourceTool"},
	},
}

// AutoToolScopes are always injected into every agent session regardless of
// the agent type's stored tool_scopes. They are not shown in the UI.
var AutoToolScopes = []ToolScopeDefinition{
	{
		Scope:       "lsp",
		Description: "Language server protocol integration",
		Tools:       []string{"LSP"},
	},
	{
		Scope:       "monitoring",
		Description: "Monitor background processes",
		Tools:       []string{"Monitor"},
	},
	{
		Scope:       "scheduling",
		Description: "Schedule recurring tasks with cron",
		Tools:       []string{"CronCreate", "CronDelete", "CronList"},
	},
	{
		Scope:       "meta",
		Description: "Tool discovery and skill execution",
		Tools:       []string{"ToolSearch", "Skill"},
	},
}
