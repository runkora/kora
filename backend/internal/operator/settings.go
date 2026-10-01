package operator

// Install-time settings the chart passes through the environment, fixed for
// the life of the process. Read by every aggregator and reconciler that
// names the system namespace.
var (
	// systemNamespace is where Orbit's services and the aggregated registries live.
	systemNamespace = "orbit-system"

	// istioNamespace is where the customer's istiod runs; tenant network policies
	// admit xDS traffic to it.
	istioNamespace = "istio-system"

	// trustDomain is the SPIFFE trust domain, which names platform principals in
	// the AuthorizationPolicies the operator generates.
	trustDomain = ""

	// tenantScope selects whether OrbitTenant (and the objects derived from it)
	// is cluster-scoped (today's default, one install per cluster) or
	// namespace-scoped (lets multiple independent installs share a cluster).
	// Must match the scope the orbit-crds chart actually installed
	// orbittenants.orbit.io with — the chart's global.tenantScope value drives
	// both. See docs/adr for the namespace-scoped tenants design.
	tenantScope = "cluster"
)

// Settings are the install-time values main wires in before starting the manager.
type Settings struct {
	SystemNamespace string
	IstioNamespace  string
	TrustDomain     string
	TenantScope     string
}

// Apply sets the package-level install settings. Call once, before any
// reconciler or aggregator is constructed.
func Apply(s Settings) {
	if s.SystemNamespace != "" {
		systemNamespace = s.SystemNamespace
	}
	if s.IstioNamespace != "" {
		istioNamespace = s.IstioNamespace
	}
	if s.TrustDomain != "" {
		trustDomain = s.TrustDomain
	}
	if s.TenantScope != "" {
		tenantScope = s.TenantScope
	}
}

// namespacedTenants reports whether this install runs with OrbitTenant
// namespace-scoped. False (cluster-scoped) is the default and preserves
// today's exact behavior.
func namespacedTenants() bool {
	return tenantScope == "namespaced"
}
