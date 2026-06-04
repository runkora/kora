package toolscopes

import (
	"slices"
	"testing"
)

func TestClaudeTools_AlwaysIncludesAutoScopes(t *testing.T) {
	tools := ClaudeTools(nil)
	for _, want := range []string{"LSP", "Monitor", "CronCreate", "ToolSearch", "Skill"} {
		if !slices.Contains(tools, want) {
			t.Errorf("auto tool %q missing from %v", want, tools)
		}
	}
}

func TestClaudeTools_GrantedScopeAddsTools(t *testing.T) {
	tools := ClaudeTools([]string{"shell", "filesystem"})
	for _, want := range []string{"Bash", "Read", "Write", "Edit", "Glob", "Grep"} {
		if !slices.Contains(tools, want) {
			t.Errorf("granted tool %q missing from %v", want, tools)
		}
	}
}

func TestClaudeTools_UnknownScopeIgnoredAndDeduped(t *testing.T) {
	tools := ClaudeTools([]string{"nope", "shell", "shell"})
	if !slices.Contains(tools, "Bash") {
		t.Errorf("expected Bash from shell scope, got %v", tools)
	}
	// "Bash" must appear exactly once despite the duplicate scope.
	count := 0
	for _, tl := range tools {
		if tl == "Bash" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Bash appears %d times, want 1: %v", count, tools)
	}
}

func TestClaudeTools_NeverGrantsAskUserQuestion(t *testing.T) {
	// AskUserQuestion is auto-rejected by Claude Code in headless mode; it must not
	// be reachable through any scope.
	all := make([]string, 0)
	for _, d := range Selectable {
		all = append(all, d.Scope)
	}
	tools := ClaudeTools(all)
	if slices.Contains(tools, "AskUserQuestion") {
		t.Errorf("AskUserQuestion must never be granted, got %v", tools)
	}
}
