// Per-tenant CA aggregator for the credential broker (Phase 4b).
//
// The egress gateway runs in orbit-system but needs every tenant's CA cert
// + key to mint leaves on outbound CONNECTs. Pods can only mount Secrets
// from their own namespace, so the operator aggregates each tenant's
// orbit-tenant-ca Secret (in the tenant namespace) into a single
// orbit-tenant-cas Secret in orbit-system. The egress-gateway pod projects
// that aggregated Secret as a volume; cabundle.LoadEager reads the flat
// {tenant}.ca.{crt,key} keys it produces.
//
// Same shape as ClientSecretAggregator (which aggregates OIDC client
// secrets for the identity service) — kept as a separate type so the two
// concerns evolve independently.
package operator

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// aggregatedTenantCASecretName is the Secret in orbit-system that
// collects every tenant's CA cert + key. Its data keys are
// "{tenant}.ca.crt" / "{tenant}.ca.key" — the layout cabundle.LoadEager
// expects when the Secret is projected as a volume.
const aggregatedTenantCASecretName = "orbit-tenant-cas"

// TenantCAAggregator reads each active OrbitTenant's orbit-tenant-ca
// Secret from its tenant namespace and writes a single aggregated Secret
// in orbit-system. Re-run on every reconcile so a newly-provisioned
// tenant's CA appears in the gateway's mount within a kubelet
// projection-update cycle (typically <60s).
type TenantCAAggregator struct {
	client client.Client
	logger logging.Logger
}

// NewTenantCAAggregator creates a TenantCAAggregator backed by the
// provided client.
func NewTenantCAAggregator(c client.Client, logger logging.Logger) *TenantCAAggregator {
	return &TenantCAAggregator{client: c, logger: logger}
}

// Sync collects every active tenant's CA material and upserts the
// aggregated Secret. Tenants whose CA Secret is absent (e.g. a brand-new
// tenant whose ensureTenantCA has not yet completed in this reconcile
// cycle) are skipped — the next reconcile will pick them up. Partial
// aggregation is preferred over failing the entire sync.
func (a *TenantCAAggregator) Sync(ctx context.Context) error {
	tenants, err := a.listActiveTenants(ctx)
	if err != nil {
		return fmt.Errorf("list active tenants: %w", err)
	}

	data, err := a.collectCAMaterial(ctx, tenants)
	if err != nil {
		return fmt.Errorf("collect tenant CAs: %w", err)
	}

	return a.upsert(ctx, data)
}

// listActiveTenants returns all OrbitTenant resources whose Spec.Active is
// true. Suspended/inactive tenants are excluded from the aggregated Secret
// so the gateway will refuse CONNECTs from their sandboxes — there are
// none currently running, but this future-proofs the aggregator against
// a tenant suspension while a sandbox is somehow still alive.
func (a *TenantCAAggregator) listActiveTenants(ctx context.Context) ([]OrbitTenant, error) {
	list := &OrbitTenantList{}
	if err := a.client.List(ctx, list, tenantListOpts()...); err != nil {
		return nil, fmt.Errorf("list OrbitTenants: %w", err)
	}
	out := make([]OrbitTenant, 0, len(list.Items))
	for _, t := range list.Items {
		if t.Spec.Active {
			out = append(out, t)
		}
	}
	return out, nil
}

// collectCAMaterial reads each tenant's orbit-tenant-ca Secret from its
// tenant namespace and returns the aggregated keyspace. The keys produced
// are "{tenant}.ca.crt" / "{tenant}.ca.key", matching what
// cabundle.LoadEager expects.
func (a *TenantCAAggregator) collectCAMaterial(ctx context.Context, tenants []OrbitTenant) (map[string][]byte, error) {
	out := make(map[string][]byte, len(tenants)*2)
	for i := range tenants {
		t := &tenants[i]
		ns := tenantNamespace(t)
		secret := &corev1.Secret{}
		err := a.client.Get(ctx, client.ObjectKey{Name: tenantCASecretName, Namespace: ns}, secret)
		if apierrors.IsNotFound(err) {
			a.logger.Warn(ctx, "tenant CA Secret not yet provisioned, skipping in aggregated Secret",
				logging.Field{Key: "tenant", Value: t.Name},
				logging.Field{Key: "namespace", Value: ns},
			)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get %s/%s: %w", ns, tenantCASecretName, err)
		}

		certBytes, certOK := secret.Data["ca.crt"]
		keyBytes, keyOK := secret.Data["ca.key"]
		if !certOK || !keyOK {
			a.logger.Warn(ctx, "tenant CA Secret missing ca.crt or ca.key, skipping",
				logging.Field{Key: "tenant", Value: t.Name},
			)
			continue
		}

		out[t.Name+".ca.crt"] = certBytes
		out[t.Name+".ca.key"] = keyBytes
	}
	return out, nil
}

// upsert creates or patches the aggregated Secret. Skipped if the content
// is unchanged to avoid spurious kubelet projection updates that would
// briefly stale the gateway's mount.
func (a *TenantCAAggregator) upsert(ctx context.Context, data map[string][]byte) error {
	existing := &corev1.Secret{}
	getErr := a.client.Get(ctx, client.ObjectKey{
		Namespace: systemNamespace,
		Name:      aggregatedTenantCASecretName,
	}, existing)

	switch {
	case apierrors.IsNotFound(getErr):
		return a.create(ctx, data)
	case getErr != nil:
		return fmt.Errorf("get %s Secret: %w", aggregatedTenantCASecretName, getErr)
	default:
		if dataEqual(existing.Data, data) {
			a.logger.Debug(ctx, "aggregated tenant CAs unchanged, skipping update")
			return nil
		}
		return a.update(ctx, existing, data)
	}
}

func (a *TenantCAAggregator) create(ctx context.Context, data map[string][]byte) error {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aggregatedTenantCASecretName,
			Namespace: systemNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "orbit-operator",
				"orbit.io/component":           "tenant-cas",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
	if err := a.client.Create(ctx, s); err != nil {
		return fmt.Errorf("create %s Secret: %w", aggregatedTenantCASecretName, err)
	}
	a.logger.Info(ctx, "created aggregated tenant CAs Secret",
		logging.Field{Key: "secret", Value: aggregatedTenantCASecretName},
		logging.Field{Key: "namespace", Value: systemNamespace},
		logging.Field{Key: "tenants", Value: len(data) / 2}, //nolint:mnd // each tenant contributes two keys
	)
	return nil
}

func (a *TenantCAAggregator) update(ctx context.Context, existing *corev1.Secret, data map[string][]byte) error {
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Data = data
	if err := a.client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch %s Secret: %w", aggregatedTenantCASecretName, err)
	}
	a.logger.Info(ctx, "updated aggregated tenant CAs Secret",
		logging.Field{Key: "secret", Value: aggregatedTenantCASecretName},
		logging.Field{Key: "namespace", Value: systemNamespace},
		logging.Field{Key: "tenants", Value: len(data) / 2}, //nolint:mnd
	)
	return nil
}

// dataEqual returns true if two Secret data maps have identical key sets
// and identical values. Order-independent.
func dataEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		bv, ok := b[k]
		if !ok {
			return false
		}
		if !bytes.Equal(a[k], bv) {
			return false
		}
	}
	return true
}
