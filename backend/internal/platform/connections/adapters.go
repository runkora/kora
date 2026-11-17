// Package connections holds cross-cutting metadata about connection adapters,
// the credential mechanisms (github_app, kubernetes) the egress gateway brokers.
// It is the single source of truth for each adapter's grantable action
// vocabulary and the egress hosts a connection of that adapter requires.
//
// It lives under platform because three features need this metadata without
// importing one another (depguard forbids feature-to-feature imports):
//
//   - internal/credentials, where the gateway adapters publish their action
//     vocabulary from here (and classify requests into these actions);
//   - internal/agents, where the grant compiler derives connector auto-egress and
//     the connection catalog renders the picker, from here;
//   - internal/operator, which may validate a Connection's base host against the
//     adapter's expectations.
//
// The table carries no secrets and no per-connection state, only static
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
	AdapterKubernetesBearer = "kubernetes"
	AdapterAPIKey           = "apikey"
)

// Action vocabulary, shared by the gateway adapters (which classify requests
// into these actions) and the connector picker (which offers them as grants).
const (
	// GitHub actions are categories of GitHub App permissions, each with a
	// read/write pair. connection:<name>:read and :write cover every category.
	// See docs/adr/2026-11-15-github-connector-route-coverage.md.
	ActionGitHubMetadataRead     = "metadata:read"
	ActionGitHubContentsRead     = "contents:read"
	ActionGitHubContentsWrite    = "contents:write"
	ActionGitHubWorkflowsWrite   = "workflows:write"
	ActionGitHubIssuesRead       = "issues:read"
	ActionGitHubIssuesWrite      = "issues:write"
	ActionGitHubPullsRead        = "pulls:read"
	ActionGitHubPullsWrite       = "pulls:write"
	ActionGitHubCIRead           = "ci:read"
	ActionGitHubCIWrite          = "ci:write"
	ActionGitHubDeploymentsRead  = "deployments:read"
	ActionGitHubDeploymentsWrite = "deployments:write"
	ActionGitHubSecurityRead     = "security:read"
	ActionGitHubSecurityWrite    = "security:write"
	ActionGitHubSecretsRead      = "secrets:read"
	ActionGitHubSecretsWrite     = "secrets:write"
	ActionGitHubAdminRead        = "admin:read"
	ActionGitHubAdminWrite       = "admin:write"
	ActionGitHubMembersRead      = "members:read"
	ActionGitHubMembersWrite     = "members:write"
	ActionGitHubOrgProjectsRead  = "org_projects:read"
	ActionGitHubOrgProjectsWrite = "org_projects:write"

	// API key actions are a coarse read/write pair chosen by HTTP method. See
	// docs/adr/2026-11-11-apikey-connection-adapter.md.
	ActionAPIKeyRead  = "read"
	ActionAPIKeyWrite = "write"

	// Kubernetes actions are resource-category-scoped, not one coarse
	// read/write pair. See docs/adr/2026-07-06-kubernetes-connection-bearer-
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
// Action is the scope fragment (e.g. "contents:read") that becomes
// connection:<name>:<action>; Description is human-facing for the picker and the
// policy-service scope vocabulary.
type ActionMeta struct {
	Action      string
	Description string

	// Requires lists sibling action fragments this action confers when granted.
	// The scope publisher resolves each fragment to the full same-connection
	// scope (connection:<name>:<fragment>) and publishes it as the vocabulary
	// entry's requires edge, expanded at bundle compile. Used for the
	// metadata:read discovery edge.
	Requires []string
}

// QualifierMeta declares an adapter's optional grant-time resource dimension:
// the connection-scope qualifier segment (connection:<name>:<category>:<qualifier>:<verb>).
// See docs/adr/2026-07-22-connection-scope-qualifiers.md. Adapters without a
// qualifier dimension leave it nil.
type QualifierMeta struct {
	// Name is the singular, human-facing dimension name ("repository",
	// "namespace"), used by the attachment picker's labels.
	Name string
	// Description explains the dimension for admins.
	Description string
}

// adapterMeta is the static metadata for one adapter.
type adapterMeta struct {
	actions     []ActionMeta
	egressHosts func(baseHost string) []string

	// requiresHostname, when true, rejects a base host that is a bare IP
	// literal. Set by kubernetes and apikey: see ErrBaseHostMustBeHostname.
	requiresHostname bool

	// qualifier is the adapter's grant-time resource dimension, nil when the
	// adapter has none.
	qualifier *QualifierMeta

	// baselineActions are action fragments every connector attachment must
	// include unqualified for the connector to function (e.g. GitHub clients
	// probe metadata endpoints before real work). The attachment compiler adds
	// them because vocabulary requires edges cannot reach repository-qualified
	// grant scopes.
	baselineActions []string
}

// githubMeta is the requires edge every GitHub category action carries:
// gh/Octokit/most SDKs probe metadata endpoints before real work, so any
// category grant auto-confers the metadata reads.
var githubMeta = []string{ActionGitHubMetadataRead}

var registry = map[string]adapterMeta{
	AdapterGitHubApp: {
		actions: []ActionMeta{
			{Action: ActionGitHubMetadataRead, Description: "Read repository metadata, rate limits, and installation identity (probed by most GitHub clients before any real work)"},
			{Action: ActionGitHubContentsRead, Description: "Read repository contents: clone and fetch, files, commits, branches, tags, releases and archives (GitHub contents)", Requires: githubMeta},
			{Action: ActionGitHubContentsWrite, Description: "Write repository contents: push, file and git data writes, releases and merging pull requests (GitHub contents)", Requires: githubMeta},
			{Action: ActionGitHubWorkflowsWrite, Description: "Change GitHub Actions workflow files (GitHub workflows)", Requires: githubMeta},
			{Action: ActionGitHubIssuesRead, Description: "Read issues, comments, labels, milestones and assignees (GitHub issues)", Requires: githubMeta},
			{Action: ActionGitHubIssuesWrite, Description: "Create and edit issues, comments, labels and milestones (GitHub issues)", Requires: githubMeta},
			{Action: ActionGitHubPullsRead, Description: "Read pull requests, reviews and review comments (GitHub pull_requests)", Requires: githubMeta},
			{Action: ActionGitHubPullsWrite, Description: "Create and edit pull requests and reviews (GitHub pull_requests)", Requires: githubMeta},
			{Action: ActionGitHubCIRead, Description: "Read workflow runs, jobs, logs, artifacts, check runs and commit statuses (GitHub actions, checks, statuses, artifact_metadata, attestations)", Requires: githubMeta},
			{Action: ActionGitHubCIWrite, Description: "Dispatch and re-run workflows, and write check runs and commit statuses (GitHub actions, checks, statuses, artifact_metadata, attestations)", Requires: githubMeta},
			{Action: ActionGitHubDeploymentsRead, Description: "Read deployments, environments and Pages (GitHub deployments, environments, pages)", Requires: githubMeta},
			{Action: ActionGitHubDeploymentsWrite, Description: "Create deployments and change environments and Pages (GitHub deployments, environments, pages)", Requires: githubMeta},
			{Action: ActionGitHubSecurityRead, Description: "Read code scanning, Dependabot and secret scanning alerts and security advisories (GitHub security_events, vulnerability_alerts, secret_scanning_alerts, repository_advisories, code_quality)", Requires: githubMeta},
			{Action: ActionGitHubSecurityWrite, Description: "Update security alerts and advisories (GitHub security_events, vulnerability_alerts, secret_scanning_alerts, repository_advisories, code_quality)", Requires: githubMeta},
			{Action: ActionGitHubSecretsRead, Description: "Read Actions, Dependabot and agent secret names and variables (GitHub secrets, actions_variables, dependabot_secrets, agent_secrets, agent_variables)", Requires: githubMeta},
			{Action: ActionGitHubSecretsWrite, Description: "Set Actions, Dependabot and agent secrets and variables (GitHub secrets, actions_variables, dependabot_secrets, agent_secrets, agent_variables)", Requires: githubMeta},
			{Action: ActionGitHubAdminRead, Description: "Read repository settings, webhooks, collaborators, branch protection and custom properties (GitHub administration, repository_hooks, repository_custom_properties, repository_creation, installation_repositories)", Requires: githubMeta},
			{Action: ActionGitHubAdminWrite, Description: "Administer the repository: settings, webhooks, collaborators, branch protection, custom properties and repository creation (GitHub administration, repository_hooks, repository_custom_properties, repository_creation, installation_repositories)", Requires: githubMeta},
			{Action: ActionGitHubMembersRead, Description: "Read the organization's members, invitations and teams (GitHub members). Organization-wide, so only an unqualified grant covers it", Requires: githubMeta},
			{Action: ActionGitHubMembersWrite, Description: "Invite and remove organization members and edit teams (GitHub members). Organization-wide, so only an unqualified grant covers it", Requires: githubMeta},
			{Action: ActionGitHubOrgProjectsRead, Description: "Read organization projects (GitHub organization_projects). Organization-wide, so only an unqualified grant covers it", Requires: githubMeta},
			{Action: ActionGitHubOrgProjectsWrite, Description: "Edit organization projects (GitHub organization_projects). Organization-wide, so only an unqualified grant covers it", Requires: githubMeta},
		},
		egressHosts: githubEgressHosts,
		qualifier: &QualifierMeta{
			Name:        "repository",
			Description: "Scope this connector to specific repositories of the installation (bare repository name; the installation fixes the owner)",
		},
		baselineActions: []string{ActionGitHubMetadataRead},
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
		// A cluster connection serves exactly its own API server host:port. It
		// has no host family the way GitHub's github.com/api.github.com does.
		egressHosts: declaredHostOnly,
		// Every real Kubernetes cluster's API server is reachable by a DNS
		// hostname (cloud-managed or self-hosted); an IP literal is either a
		// misconfiguration or a cluster's default bare-IP control-plane
		// endpoint. Reject it here rather than let it fail closed later. See
		// ErrBaseHostMustBeHostname.
		requiresHostname: true,
	},
	AdapterAPIKey: {
		actions: []ActionMeta{
			{Action: ActionAPIKeyRead, Description: "Read access to the host: GET and HEAD requests"},
			{Action: ActionAPIKeyWrite, Description: "Write access to the host: POST, PUT, PATCH and DELETE requests"},
		},
		// An apikey connection serves exactly its declared host. It has no
		// qualifier and no baseline actions.
		egressHosts: declaredHostOnly,
		// The sandbox's egress interception authorizes by TLS SNI, which an
		// IP-literal destination never carries. Reject a bare IP here, as for
		// kubernetes. See ErrBaseHostMustBeHostname.
		requiresHostname: true,
	},
}

// githubEgressHosts returns the hosts a GitHub App connection must reach. For
// github.com SaaS this is the well-known host family: git over HTTPS, the REST
// API, archive downloads, and raw/LFS content. For GitHub Enterprise the API,
// git, and uploads share the appliance host, so the single base host suffices.
func githubEgressHosts(baseHost string) []string {
	h := strings.ToLower(strings.TrimSpace(baseHost))
	if h == "" || h == "github.com" {
		return []string{"github.com", "api.github.com", "uploads.github.com", "codeload.github.com", "*.githubusercontent.com"}
	}
	return []string{h}
}

// declaredHostOnly returns the connection's single declared host, for adapters
// that reach exactly that host and no family around it.
//
// The port is stripped. An egress scope names a host, never a host:port: the
// grant path rejects a host segment containing ":" (isStructurallyValidEgressScope
// reserves the colon for IPv6 literals), and the rego matcher compares against
// the CONNECT :authority with its port already removed (_connect_host in
// authz.rego). A kubernetes baseHost is conventionally "host:443", which would
// otherwise yield the ungrantable scope "egress:host:443:connector".
func declaredHostOnly(baseHost string) []string {
	h := strings.ToLower(strings.TrimSpace(baseHost))
	if h == "" {
		return nil
	}
	// SplitHostPort errors when there is no port, so keep h as-is in that case.
	// A bracketed IPv6 literal yields its bare address, which no egress pattern
	// matches (deny), consistent with the rego's IPv4/DNS-only host matching.
	if host, _, err := net.SplitHostPort(h); err == nil && host != "" {
		h = host
	}
	return []string{h}
}

// ErrBaseHostMustBeHostname is returned by ValidateBaseHost when adapter
// requires a DNS hostname and baseHost is a bare IP literal. TLS ClientHellos
// never carry SNI for an IP-literal destination (per spec), and the sandbox's
// transparent-egress interceptor
// (charts/kora/templates/egress-gateway/transparent-egress.yaml) authorizes
// outbound HTTPS by matching that SNI against the connection's granted
// egress:<host>:connect scope, so an IP-literal connection can never be
// authorized through a live agent sandbox. See
// docs/adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md,
// "Blocker 1".
var ErrBaseHostMustBeHostname = errors.New("base_host must be a DNS hostname, not an IP address")

// ValidateBaseHost checks baseHost against adapter's shape requirements.
// Unknown adapters, and adapters with no requirement, always return nil: this
// rejects only the case an adapter has opted into: today the bare-IP
// rejection of kubernetes and apikey.
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
// ["contents:read", "contents:write"]). Empty (non-nil) for an unknown adapter.
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

// Qualifier returns the adapter's grant-time resource dimension, or nil when
// the adapter declares none (or is unknown). The returned value is a copy.
func Qualifier(adapter string) *QualifierMeta {
	m, ok := registry[adapter]
	if !ok || m.qualifier == nil {
		return nil
	}
	q := *m.qualifier
	return &q
}

// BaselineActions returns the action fragments every connector attachment must
// include unqualified for the adapter's connections to function. Empty
// (non-nil) for an unknown adapter or one with no baselines.
func BaselineActions(adapter string) []string {
	m, ok := registry[adapter]
	if !ok {
		return []string{}
	}
	out := make([]string, len(m.baselineActions))
	copy(out, m.baselineActions)
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
