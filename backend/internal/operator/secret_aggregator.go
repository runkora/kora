package operator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	// aggregatedSecretName is the Secret in kora-system that collects all
	// per-tenant OIDC client secrets. The identity service mounts this as a
	// volume and hot-reloads it when Kubelet updates the symlink.
	aggregatedSecretName = "kora-tenant-oidc-secrets"

	// aggregatedSecretKey is the key within the aggregated Secret that holds
	// the YAML map of tenantID → clientSecret.
	aggregatedSecretKey = "client-secrets.yaml"

	// sourceSecretKey is the fixed key within each per-tenant Secret from
	// which the OIDC client secret value is read.
	sourceSecretKey = "OIDC_CLIENT_SECRET"
)

// ClientSecretAggregator reads per-tenant OIDC client secrets from the
// Secrets referenced by spec.oidc.clientSecretRef and writes a single
// aggregated Secret (kora-tenant-oidc-secrets) in kora-system.
//
// The identity service mounts this Secret as a file and hot-reloads it
// via fsnotify when Kubelet writes a new symlink — tenant additions and
// secret rotations never require an identity service pod restart.
//
// See docs/adr/2026-03-31-oidc-client-secret-projection-for-confidential-tenants.md
type ClientSecretAggregator struct {
	client client.Client
	logger logging.Logger
}

// NewClientSecretAggregator creates an aggregator backed by the provided client.
func NewClientSecretAggregator(c client.Client, logger logging.Logger) *ClientSecretAggregator {
	return &ClientSecretAggregator{client: c, logger: logger}
}

// Sync reads the OIDC client secret for each active KoraTenant that
// specifies a clientSecretRef, then writes (or updates) the aggregated
// Secret in kora-system. Tenants without a clientSecretRef (public OIDC
// clients) are omitted — the identity service treats a missing entry as an
// empty secret, which suppresses the client_secret parameter in token requests.
//
// If a referenced Secret does not exist or is missing the expected key, the
// tenant is skipped with a warning. Partial aggregation is preferred over
// failing the entire sync and degrading unaffected tenants.
func (a *ClientSecretAggregator) Sync(ctx context.Context) error {
	tenants, err := a.listConfidentialTenants(ctx)
	if err != nil {
		return fmt.Errorf("list confidential tenants: %w", err)
	}

	secrets, err := a.collectSecrets(ctx, tenants)
	if err != nil {
		return fmt.Errorf("collect client secrets: %w", err)
	}

	return a.upsertAggregatedSecret(ctx, secrets)
}

// -----------------------------------------------------------------------------
// private helpers
// -----------------------------------------------------------------------------

// listConfidentialTenants returns active tenants that have a clientSecretRef —
// i.e. confidential OIDC clients that require a client_secret in token requests.
func (a *ClientSecretAggregator) listConfidentialTenants(ctx context.Context) ([]KoraTenant, error) {
	list := &KoraTenantList{}
	if err := a.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list KoraTenants: %w", err)
	}
	out := make([]KoraTenant, 0, len(list.Items))
	for _, t := range list.Items {
		if t.Spec.Active && t.Spec.OIDC.ClientSecretRef != nil {
			out = append(out, t)
		}
	}
	return out, nil
}

// collectSecrets fetches the OIDC_CLIENT_SECRET key from each tenant's
// referenced Secret. Missing Secrets and missing keys produce a warning and
// are skipped rather than aborting the sync.
func (a *ClientSecretAggregator) collectSecrets(ctx context.Context, tenants []KoraTenant) (map[string]string, error) {
	out := make(map[string]string, len(tenants))
	for _, t := range tenants {
		ref := t.Spec.OIDC.ClientSecretRef
		ns := ref.Namespace
		if ns == "" {
			ns = systemNamespace
		}

		secret := &corev1.Secret{}
		err := a.client.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ns}, secret)
		if apierrors.IsNotFound(err) {
			a.logger.Warn(ctx, "clientSecretRef not found, skipping tenant in aggregated Secret",
				logging.Field{Key: "tenant", Value: t.Name},
				logging.Field{Key: "secret", Value: ref.Name},
				logging.Field{Key: "namespace", Value: ns},
			)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get Secret %s/%s for tenant %q: %w", ns, ref.Name, t.Name, err)
		}

		val, ok := secret.Data[sourceSecretKey]
		if !ok {
			a.logger.Warn(ctx, "clientSecretRef missing expected key, skipping tenant in aggregated Secret",
				logging.Field{Key: "tenant", Value: t.Name},
				logging.Field{Key: "secret", Value: ref.Name},
				logging.Field{Key: "key", Value: sourceSecretKey},
			)
			continue
		}

		out[t.Name] = string(val)
	}
	return out, nil
}

// upsertAggregatedSecret creates or patches kora-tenant-oidc-secrets.
// It is a no-op when the content is unchanged to avoid spurious Kubelet
// volume re-mounts.
func (a *ClientSecretAggregator) upsertAggregatedSecret(ctx context.Context, secrets map[string]string) error {
	payload, err := sigsyaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("marshal aggregated secrets: %w", err)
	}

	existing := &corev1.Secret{}
	getErr := a.client.Get(ctx, client.ObjectKey{
		Namespace: systemNamespace,
		Name:      aggregatedSecretName,
	}, existing)

	switch {
	case apierrors.IsNotFound(getErr):
		return a.createAggregatedSecret(ctx, payload)
	case getErr != nil:
		return fmt.Errorf("get %s Secret: %w", aggregatedSecretName, getErr)
	default:
		if string(existing.Data[aggregatedSecretKey]) == string(payload) {
			a.logger.Debug(ctx, "aggregated client secrets unchanged, skipping update")
			return nil
		}
		return a.updateAggregatedSecret(ctx, existing, payload)
	}
}

func (a *ClientSecretAggregator) createAggregatedSecret(ctx context.Context, payload []byte) error {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aggregatedSecretName,
			Namespace: systemNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kora-operator",
				"runkora.io/component":         "tenant-client-secrets",
			},
		},
		Data: map[string][]byte{
			aggregatedSecretKey: payload,
		},
	}
	if err := a.client.Create(ctx, s); err != nil {
		return fmt.Errorf("create %s Secret: %w", aggregatedSecretName, err)
	}
	a.logger.Info(ctx, "created aggregated client secrets Secret",
		logging.Field{Key: "secret", Value: aggregatedSecretName},
		logging.Field{Key: "namespace", Value: systemNamespace},
	)
	return nil
}

func (a *ClientSecretAggregator) updateAggregatedSecret(ctx context.Context, existing *corev1.Secret, payload []byte) error {
	patch := client.MergeFrom(existing.DeepCopy())
	if existing.Data == nil {
		existing.Data = make(map[string][]byte)
	}
	existing.Data[aggregatedSecretKey] = payload
	if err := a.client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch %s Secret: %w", aggregatedSecretName, err)
	}
	a.logger.Info(ctx, "updated aggregated client secrets Secret",
		logging.Field{Key: "secret", Value: aggregatedSecretName},
		logging.Field{Key: "namespace", Value: systemNamespace},
	)
	return nil
}
