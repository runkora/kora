package operator

import "fmt"

// tenantNamespace returns spec.namespace, or kora-tenant-<name> if unset.
func tenantNamespace(t *KoraTenant) string {
	if t.Spec.Namespace != "" {
		return t.Spec.Namespace
	}
	return fmt.Sprintf("kora-tenant-%s", t.Name)
}

// namespaceConflict says why tenant may not use its namespace, or returns "".
// The system namespace is never a tenant's. A namespace two tenants name stays
// with the one created first.
func namespaceConflict(tenant *KoraTenant, tenants []KoraTenant) string {
	ns := tenantNamespace(tenant)
	if ns == systemNamespace {
		return fmt.Sprintf("namespace %q is the system namespace", ns)
	}
	for i := range tenants {
		other := &tenants[i]
		if other.Name != tenant.Name && tenantNamespace(other) == ns && createdFirst(other, tenant) {
			return fmt.Sprintf("namespace %q belongs to tenant %q", ns, other.Name)
		}
	}
	return ""
}

// createdFirst orders tenants by creation time, then by name.
func createdFirst(a, b *KoraTenant) bool {
	if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	}
	return a.Name < b.Name
}

// servedTenants returns the active tenants that hold their namespace. The
// aggregators serve a namespace's resources only to the tenant that owns it.
func servedTenants(tenants []KoraTenant) []KoraTenant {
	out := make([]KoraTenant, 0, len(tenants))
	for i := range tenants {
		if tenants[i].Spec.Active && namespaceConflict(&tenants[i], tenants) == "" {
			out = append(out, tenants[i])
		}
	}
	return out
}
