package operator

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/platform/claimbinding"
)

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
		if other.Name != tenant.Name && tenantNamespace(other) == ns && declaredBefore(other, tenant) {
			return fmt.Sprintf("namespace %q belongs to tenant %q", ns, other.Name)
		}
	}
	return ""
}

// declaredBefore orders objects claiming one key: the older keeps it, with
// namespace/name breaking a tie.
func declaredBefore(a, b client.Object) bool {
	ta, tb := a.GetCreationTimestamp(), b.GetCreationTimestamp()
	if !ta.Equal(&tb) {
		return ta.Before(&tb)
	}
	return a.GetNamespace()+"/"+a.GetName() < b.GetNamespace()+"/"+b.GetName()
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

// claimTenant is the routing view of a KoraTenant for the overlap rule.
func claimTenant(t *KoraTenant) claimbinding.Tenant {
	var b *claimbinding.Binding
	if cb := t.Spec.OIDC.ClaimBinding; cb != nil {
		b = &claimbinding.Binding{Claim: cb.Claim, Values: cb.Values}
	}
	return claimbinding.Tenant{
		ID:           t.Name,
		IssuerURL:    t.Spec.OIDC.IssuerURL,
		ClientID:     t.Spec.OIDC.ClientID,
		EmailDomains: t.Spec.OIDC.EmailDomains,
		Binding:      b,
	}
}

// emailDomainConflict says why tenant may not claim its email domains, or
// returns "". Two active tenants may share a domain only when they meet the
// shared-domain rule. The older tenant keeps a contested domain.
func emailDomainConflict(tenant *KoraTenant, tenants []KoraTenant) string {
	mine := claimTenant(tenant)
	for i := range tenants {
		other := &tenants[i]
		if other.Name == tenant.Name || !other.Spec.Active || !declaredBefore(other, tenant) {
			continue
		}
		if d := claimbinding.Overlap(mine, claimTenant(other)); d != "" {
			return fmt.Sprintf("email domain %q belongs to tenant %q, which does not share it through claimBinding", d, other.Name)
		}
	}
	return ""
}
