package operator

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// tenantNamespace returns spec.namespace, or the auto-derived default if
// unset: orbit-tenant-<name> in cluster-scoped mode (global, matching today's
// behavior), or <systemNamespace>-tenant-<name> in namespace-scoped mode —
// prefixed so two installs sharing a cluster never derive the same tenant
// namespace for a same-named tenant.
func tenantNamespace(t *OrbitTenant) string {
	if t.Spec.Namespace != "" {
		return t.Spec.Namespace
	}
	if namespacedTenants() {
		return fmt.Sprintf("%s-tenant-%s", systemNamespace, t.Name)
	}
	return fmt.Sprintf("orbit-tenant-%s", t.Name)
}

// tenantListOpts scopes an OrbitTenantList List call to this install's own
// tenants in namespace-scoped mode (every OrbitTenant for this install lives
// in its system namespace), or returns no options in cluster-scoped mode,
// where OrbitTenant has no namespace and every tenant on the cluster belongs
// to the single install by definition.
func tenantListOpts() []client.ListOption {
	if namespacedTenants() {
		return []client.ListOption{client.InNamespace(systemNamespace)}
	}
	return nil
}
