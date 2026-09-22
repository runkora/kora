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

// pullSecretComponent marks a Secret in a tenant namespace as a replica of a
// system pull Secret. The managed-by label alone is too broad to prune on:
// the tenant CA and the aggregated registries carry it too.
const pullSecretComponent = "image-pull-secret"

// ensureImagePullSecrets mirrors configured system pull Secrets into the tenant
// namespace and removes the replicas that are no longer configured, so a
// credential dropped from global.imagePullSecrets is dropped from the tenants
// that received it.
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
					"orbit.io/component":           pullSecretComponent,
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
		case !reflect.DeepEqual(existing.Data, desired.Data) ||
			existing.Type != desired.Type ||
			existing.Labels["orbit.io/component"] != pullSecretComponent:
			// Labels are reconciled alongside the data: a replica created
			// before the component label existed has to gain it, or the prune
			// below can never see it.
			patch := client.MergeFrom(existing.DeepCopy())
			existing.Data = desired.Data
			existing.Type = desired.Type
			existing.Labels = desired.Labels
			if patchErr := r.Patch(ctx, existing, patch); patchErr != nil {
				return fmt.Errorf("patch pull secret %q in %q: %w", name, ns, patchErr)
			}
		}
	}
	return r.pruneImagePullSecrets(ctx, ns)
}

// pruneImagePullSecrets deletes the replicas in the tenant namespace that no
// longer name a configured pull Secret.
func (r *TenantReconciler) pruneImagePullSecrets(ctx context.Context, ns string) error {
	replicas := &corev1.SecretList{}
	if err := r.List(ctx, replicas, client.InNamespace(ns), client.MatchingLabels{
		"app.kubernetes.io/managed-by": "orbit-operator",
		"orbit.io/component":           pullSecretComponent,
	}); err != nil {
		return fmt.Errorf("list pull secrets in %q: %w", ns, err)
	}

	desired := make(map[string]struct{}, len(r.ImagePullSecrets))
	for _, name := range r.ImagePullSecrets {
		desired[name] = struct{}{}
	}

	for i := range replicas.Items {
		secret := &replicas.Items[i]
		if _, keep := desired[secret.Name]; keep {
			continue
		}
		if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete pull secret %q in %q: %w", secret.Name, ns, err)
		}
		r.Logger.Info(ctx, "removed image pull secret replica",
			logging.Field{Key: "secret", Value: secret.Name},
			logging.Field{Key: "namespace", Value: ns},
		)
	}
	return nil
}
