// Package toolscopes is the single source of truth for Orbit agent tool scopes and
// the Claude Code built-in tools each one enables. Both the agents API (which lists
// scopes for the UI) and the session-manager runner (which builds the Claude --tools
// flag) depend on this mapping, so it is defined exactly once here. The agents
// package imports sessionmanager, so a shared leaf package is the only way to avoid
// duplicating the table (or an import cycle).
package toolscopes

// Definition describes an Orbit agent tool scope and the Claude Code built-in tool
// names it enables via the --tools flag.
type Definition struct {
	Scope       string   `json:"scope"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
}

// Selectable are the scopes shown in the UI — the operator chooses which of these
// to grant to an agent type.
//
// Excluded: web (WebFetch, WebSearch) — these are Anthropic server-side tools that
// are not available when routing through a non-Anthropic inference gateway.
var Selectable = []Definition{
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

// Auto are always injected into every agent session regardless of the agent type's
// stored tool_scopes. They are not shown in the UI.
var Auto = []Definition{
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

// ClaudeTools resolves the given granted scope names plus the always-on Auto scopes
// into a deduplicated, order-stable list of Claude Code built-in tool names for the
// --tools flag. Unrecognized scopes are ignored. AskUserQuestion is intentionally
// absent from every definition: Claude Code auto-rejects it in headless stream-json
// mode; use a custom MCP tool for human-in-the-loop instead.
func ClaudeTools(scopes []string) []string {
	byScope := make(map[string][]string, len(Selectable)+len(Auto))
	for _, d := range Selectable {
		byScope[d.Scope] = d.Tools
	}
	for _, d := range Auto {
		byScope[d.Scope] = d.Tools
	}

	seen := make(map[string]bool)
	var tools []string
	add := func(scope string) {
		for _, t := range byScope[scope] {
			if !seen[t] {
				seen[t] = true
				tools = append(tools, t)
			}
		}
	}
	for _, s := range scopes {
		add(s)
	}
	for _, d := range Auto {
		add(d.Scope)
	}
	return tools
}
