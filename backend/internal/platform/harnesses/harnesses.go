// Package harnesses is the single list of supported agent harnesses (the CLI
// frameworks that run inside agent pods). It holds data only and imports no
// feature package, so agents, sessionmanager, koramcp and declarative can all
// read it.
//
// Adding a harness: add an entry to all, add an ACP profile and a profileFor
// case in sessionmanager, update the kubebuilder Enum marker on
// declarative/v1alpha1.AgentSpec.Framework, add the image stage, then run
// `make generate`. Tests and `make check` fail when a piece is missing.
package harnesses

import "slices"

// Harness is one supported agent harness.
type Harness struct {
	// ID is the framework value stored on agent types and sent to the session-manager.
	ID string
	// Label is the one display name used everywhere in the UI.
	Label string
	// Description is the one-line summary shown on the harness picker card.
	Description string
}

// Default is the harness an agent type gets when none is given on create.
const Default = "claude-code"

var all = []Harness{
	{ID: "claude-code", Label: "Claude Code", Description: "Anthropic's agentic coding harness."},
	{ID: "codex", Label: "Codex", Description: "OpenAI's coding agent runtime."},
	{ID: "opencode", Label: "OpenCode", Description: "Open-source, model-agnostic agent."},
}

// All returns the supported harnesses in display order.
func All() []Harness { return slices.Clone(all) }

// IDs returns the supported harness IDs in display order.
func IDs() []string {
	ids := make([]string, len(all))
	for i, h := range all {
		ids[i] = h.ID
	}
	return ids
}

// Valid reports whether id is a supported harness.
func Valid(id string) bool {
	return slices.ContainsFunc(all, func(h Harness) bool { return h.ID == id })
}
