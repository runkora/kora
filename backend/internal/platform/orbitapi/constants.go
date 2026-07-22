package orbitapi

import "time"

// Single-sourced dev-environment constants. Both internal/operator and
// internal/devenvironments import this package already (for the CRD types),
// so these values live here instead of being re-declared per package — the
// prototype duplicated the TTL hard cap three times across two languages and
// the "16443"/"orbit-devenv-" literals across several files (ADR §7).
const (
	// DevEnvTTLHardCap is the maximum spec.ttl a dev environment may be
	// extended to, regardless of how many times ExtendEnvironment is called.
	DevEnvTTLHardCap = 7 * 24 * time.Hour

	// DevEnvAPIGatewayPort is the shared gateway's TLS-passthrough listener
	// port for brokered kubectl access (SNI-routed per environment, never
	// requiring a DNS record — ADR pinned decision 2).
	DevEnvAPIGatewayPort = 16443

	// DevEnvNamespacePrefix precedes every dev environment's host namespace
	// name: the environment ID "foo" lives in namespace
	// "orbit-devenv-foo".
	DevEnvNamespacePrefix = "orbit-devenv-"
)
