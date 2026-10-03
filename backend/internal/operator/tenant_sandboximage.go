package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
)

// SandboxBase is the install's base sandbox image, registered in every tenant
// namespace as the SandboxImage kora-base.
type SandboxBase struct {
	Image string
	Warm  bool
}

// ensureBaseSandboxImage keeps kora-base pointing at the install's base image.
// Every reconcile restores it, so a tenant edit does not last.
func (r *TenantReconciler) ensureBaseSandboxImage(ctx context.Context, tenant *OrbitTenant) error {
	desired := baseSandboxImage(tenantNamespace(tenant), tenant.Name, r.SandboxBase)
	err := r.Create(ctx, desired)
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := r.Patch(ctx, desired, client.Merge, client.FieldOwner("orbit-operator")); err != nil {
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
			Description: "The kora base image: general command-line tools, Go and Python.",
			Warm:        base.Warm,
		},
	}
}
