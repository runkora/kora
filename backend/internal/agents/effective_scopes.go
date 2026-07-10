package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
	platformconns "github.com/clear-route/orbit/internal/platform/connections"
)

// environmentLookup loads an environment by id within a tenant. Implemented by
// the environments repository; may be nil in a deployment without an
// environments store, in which case environment expansion is skipped.
type environmentLookup interface {
	GetEnvironment(ctx context.Context, tenantID, id string) (*Environment, error)
}

// connectionResolver resolves a connection name to its adapter and base host.
// Implemented by ConnectionCatalog over the mounted registry; may be nil when no
// connection registry is mounted, in which case connector auto-egress is skipped.
type connectionResolver interface {
	ResolveConnection(ctx context.Context, tenantID, name string) (adapter, baseHost string, ok bool, err error)
}

// ScopeResolver compiles an agent type's FULL effective grant scope set:
//
//   - the agent's own scopes — capability GrantScopes, its connectivity posture
//     (egress:<host>:connect), and its connector scopes (connection:<name>:<action>);
//   - the contribution of the referenced environment — its egress hosts and its
//     connectors;
//   - connector auto-egress — for every connector (own and environment), the
//     egress hosts the connection's adapter declares, so granting a connector
//     auto-allows reaching exactly that connection's hosts.
//
// It exists because two of those expansions need data the pure
// AgentType.EffectiveGrantScopes() method cannot reach: the environments store
// (DB) and the connection registry (mounted ConfigMap). The provisioner invokes
// it at the single grant-reconcile choke point, so both the provision and update
// paths produce identical effective scopes.
type ScopeResolver struct {
	envs  environmentLookup
	conns connectionResolver
}

// NewScopeResolver constructs a resolver. Either dependency may be nil — a nil
// environmentLookup skips environment expansion, a nil connectionResolver skips
// connector auto-egress — so a partially-wired deployment degrades gracefully to
// the agent's own configured scopes rather than failing.
func NewScopeResolver(envs environmentLookup, conns connectionResolver) *ScopeResolver {
	return &ScopeResolver{envs: envs, conns: conns}
}

// EffectiveScopes returns the deduplicated scope set to reconcile onto at's
// policy grant. A missing referenced environment (deleted, or a stale reference)
// is tolerated: the agent loses that contribution rather than failing
// reconciliation. A connection not present in the registry yields no auto-egress
// (its connection scope is still granted) — again, no hard failure.
func (r *ScopeResolver) EffectiveScopes(ctx context.Context, at AgentType) ([]string, error) {
	// Start from the agent's own compiled scopes (capability + own egress posture
	// + own connector scopes).
	scopes := at.EffectiveGrantScopes()

	// Merge the referenced environment, if any.
	connectors := at.Connectors
	if at.EnvironmentID != nil && strings.TrimSpace(*at.EnvironmentID) != "" && r.envs != nil {
		env, err := r.envs.GetEnvironment(ctx, at.TenantID, *at.EnvironmentID)
		switch {
		case err == nil && env != nil:
			scopes = append(scopes, env.EgressScopes()...)
			scopes = append(scopes, AgentType{Connectors: env.Connectors}.ConnectorScopes()...)
			connectors = append(append([]Connector{}, at.Connectors...), env.Connectors...)
		case errors.Is(err, ErrNotFound):
			// tolerated — the agent simply loses the environment's contribution
		default:
			return nil, fmt.Errorf("resolve environment %q: %w", *at.EnvironmentID, err)
		}
	}

	// Connector auto-egress: each connector's adapter declares the hosts its
	// connection must reach. Granting a connector auto-allows exactly those hosts,
	// independent of the agent's connectivity mode — a none-mode agent with a
	// GitHub connector can still reach GitHub, and only GitHub.
	if r.conns != nil {
		for _, c := range connectors {
			name := strings.TrimSpace(c.Connection)
			if name == "" {
				continue
			}
			adapter, baseHost, ok, err := r.conns.ResolveConnection(ctx, at.TenantID, name)
			if err != nil {
				return nil, fmt.Errorf("resolve connection %q for auto-egress: %w", name, err)
			}
			if !ok {
				continue // not registered (yet) — connection scope still granted, no egress derived
			}
			for _, h := range platformconns.EgressHosts(adapter, baseHost) {
				scopes = append(scopes, "egress:"+h+":connect")
			}
		}
	}

	return dedupeScopes(scopes), nil
}

