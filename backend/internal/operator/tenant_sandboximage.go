package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
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

// BundledSandboxImage is one bundled image as the install configures it.
type BundledSandboxImage struct {
	Image   string
	Enabled bool
	Warm    bool
}

// ensureBundledSandboxImages registers each enabled bundled image in the tenant
// namespace and removes each disabled one. It patches and deletes only the
// SandboxImages it created. A tenant's own SandboxImage under a bundled name is
// left alone, and that tenant does not receive the bundled image.
func (r *TenantReconciler) ensureBundledSandboxImages(ctx context.Context, tenant *OrbitTenant) error {
	ns := tenantNamespace(tenant)
	for _, b := range si.Bundled {
		cfg := r.SandboxBundled[b.Key]
		existing := &si.SandboxImage{}
		err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: b.Name}, existing)
		found := err == nil
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("get SandboxImage %q in %q: %w", b.Name, ns, err)
		}
		switch {
		case found && !ownsBundledSandboxImage(existing):
			if cfg.Enabled {
				r.Logger.Info(ctx, "tenant SandboxImage uses a bundled image name; bundled image not registered",
					logging.Field{Key: "tenant", Value: tenant.Name},
					logging.Field{Key: "namespace", Value: ns},
					logging.Field{Key: "name", Value: b.Name},
				)
			}
		case !cfg.Enabled:
			if found {
				if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
					return fmt.Errorf("delete SandboxImage %q in %q: %w", b.Name, ns, err)
				}
			}
		case !found:
			if err := r.Create(ctx, bundledSandboxImage(ns, tenant.Name, b, cfg)); err != nil {
				return fmt.Errorf("create SandboxImage %q in %q: %w", b.Name, ns, err)
			}
		default:
			desired := bundledSandboxImage(ns, tenant.Name, b, cfg)
			patch := client.MergeFrom(existing.DeepCopy())
			existing.Spec = desired.Spec
			existing.Labels = desired.Labels
			if err := r.Patch(ctx, existing, patch); err != nil {
				return fmt.Errorf("patch SandboxImage %q in %q: %w", b.Name, ns, err)
			}
		}
	}
	return nil
}

// ownsBundledSandboxImage reports whether the operator created img as a bundled image.
func ownsBundledSandboxImage(img *si.SandboxImage) bool {
	return img.Labels["app.kubernetes.io/managed-by"] == "orbit-operator" &&
		img.Labels["orbit.io/component"] == si.BundledComponent
}

func bundledSandboxImage(ns, tenantName string, b si.BundledImage, cfg BundledSandboxImage) *si.SandboxImage {
	labels := tenantPolicyLabels(tenantName)
	labels["orbit.io/component"] = si.BundledComponent
	return &si.SandboxImage{
		TypeMeta:   metav1.TypeMeta{APIVersion: si.GroupVersion.String(), Kind: "SandboxImage"},
		ObjectMeta: metav1.ObjectMeta{Name: b.Name, Namespace: ns, Labels: labels},
		Spec: si.Spec{
			Image:       cfg.Image,
			Description: b.Description,
			Warm:        cfg.Warm,
		},
	}
}
