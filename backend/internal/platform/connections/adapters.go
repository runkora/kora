// Package connections holds cross-cutting metadata about connection adapters —
// the credential mechanisms (github_app, passthrough, …) the egress gateway
// brokers. It is the single source of truth for each adapter's grantable action
// vocabulary and the egress hosts a connection of that adapter requires.
//
// It lives under platform because three features need this metadata without
// importing one another (depguard forbids feature→feature imports):
//
//   - internal/credentials — the gateway adapters publish their action
//     vocabulary from here (and classify requests into these actions);
//   - internal/agents — the grant compiler derives connector auto-egress, and
//     the connection catalog renders the picker, from here;
//   - internal/operator — may validate a Connection's base host against the
//     adapter's expectations.
//
// The table carries NO secrets and NO per-connection state — only static,
// adapter-intrinsic metadata.
package connections

import "strings"

// Adapter type identifiers. These mirror the credentials.AdapterType constants
// but are declared here as plain strings so platform carries no dependency on
// the feature package; the credential adapters key off these same values.
const (
	AdapterGitHubApp   = "github_app"
	AdapterPassthrough = "passthrough"
)

// Action vocabulary, shared by the gateway adapters (which classify requests
// into these actions) and the connector picker (which offers them as grants).
const (
	ActionGitHubRepoRead  = "repo:read"
	ActionGitHubRepoWrite = "repo:write"
	ActionPassthroughUse  = "use"
)

// ActionMeta describes one grantable action an adapter classifies requests into.
// Action is the scope fragment (e.g. "repo:read") that becomes
// connection:<name>:<action>; Description is human-facing for the picker and the
// policy-service scope vocabulary.
type ActionMeta struct {
	Action      string
	Description string
}

// adapterMeta is the static metadata for one adapter.
type adapterMeta struct {
	actions     []ActionMeta
	egressHosts func(baseHost string) []string
}

var registry = map[string]adapterMeta{
	AdapterGitHubApp: {
		actions: []ActionMeta{
			{Action: ActionGitHubRepoRead, Description: "Read access to the connection's GitHub repositories (clone, fetch, REST GET/HEAD)"},
			{Action: ActionGitHubRepoWrite, Description: "Write access to the connection's GitHub repositories (push, REST POST/PATCH/PUT/DELETE)"},
		},
		egressHosts: githubEgressHosts,
	},
	AdapterPassthrough: {
		actions: []ActionMeta{
			{Action: ActionPassthroughUse, Description: "Reach the connection's host with no credential injected"},
		},
		egressHosts: passthroughEgressHosts,
	},
}

// githubEgressHosts returns the hosts a GitHub App connection must reach. For
// github.com SaaS this is the well-known host family — git over HTTPS, the REST
// API, archive downloads, and raw/LFS content. For GitHub Enterprise the API,
// git, and uploads share the appliance host, so the single base host suffices.
func githubEgressHosts(baseHost string) []string {
	h := strings.ToLower(strings.TrimSpace(baseHost))
	if h == "" || h == "github.com" {
		return []string{"github.com", "api.github.com", "codeload.github.com", "*.githubusercontent.com"}
	}
	return []string{h}
}

// passthroughEgressHosts returns the connection's single declared host, if any —
// a passthrough connection injects no credential and reaches exactly its host.
func passthroughEgressHosts(baseHost string) []string {
	h := strings.ToLower(strings.TrimSpace(baseHost))
	if h == "" {
		return nil
	}
	return []string{h}
}

// Known reports whether adapter has registered metadata.
func Known(adapter string) bool {
	_, ok := registry[adapter]
	return ok
}

// Actions returns the adapter's grantable action vocabulary, or an empty
// (non-nil) slice for an unknown adapter so JSON serializes as [] not null.
func Actions(adapter string) []ActionMeta {
	m, ok := registry[adapter]
	if !ok {
		return []ActionMeta{}
	}
	out := make([]ActionMeta, len(m.actions))
	copy(out, m.actions)
	return out
}

// ActionNames returns just the action fragments for adapter (e.g.
// ["repo:read", "repo:write"]). Empty (non-nil) for an unknown adapter.
func ActionNames(adapter string) []string {
	m, ok := registry[adapter]
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(m.actions))
	for _, a := range m.actions {
		out = append(out, a.Action)
	}
	return out
}

// EgressHosts returns the host patterns a connection of this adapter and base
// host requires the agent to reach. Granting a connector auto-allows egress to
// exactly these hosts. Nil for an unknown adapter or one that declares no fixed
// hosts.
func EgressHosts(adapter, baseHost string) []string {
	m, ok := registry[adapter]
	if !ok || m.egressHosts == nil {
		return nil
	}
	return m.egressHosts(baseHost)
}
