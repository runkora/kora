package agents

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
	platformconns "github.com/clear-route/orbit/internal/platform/connections"
)

// environmentLookup loads a tenant's environments by ID, in the order given.
// An ID the tenant has no environment for is left out.
type environmentLookup interface {
	ListEnvironmentsByIDs(ctx context.Context, tenantID string, ids []string) ([]Environment, error)
}

// environmentIDs lists the IDs of the attached environments, default first as stored.
func environmentIDs(envs []AgentEnvironment) []string {
	ids := make([]string, 0, len(envs))
	for _, e := range envs {
		ids = append(ids, e.EnvironmentID)
	}
	return ids
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
//   - the union of every attached environment's egress hosts and connectors;
//   - connector auto-egress — for every connector (own and environment), the
//     egress hosts the connection's adapter declares as egress:<host>:connector,
//     so granting a connector auto-allows reaching exactly that connection's hosts.
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

// EffectiveScopes returns the deduplicated scope set to reconcile onto at's policy grant.
// A missing attached environment or unregistered connection drops its contribution and does not fail reconciliation.
func (r *ScopeResolver) EffectiveScopes(ctx context.Context, at AgentType) ([]string, error) {
	// Start from the agent's own compiled scopes (capability + own egress posture
	// + own connector scopes).
	scopes := at.EffectiveGrantScopes()

	// The union of every attached environment.
	envs, err := r.attachedEnvironments(ctx, at)
	if err != nil {
		return nil, err
	}
	connectors := append([]Connector{}, at.Connectors...)
	for _, env := range envs {
		scopes = append(scopes, env.EgressScopes()...)
		scopes = append(scopes, AgentType{Connectors: env.Connectors}.ConnectorScopes()...)
		connectors = append(connectors, env.Connectors...)
	}

	// Connector auto-egress: each connector's adapter declares the hosts its
	// connection must reach. Granting a connector auto-allows exactly those hosts,
	// independent of the agent's connectivity mode — a none-mode agent with a
	// GitHub connector can still reach GitHub, and only GitHub.
	//
	// Baseline actions ride the same resolution: the adapter's declared
	// baselines (e.g. GitHub metadata:read) are compiled as unqualified scopes
	// on every attachment that grants at least one action. They must be added
	// here — vocabulary requires edges cannot reach resource-qualified grant
	// scopes. See docs/adr/2026-07-22-connection-scope-qualifiers.md.
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
			// Connector hosts get their own verb so the egress gateway can tell
			// them from the agent's own posture (ADR 2026-06-06, Amendment 5).
			for _, h := range platformconns.EgressHosts(adapter, baseHost) {
				scopes = append(scopes, "egress:"+h+":connector")
			}
			if hasAction(c) {
				for _, baseline := range platformconns.BaselineActions(adapter) {
					scopes = append(scopes, "connection:"+name+":"+baseline)
				}
			}
		}
	}

	return dedupeScopes(scopes), nil
}

// attachedEnvironments loads the agent's environments. One the store cannot
// find contributes nothing and does not fail the grant.
func (r *ScopeResolver) attachedEnvironments(ctx context.Context, at AgentType) ([]Environment, error) {
	if r.envs == nil {
		return nil, nil
	}
	envs, err := r.envs.ListEnvironmentsByIDs(ctx, at.TenantID, environmentIDs(at.Environments))
	if err != nil {
		return nil, fmt.Errorf("resolve environments: %w", err)
	}
	return envs, nil
}

// hasAction reports whether the connector grants at least one non-empty action
// — baselines apply only to attachments that actually grant something.
func hasAction(c Connector) bool {
	for _, a := range c.Actions {
		if strings.TrimSpace(a) != "" {
			return true
		}
	}
	return false
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

// EnvironmentAccess is what one attached environment contributes.
type EnvironmentAccess struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Hosts []string `json:"hosts"`
}

// EffectiveAccess is everything an agent type can reach, per layer and as a union, for the "Effective access" panel.
// It uses the same inputs as the grant resolver.
type EffectiveAccess struct {
	Open         bool                `json:"open"` // open egress: any host
	OwnHosts     []string            `json:"own_hosts"`
	Environments []EnvironmentAccess `json:"environments"`
	// EnvironmentHosts is the union of every environment's hosts.
	EnvironmentHosts []string          `json:"environment_hosts"`
	ConnectorEgress  []ConnectorEgress `json:"connector_egress"`
	Connectors       []Connector       `json:"connectors"` // effective connectors (own + environment)
	Hosts            []string          `json:"hosts"`      // deduplicated union (empty when Open)
}

// EffectiveAccess resolves at's access per layer with EffectiveScopes' merge logic.
// A missing environment or unregistered connection contributes nothing.
func (r *ScopeResolver) EffectiveAccess(ctx context.Context, at AgentType) (*EffectiveAccess, error) {
	ea := &EffectiveAccess{
		Open:             at.Connectivity.Mode == ConnectivityOpen,
		OwnHosts:         []string{},
		Environments:     []EnvironmentAccess{},
		EnvironmentHosts: []string{},
		ConnectorEgress:  []ConnectorEgress{},
		Connectors:       []Connector{},
		Hosts:            []string{},
	}
	if at.Connectivity.Mode == ConnectivityWhitelist {
		ea.OwnHosts = trimNonEmpty(at.Connectivity.Hosts)
	}

	envs, err := r.attachedEnvironments(ctx, at)
	if err != nil {
		return nil, err
	}
	connectors := append([]Connector{}, at.Connectors...)
	var envHosts []string
	for _, env := range envs {
		hosts := trimNonEmpty(env.EgressHosts)
		ea.Environments = append(ea.Environments, EnvironmentAccess{ID: env.ID, Name: env.Name, Hosts: hosts})
		envHosts = append(envHosts, hosts...)
		connectors = append(connectors, env.Connectors...)
	}
	ea.EnvironmentHosts = unionHosts(envHosts, nil, nil)
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
// actions and resources while preserving first-seen order. Empty names/actions
// are dropped. Resource union: an empty Resources list means "all resources",
// so any all-resources contribution dominates the merge (the merged connector
// is unrestricted); otherwise the resource lists union.
func mergeConnectors(in []Connector) []Connector {
	type agg struct {
		actions   []string
		resources []string
		all       bool
	}
	byName := map[string]*agg{}
	order := make([]string, 0, len(in))
	for _, c := range in {
		name := strings.TrimSpace(c.Connection)
		if name == "" {
			continue
		}
		a, ok := byName[name]
		if !ok {
			a = &agg{actions: []string{}, resources: []string{}}
			order = append(order, name)
			byName[name] = a
		}
		for _, act := range c.Actions {
			if act = strings.TrimSpace(act); act != "" && !slices.Contains(a.actions, act) {
				a.actions = append(a.actions, act)
			}
		}
		if len(c.Resources) == 0 {
			a.all = true
			continue
		}
		for _, r := range c.Resources {
			if r = strings.ToLower(strings.TrimSpace(r)); r != "" && !slices.Contains(a.resources, r) {
				a.resources = append(a.resources, r)
			}
		}
	}
	out := make([]Connector, 0, len(order))
	for _, name := range order {
		a := byName[name]
		resources := a.resources
		if a.all {
			resources = nil
		}
		out = append(out, Connector{Connection: name, Actions: a.actions, Resources: resources})
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