// dedupeScopes removes duplicate scope strings while preserving first-seen order.
func dedupeScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ── Effective access (read model for the UI) ────────────────────────────────

// ConnectorEgress is the egress one connector auto-brings via its adapter.
type ConnectorEgress struct {
	Connection string   `json:"connection"`
	Adapter    string   `json:"adapter"`
	Hosts      []string `json:"hosts"`
}

// EffectiveAccess is the resolved, human-facing view of everything an agent type
// can reach: its own egress posture, the environment it references, the egress
// each connector auto-brings, and the deduplicated union. It is the read model
// behind the "Effective access" panel — the same inputs the grant resolver uses,
// surfaced for legibility so "what can this agent reach" is answerable at a glance.
type EffectiveAccess struct {
	Open             bool              `json:"open"` // open egress — any host
	OwnHosts         []string          `json:"own_hosts"`
	EnvironmentID    string            `json:"environment_id,omitempty"`
	EnvironmentName  string            `json:"environment_name,omitempty"`
	EnvironmentHosts []string          `json:"environment_hosts"`
	ConnectorEgress  []ConnectorEgress `json:"connector_egress"`
	Connectors       []Connector       `json:"connectors"` // effective connectors (own + environment)
	Hosts            []string          `json:"hosts"`      // deduplicated union (empty when Open)
}

// EffectiveAccess resolves the full, human-facing access view for at. It mirrors
// EffectiveScopes' merge logic but returns a structured, per-layer breakdown
// rather than scope strings. A missing environment or unregistered connection is
// tolerated (its contribution is simply omitted).
func (r *ScopeResolver) EffectiveAccess(ctx context.Context, at AgentType) (*EffectiveAccess, error) {
	ea := &EffectiveAccess{
		Open:             at.Connectivity.Mode == ConnectivityOpen,
		OwnHosts:         []string{},
		EnvironmentHosts: []string{},
		ConnectorEgress:  []ConnectorEgress{},
		Connectors:       []Connector{},
		Hosts:            []string{},
	}
	if at.Connectivity.Mode == ConnectivityWhitelist {
		ea.OwnHosts = trimNonEmpty(at.Connectivity.Hosts)
	}

	connectors := append([]Connector{}, at.Connectors...)
	if at.EnvironmentID != nil && strings.TrimSpace(*at.EnvironmentID) != "" && r.envs != nil {
		env, err := r.envs.GetEnvironment(ctx, at.TenantID, *at.EnvironmentID)
		switch {
		case err == nil && env != nil:
			ea.EnvironmentID = env.ID
			ea.EnvironmentName = env.Name
			ea.EnvironmentHosts = trimNonEmpty(env.EgressHosts)
			connectors = append(connectors, env.Connectors...)
		case errors.Is(err, ErrNotFound):
			// tolerated — the environment was deleted or the reference is stale
		default:
			return nil, fmt.Errorf("resolve environment %q: %w", *at.EnvironmentID, err)
		}
	}
	// Merge by connection name (own + environment) so a connection attached at
	// both layers appears once, with the union of its actions.
	ea.Connectors = mergeConnectors(connectors)

	ce, err := r.resolveConnectorEgress(ctx, at.TenantID, ea.Connectors)
	if err != nil {
		return nil, err
	}
	ea.ConnectorEgress = ce

	// The union is "any host" when open, so the explicit host list stays empty.
	if !ea.Open {
		ea.Hosts = unionHosts(ea.OwnHosts, ea.EnvironmentHosts, ea.ConnectorEgress)
	}
	return ea, nil
}

