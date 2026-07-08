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

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// Adapter type identifiers. These mirror the credentials.AdapterType constants
// but are declared here as plain strings so platform carries no dependency on
// the feature package; the credential adapters key off these same values.
const (
	AdapterGitHubApp        = "github_app"
	AdapterPassthrough      = "passthrough"
	AdapterKubernetesBearer = "kubernetes"
)

// Action vocabulary, shared by the gateway adapters (which classify requests
// into these actions) and the connector picker (which offers them as grants).
const (
	ActionGitHubRepoRead  = "repo:read"
	ActionGitHubRepoWrite = "repo:write"
	ActionPassthroughUse  = "use"

	// Kubernetes actions are resource-category-scoped, not one coarse
	// read/write pair — see docs/adr/2026-07-06-kubernetes-connection-bearer-
	// adapter-and-gateway-ca-trust.md, Amendment 1. ActionClusterRead/Write is
	// the catch-all for cluster-scoped/administrative resources not covered by
	// a more specific category, and for API discovery calls.
	ActionClusterRead     = "cluster:read"
	ActionClusterWrite    = "cluster:write"
	ActionWorkloadsRead   = "workloads:read"
	ActionWorkloadsWrite  = "workloads:write"
	ActionConfigRead      = "config:read"
	ActionConfigWrite     = "config:write"
	ActionSecretsRead     = "secrets:read"
	ActionSecretsWrite    = "secrets:write"
	ActionNetworkingRead  = "networking:read"
	ActionNetworkingWrite = "networking:write"
	ActionStorageRead     = "storage:read"
	ActionStorageWrite    = "storage:write"
	ActionRBACRead        = "rbac:read"
	ActionRBACWrite       = "rbac:write"
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

	// requiresHostname, when true, rejects a base host that is a bare IP
	// literal. Only kubernetes sets this: see ErrBaseHostMustBeHostname.
	requiresHostname bool
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
	AdapterKubernetesBearer: {
		actions: []ActionMeta{
			{Action: ActionWorkloadsRead, Description: "Read access to workload resources (pods, deployments, replicasets, statefulsets, daemonsets, jobs, cronjobs)"},
			{Action: ActionWorkloadsWrite, Description: "Write access to workload resources (pods, deployments, replicasets, statefulsets, daemonsets, jobs, cronjobs)"},
			{Action: ActionConfigRead, Description: "Read access to ConfigMaps"},
			{Action: ActionConfigWrite, Description: "Write access to ConfigMaps"},
			{Action: ActionSecretsRead, Description: "Read access to Secrets"},
			{Action: ActionSecretsWrite, Description: "Write access to Secrets"},
			{Action: ActionNetworkingRead, Description: "Read access to networking resources (services, endpoints, ingresses, network policies)"},
			{Action: ActionNetworkingWrite, Description: "Write access to networking resources (services, endpoints, ingresses, network policies)"},
			{Action: ActionStorageRead, Description: "Read access to storage resources (persistent volumes/claims, storage classes)"},
			{Action: ActionStorageWrite, Description: "Write access to storage resources (persistent volumes/claims, storage classes)"},
			{Action: ActionRBACRead, Description: "Read access to RBAC resources (roles, role bindings, cluster roles, cluster role bindings, service accounts)"},
			{Action: ActionRBACWrite, Description: "Write access to RBAC resources (roles, role bindings, cluster roles, cluster role bindings, service accounts)"},
			{Action: ActionClusterRead, Description: "Read access to cluster-scoped/administrative resources not covered by a more specific category (nodes, namespaces, events, resource quotas), and API discovery"},
			{Action: ActionClusterWrite, Description: "Write access to cluster-scoped/administrative resources not covered by a more specific category (nodes, namespaces, events, resource quotas)"},
		},
		// A cluster connection serves exactly its own API server host:port —
		// no host family the way GitHub's github.com/api.github.com/… is.
		egressHosts: passthroughEgressHosts,
		// Every real Kubernetes cluster's API server is reachable by a DNS
		// hostname (cloud-managed or self-hosted); an IP literal is either a
		// misconfiguration or a cluster's default bare-IP control-plane
		// endpoint. Reject it here rather than let it fail closed later. See
		// ErrBaseHostMustBeHostname.
		requiresHostname: true,
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

// ErrBaseHostMustBeHostname is returned by ValidateBaseHost when adapter
// requires a DNS hostname and baseHost is a bare IP literal. TLS ClientHellos
// never carry SNI for an IP-literal destination (per spec), and the sandbox's
// transparent-egress interceptor
// (charts/orbit/templates/egress-gateway/transparent-egress.yaml) authorizes
// outbound HTTPS by matching that SNI against the connection's granted
// egress:<host>:connect scope — so an IP-literal connection can never be
// authorized through a live agent sandbox. See
// docs/adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md,
// "Blocker 1".
var ErrBaseHostMustBeHostname = errors.New("base_host must be a DNS hostname, not an IP address")

// ValidateBaseHost checks baseHost against adapter's shape requirements.
// Unknown adapters, and adapters with no requirement, always return nil —
// this only rejects the specific case an adapter has opted into (currently
// just kubernetes's bare-IP rejection).
func ValidateBaseHost(adapter, baseHost string) error {
	m, ok := registry[adapter]
	if !ok || !m.requiresHostname {
		return nil
	}

	host := strings.TrimSpace(baseHost)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")
	if host == "" {
		return nil
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")

	if net.ParseIP(host) != nil {
		return fmt.Errorf("%w: %q", ErrBaseHostMustBeHostname, baseHost)
	}
	return nil
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
