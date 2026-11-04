package operator

// Install-time settings the chart passes through the environment, fixed for
// the life of the process. Read by every aggregator and reconciler that
// names the system namespace.
var (
	// systemNamespace is where kora's services and the aggregated registries live.
	systemNamespace = "kora-system"

	// istioNamespace is where the customer's istiod runs; tenant network policies
	// admit xDS traffic to it.
	istioNamespace = "istio-system"

	// trustDomain is the SPIFFE trust domain, which names platform principals in
	// the AuthorizationPolicies the operator generates.
	trustDomain = ""
)

// Settings are the install-time values main wires in before starting the manager.
type Settings struct {
	SystemNamespace string
	IstioNamespace  string
	TrustDomain     string
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
}
