package operator

import (
	"context"
	"fmt"
	"maps"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
)

// SandboxBase is the install's base sandbox image, registered in every tenant
// namespace as the SandboxImage kora-base.
type SandboxBase struct {
	Image string
	Warm  bool
}

// ensureBaseSandboxImage keeps kora-base pointing at the install's base image.
// Every reconcile replaces its whole spec. A field a tenant added, which a
// server-side apply would keep, does not survive the next reconcile.
func (r *TenantReconciler) ensureBaseSandboxImage(ctx context.Context, tenant *OrbitTenant) error {
	desired := baseSandboxImage(tenantNamespace(tenant), tenant.Name, r.SandboxBase)
	live := &si.SandboxImage{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), live)
	if apierrors.IsNotFound(err) {
		if err = r.Create(ctx, desired); err != nil {
			return fmt.Errorf("create SandboxImage %q in %q: %w", desired.Name, desired.Namespace, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get SandboxImage %q in %q: %w", desired.Name, desired.Namespace, err)
	}
	if equality.Semantic.DeepEqual(live.Spec, desired.Spec) && maps.Equal(live.Labels, desired.Labels) {
		return nil
	}
	live.Spec = desired.Spec
	live.Labels = desired.Labels
	if err = r.Update(ctx, live); err != nil {
		return fmt.Errorf("update SandboxImage %q in %q: %w", desired.Name, desired.Namespace, err)
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
