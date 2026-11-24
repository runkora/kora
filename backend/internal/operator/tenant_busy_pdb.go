package operator

import (
	"context"
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The PodDisruptionBudgets that refuse a voluntary eviction of a pod a run holds
// (docs/adr/2026-11-22-agent-cold-tier.md, Amendment 1). kora-agents labels a
// bound pod busy when it places a run and removes the label when the pod is idle.
const (
	agentBusyPDBName   = "agent-busy"
	sandboxBusyPDBName = "sandbox-busy"
)

// ensureBusyPDBs reconciles the agent-busy and sandbox-busy PodDisruptionBudgets in the tenant namespace.
func (r *TenantReconciler) ensureBusyPDBs(ctx context.Context, tenant *KoraTenant) error {
	ns := tenantNamespace(tenant)
	for _, desired := range []*policyv1.PodDisruptionBudget{
		busyPDB(ns, agentBusyPDBName, tenant.Name, map[string]string{"runkora.io/busy": "true"}),
		busyPDB(ns, sandboxBusyPDBName, tenant.Name, map[string]string{"runkora.io/role": "sandbox"}),
	} {
		if err := r.ensurePDB(ctx, desired); err != nil {
			return err
		}
	}
	return nil
}

// ensurePDB creates the PodDisruptionBudget, or patches an existing one back to the desired spec and labels.
func (r *TenantReconciler) ensurePDB(ctx context.Context, desired *policyv1.PodDisruptionBudget) error {
	existing := &policyv1.PodDisruptionBudget{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		// The cache can miss a budget created a moment ago. The next reconcile patches it.
		if err = r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create PodDisruptionBudget %q in %q: %w", desired.Name, desired.Namespace, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get PodDisruptionBudget %q in %q: %w", desired.Name, desired.Namespace, err)
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch PodDisruptionBudget %q in %q: %w", desired.Name, desired.Namespace, err)
	}
	return nil
}

// busyPDB allows no voluntary eviction of a pod that matches selector.
func busyPDB(ns, name, tenantName string, selector map[string]string) *policyv1.PodDisruptionBudget {
	none := intstr.FromInt32(0)
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: tenantPolicyLabels(tenantName)},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &none,
			Selector:       &metav1.LabelSelector{MatchLabels: selector},
		},
	}
}
