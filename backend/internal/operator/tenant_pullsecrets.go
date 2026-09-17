package operator

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ensureImagePullSecrets mirrors configured system pull Secrets into the tenant namespace.
func (r *TenantReconciler) ensureImagePullSecrets(ctx context.Context, tenant *OrbitTenant) error {
	ns := tenantNamespace(tenant)
	for _, name := range r.ImagePullSecrets {
		src := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: systemNamespace}, src); err != nil {
			return fmt.Errorf("get pull secret %q in %q: %w", name, systemNamespace, err)
		}
		desired := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "orbit-operator",
					"orbit.io/tenant":              tenant.Name,
				},
			},
			Type: src.Type,
			Data: src.Data,
		}
		existing := &corev1.Secret{}
		err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, existing)
		switch {
		case apierrors.IsNotFound(err):
			if createErr := r.Create(ctx, desired); createErr != nil {
				return fmt.Errorf("create pull secret %q in %q: %w", name, ns, createErr)
			}
			r.Logger.Info(ctx, "replicated image pull secret",
				logging.Field{Key: "secret", Value: name},
				logging.Field{Key: "namespace", Value: ns},
			)
		case err != nil:
			return fmt.Errorf("get pull secret %q in %q: %w", name, ns, err)
		case !reflect.DeepEqual(existing.Data, desired.Data) || existing.Type != desired.Type:
			patch := client.MergeFrom(existing.DeepCopy())
			existing.Data = desired.Data
			existing.Type = desired.Type
			if patchErr := r.Patch(ctx, existing, patch); patchErr != nil {
				return fmt.Errorf("patch pull secret %q in %q: %w", name, ns, patchErr)
			}
		}
	}
	return nil
}