// resolveConnectorEgress resolves each connector's adapter-declared egress hosts
// from the connection registry. A connection not present (yet) contributes no
// egress; a nil resolver yields none.
func (r *ScopeResolver) resolveConnectorEgress(ctx context.Context, tenantID string, connectors []Connector) ([]ConnectorEgress, error) {
	out := []ConnectorEgress{}
	if r.conns == nil {
		return out, nil
	}
	for _, c := range connectors {
		adapter, baseHost, ok, err := r.conns.ResolveConnection(ctx, tenantID, c.Connection)
		if err != nil {
			return nil, fmt.Errorf("resolve connection %q for effective access: %w", c.Connection, err)
		}
		if !ok {
			continue
		}
		hosts := platformconns.EgressHosts(adapter, baseHost)
		if len(hosts) == 0 {
			continue
		}
		out = append(out, ConnectorEgress{Connection: c.Connection, Adapter: adapter, Hosts: hosts})
	}
	return out, nil
}

// unionHosts returns the deduplicated, sorted union of egress hosts across the
// posture, environment, and connector layers.
func unionHosts(own, env []string, connectorEgress []ConnectorEgress) []string {
	out := []string{}
	seen := make(map[string]struct{})
	add := func(hs []string) {
		for _, h := range hs {
			if _, ok := seen[h]; !ok {
				seen[h] = struct{}{}
				out = append(out, h)
			}
		}
	}
	add(own)
	add(env)
	for _, c := range connectorEgress {
		add(c.Hosts)
	}
	sort.Strings(out)
	return out
}

// mergeConnectors deduplicates connectors by connection name, unioning their
// actions and preserving first-seen order. Empty names/actions are dropped.
func mergeConnectors(in []Connector) []Connector {
	actions := map[string][]string{}
	order := make([]string, 0, len(in))
	for _, c := range in {
		name := strings.TrimSpace(c.Connection)
		if name == "" {
			continue
		}
		if _, ok := actions[name]; !ok {
			order = append(order, name)
			actions[name] = []string{}
		}
		for _, a := range c.Actions {
			if a = strings.TrimSpace(a); a != "" && !slices.Contains(actions[name], a) {
				actions[name] = append(actions[name], a)
			}
		}
	}
	out := make([]Connector, 0, len(order))
	for _, name := range order {
		out = append(out, Connector{Connection: name, Actions: actions[name]})
	}
	return out
}

// KubeClusters resolves the agent's Kubernetes connections into the clusters
// materialized as a kubeconfig inside its sandbox, so `kubectl` works with no
// flags and no credential. Connectors of any other adapter are ignored — this is
// an explicit allowlist on AdapterKubernetesBearer, not a silent skip of an
// unrecognized adapter: a github_app connector correctly contributes no
// kubeconfig entry.
//
// unresolved counts connectors the registry could not resolve at all. The
// ConnectionCatalog treats a missing connections.json as an empty registry, so
// unresolved > 0 means "the registry has not loaded yet", which is NOT the same
// as "this agent has no Kubernetes connection". Callers that would bake the
// result into a long-lived pod must not treat the two alike.
//
// Returns nil when no connection resolver is wired: the sandbox then gets no
// kubeconfig at all.
func (r *ScopeResolver) KubeClusters(ctx context.Context, tenantID string, connectors []Connector) (clusters []kubeconfig.Cluster, unresolved int, err error) {
	if r == nil || r.conns == nil {
		return nil, 0, nil
	}
	for _, c := range mergeConnectors(connectors) {
		name := strings.TrimSpace(c.Connection)
		if name == "" {
			continue
		}
		adapter, baseHost, ok, err := r.conns.ResolveConnection(ctx, tenantID, name)
		if err != nil {
			return nil, 0, fmt.Errorf("resolve connection %q for kubeconfig: %w", name, err)
		}
		if !ok {
			unresolved++
			continue
		}
		if adapter != platformconns.AdapterKubernetesBearer {
			continue
		}
		if server := kubeconfig.ServerURL(baseHost); server != "" {
			clusters = append(clusters, kubeconfig.Cluster{Name: name, Server: server})
		}
	}
	return clusters, unresolved, nil
}
