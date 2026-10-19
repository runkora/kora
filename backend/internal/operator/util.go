package operator

import "fmt"

// tenantNamespace returns spec.namespace, or orbit-tenant-<name> if unset.
func tenantNamespace(t *KoraTenant) string {
	if t.Spec.Namespace != "" {
		return t.Spec.Namespace
	}
	return fmt.Sprintf("orbit-tenant-%s", t.Name)
}
