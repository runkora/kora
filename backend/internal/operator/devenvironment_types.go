// OrbitDevEnvironment CRD Go types — see
// docs/adr/2026-07-13-ephemeral-dev-environments-via-vcluster.md for the
// design rationale and what changed on the way to production from the
// original prototype spike.
//
// The actual type definitions live in internal/platform/orbitapi (shared
// with cmd/identity's devenvironments feature — internal/operator is itself
// a feature package under this repo's "features never import each other"
// convention, so a second feature package cannot import it directly). These
// are type aliases, not copies: every reconciler file in this package
// (devenvironment_controller.go, devenvironment_csr.go,
// devenvironment_platform.go, devenvironment_identity.go,
// devenvironment_fixtures.go, devenvironment_ingress.go,
// devenvironment_app_ingress.go, devenvironment_migrations.go, and their
// tests) keeps working unchanged, since an alias is the identical Go type,
// not a distinct one requiring conversion.
package operator

import "github.com/clear-route/orbit/internal/platform/orbitapi"

type (
	// OrbitDevEnvironment declares the desired state of one ephemeral
	// development environment. See orbitapi.OrbitDevEnvironment for the full
	// field-level documentation.
	OrbitDevEnvironment = orbitapi.OrbitDevEnvironment
	// OrbitDevEnvironmentSpec defines the desired state of a dev environment.
	OrbitDevEnvironmentSpec = orbitapi.OrbitDevEnvironmentSpec
	// OrbitDevEnvironmentStatus records observed state.
	OrbitDevEnvironmentStatus = orbitapi.OrbitDevEnvironmentStatus
	// OrbitDevEnvironmentList is a list of OrbitDevEnvironment resources.
	OrbitDevEnvironmentList = orbitapi.OrbitDevEnvironmentList
)

// DevEnvironment status condition types.
const (
	ConditionDevEnvNamespaceReady  = orbitapi.ConditionDevEnvNamespaceReady
	ConditionDevEnvVClusterReady   = orbitapi.ConditionDevEnvVClusterReady
	ConditionDevEnvKubeconfigCSR   = orbitapi.ConditionDevEnvKubeconfigCSR
	ConditionDevEnvPlatformApplied = orbitapi.ConditionDevEnvPlatformApplied
	ConditionDevEnvPlatformReady   = orbitapi.ConditionDevEnvPlatformReady
	ConditionDevEnvProgressing     = orbitapi.ConditionDevEnvProgressing
)

// DevEnvironment phase values. There is deliberately no "Expired" alias —
// expiry deletes the CR outright, so the phase is never observable.
const (
	DevEnvPhasePending      = orbitapi.DevEnvPhasePending
	DevEnvPhaseProvisioning = orbitapi.DevEnvPhaseProvisioning
	DevEnvPhaseReady        = orbitapi.DevEnvPhaseReady
	DevEnvPhaseSleeping     = orbitapi.DevEnvPhaseSleeping
	DevEnvPhaseFailed       = orbitapi.DevEnvPhaseFailed
)

// devEnvironmentFinalizer ensures the host namespace and vcluster Helm
// release are cleaned up on delete, whether deletion is triggered by TTL
// expiry (the reconciler deletes the CR itself) or a direct kubectl/API
// delete — both paths converge on the same finalizer-driven cleanup.
const devEnvironmentFinalizer = "orbit.io/devenvironment-cleanup"

// devEnvNamespace returns the host namespace for an environment: the CR name
// is the environment ID, so no separate ID field is needed (mirrors
// tenantNamespace's derivation for OrbitTenant).
func devEnvNamespace(e *OrbitDevEnvironment) string {
	return orbitapi.DevEnvNamespacePrefix + e.Name
}
