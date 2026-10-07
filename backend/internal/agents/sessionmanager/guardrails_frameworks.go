package sessionmanager

import (
	"encoding/json"
)

// The loop guard compares tool calls from Claude Code, Codex and OpenCode. Each
// runner records a call as the tool's name and its own arguments; these tables
// name each runtime's tools (docs/adr/2026-09-29-agent-run-guardrails.md).

// shellTools run a shell command: Claude Code's Bash, Codex's commandExecution,
// OpenCode's bash.
var shellTools = map[string]bool{"Bash": true, "commandExecution": true, "bash": true}

// waitTools re-arm a wait. Each call counts as one re-arm. Codex and OpenCode
// have none of their own; their waits are shell sleeps.
var waitTools = map[string]bool{"Monitor": true, "CronCreate": true, "ScheduleWakeup": true}

// observeTools only look or keep notes. Their results are not progress, so a
// loop of reading the same monitor output and re-arming still counts as a loop.
var observeTools = map[string]bool{
	// Claude Code
	"Read": true, "Glob": true, "Grep": true, "BashOutput": true, "TaskOutput": true,
	"TaskList": true, "TaskGet": true, "TaskStop": true, "CronList": true, "CronDelete": true,
	"TodoWrite": true, "TaskCreate": true, "TaskUpdate": true,
	// OpenCode
	"read": true, "glob": true, "grep": true, "list": true, "todowrite": true, "todoread": true,
}

// toolCall is one tool call as the loop guard sees it.
type toolCall struct {
	name    string
	key     string // name and input, without the description
	shell   bool
	command string
}

// normaliseCall keys a call on its name and input, dropping the free-text
// description Claude Code and OpenCode rewrite on every call.
func normaliseCall(p ToolUsePayload) toolCall {
	var fields map[string]any
	if json.Unmarshal(p.Input, &fields) != nil {
		return toolCall{name: p.Name, key: p.Name + "\x00" + string(p.Input)}
	}
	delete(fields, "description")
	canonical, _ := json.Marshal(fields) // map keys marshal sorted
	call := toolCall{name: p.Name, key: p.Name + "\x00" + string(canonical), shell: shellTools[p.Name]}
	if call.shell {
		call.command, _ = fields["command"].(string)
	}
	return call
}
