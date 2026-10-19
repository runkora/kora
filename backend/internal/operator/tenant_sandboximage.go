package operator

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/platform/logging"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

// SandboxBase is the install's base sandbox image. Every tenant namespace
// registers it as the SandboxImage kora-base.
type SandboxBase struct {
	Image string
	Warm  bool
}

// ensureBaseSandboxImage keeps kora-base pointing at the install's base image.
// Every reconcile replaces its whole spec and drops a field a tenant added.
func (r *TenantReconciler) ensureBaseSandboxImage(ctx context.Context, tenant *KoraTenant) error {
	desired := baseSandboxImage(tenantNamespace(tenant), tenant.Name, r.SandboxBase, r.ProductName)
	existing, err := r.getSandboxImage(ctx, desired.Namespace, desired.Name)
	if err != nil {
		return err
	}
	return r.upsertSandboxImage(ctx, desired, existing)
}

// getSandboxImage returns the named SandboxImage, or nil when there is none.
func (r *TenantReconciler) getSandboxImage(ctx context.Context, ns, name string) (*si.SandboxImage, error) {
	img := &si.SandboxImage{}
	err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, img)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get SandboxImage %q in %q: %w", name, ns, err)
	}
	return img, nil
}

// upsertSandboxImage creates desired when existing is nil, and otherwise
// replaces the spec and labels of existing with those of desired.
func (r *TenantReconciler) upsertSandboxImage(ctx context.Context, desired, existing *si.SandboxImage) error {
	if existing == nil {
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("create SandboxImage %q in %q: %w", desired.Name, desired.Namespace, err)
		}
		return nil
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch SandboxImage %q in %q: %w", desired.Name, desired.Namespace, err)
	}
	return nil
}

// product is the configured product name the description reads.
func baseSandboxImage(ns, tenantName string, base SandboxBase, product string) *si.SandboxImage {
	return &si.SandboxImage{
		TypeMeta:   metav1.TypeMeta{APIVersion: si.GroupVersion.String(), Kind: "SandboxImage"},
		ObjectMeta: metav1.ObjectMeta{Name: si.BaseName, Namespace: ns, Labels: tenantPolicyLabels(tenantName)},
		Spec: si.Spec{
			Image:       base.Image,
			Description: si.BaseDescription(product),
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
// left alone, and that tenant does not receive the bundled image. A failure on
// one image does not stop the others.
func (r *TenantReconciler) ensureBundledSandboxImages(ctx context.Context, tenant *KoraTenant) error {
	ns := tenantNamespace(tenant)
	var errs []error
	for _, b := range si.Bundled {
		cfg := r.SandboxBundled[b.Key]
		desired := bundledSandboxImage(ns, tenant.Name, b, cfg, r.ProductName)
		existing, err := r.getSandboxImage(ctx, ns, b.Name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch {
		case existing != nil && !ownsBundledSandboxImage(existing):
			if cfg.Enabled {
				r.Logger.Debug(ctx, "tenant SandboxImage uses a bundled image name; bundled image not registered",
					logging.Field{Key: "tenant", Value: tenant.Name},
					logging.Field{Key: "namespace", Value: ns},
					logging.Field{Key: "name", Value: b.Name},
				)
			}
		case !cfg.Enabled:
			if existing != nil {
				if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
					errs = append(errs, fmt.Errorf("delete SandboxImage %q in %q: %w", b.Name, ns, err))
				}
			}
		default:
			if err := r.upsertSandboxImage(ctx, desired, existing); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// ownsBundledSandboxImage reports whether the operator created img as a bundled image.
func ownsBundledSandboxImage(img *si.SandboxImage) bool {
	return img.Labels["app.kubernetes.io/managed-by"] == "orbit-operator" &&
		img.Labels["orbit.io/component"] == si.BundledComponent
}

// product is the configured product name the description reads.
func bundledSandboxImage(ns, tenantName string, b si.BundledImage, cfg BundledSandboxImage, product string) *si.SandboxImage {
	labels := tenantPolicyLabels(tenantName)
	labels["orbit.io/component"] = si.BundledComponent
	return &si.SandboxImage{
		TypeMeta:   metav1.TypeMeta{APIVersion: si.GroupVersion.String(), Kind: "SandboxImage"},
		ObjectMeta: metav1.ObjectMeta{Name: b.Name, Namespace: ns, Labels: labels},
		Spec: si.Spec{
			Image:       cfg.Image,
			Description: b.Description(product),
			Warm:        cfg.Warm,
		},
	}
}
