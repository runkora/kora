package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
)

// SandboxBase is the install's base sandbox image. Every tenant namespace
// registers it as the SandboxImage kora-base.
type SandboxBase struct {
	Image string
	Warm  bool
}

// ensureBaseSandboxImage keeps kora-base pointing at the install's base image.
// Every reconcile replaces its whole spec and drops a field a tenant added.
func (r *TenantReconciler) ensureBaseSandboxImage(ctx context.Context, tenant *OrbitTenant) error {
	desired := baseSandboxImage(tenantNamespace(tenant), tenant.Name, r.SandboxBase)
	existing := &si.SandboxImage{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		if err = r.Create(ctx, desired); err != nil {
			return fmt.Errorf("create SandboxImage %q in %q: %w", desired.Name, desired.Namespace, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get SandboxImage %q in %q: %w", desired.Name, desired.Namespace, err)
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err = r.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch SandboxImage %q in %q: %w", desired.Name, desired.Namespace, err)
	}
	return nil
}

func baseSandboxImage(ns, tenantName string, base SandboxBase) *si.SandboxImage {
	return &si.SandboxImage{
		TypeMeta:   metav1.TypeMeta{APIVersion: si.GroupVersion.String(), Kind: "SandboxImage"},
		ObjectMeta: metav1.ObjectMeta{Name: si.BaseName, Namespace: ns, Labels: tenantPolicyLabels(tenantName)},
		Spec: si.Spec{
			Image:       base.Image,
			Description: si.BaseDescription,
			Warm:        base.Warm,
		},
	}
}
