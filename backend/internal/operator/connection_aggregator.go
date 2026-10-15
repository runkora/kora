// Connection aggregator for the credential broker (Amendment 3 — GitOps).
//
// The egress gateway runs in orbit-system but needs every tenant's connection
// config + secret material to broker outbound CONNECTs. Pods can only mount
// Secrets/ConfigMaps from their own namespace, so the operator aggregates each
// tenant's Connection CRs (in the tenant namespace) plus their referenced
// Secrets into two orbit-system objects the gateway mounts:
//
//   - ConfigMap orbit-tenant-connections — one data key, connections.json, a
//     JSON array of registry entries (tenant, name, adapter, base_host,
//     secret_ref, config). The non-secret metadata only.
//   - Secret orbit-tenant-connection-secrets — keyed by the same flat
//     secret_ref filenames, each holding the referenced Secret's raw material.
//
// This mirrors tenant_ca_aggregator.go exactly: the gateway already receives
// its most security-critical per-tenant material — the per-tenant CA — this
// way, so connections take the same path rather than inventing a second one.
//
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md,
// "Amendment 3 — 2026-06-12".
package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformconns "github.com/clear-route/orbit/internal/platform/connections"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	// aggregatedConnectionsConfigMapName is the ConfigMap in orbit-system that
	// collects every tenant's connection registry metadata. Its single data key
	// is connections.json — the JSON array the gateway's ConfigMap-backed
	// registry reader parses.
	aggregatedConnectionsConfigMapName = "orbit-tenant-connections"

	// aggregatedConnectionsConfigMapKey is the data key within the ConfigMap.
	aggregatedConnectionsConfigMapKey = "connections.json"

	// aggregatedConnectionSecretsName is the Secret in orbit-system that
	// collects every connection's secret material, keyed by the flat secret_ref
	// filenames the registry entries reference. The gateway's k8sfile resolver
	// reads {ConnectionSecretRoot}/<secret_ref>.
	aggregatedConnectionSecretsName = "orbit-tenant-connection-secrets"
)

// secretRefSanitize replaces any character outside [-_a-zA-Z0-9] with "_" so a
// tenant or connection name can be folded into a flat, slash-free secret_ref
// segment. NOTE: "." is deliberately EXCLUDED from the allowed class even though
// Kubernetes Secret data keys permit it. connectionSecretRef joins the two
// sanitized segments with "." as the only structural separator; if "." survived
// inside a segment, the boundary would be ambiguous and refs would collide
// across tenants — e.g. tenant "a"+conn "b.c" and tenant "a.b"+conn "c" both
// fold to "a.b.c.key", which is cross-tenant credential bleed in the
// cluster-wide aggregated Secret. Replacing intra-segment dots makes the two
// joining dots (plus the trailing ".key") the only dots in the ref, so the
// segmentation is unambiguous.
var secretRefSanitize = regexp.MustCompile(`[^-_a-zA-Z0-9]`)

// connectionRegistryEntry is one element of connections.json — the runtime
// contract the egress gateway's registry reader agrees on. tenant_id MUST equal
// the value the gateway sees as claims.Orbit.TenantID (the OrbitTenant CR name,
// the same key the CA aggregator uses for tenant CA lookup).
type connectionRegistryEntry struct {
	TenantID  string          `json:"tenant_id"`
	Name      string          `json:"name"`
	Adapter   string          `json:"adapter"`
	BaseHost  string          `json:"base_host"`
	SecretRef string          `json:"secret_ref"`
	Config    json.RawMessage `json:"config,omitempty"`

	// CACert is the authority the gateway verifies this upstream against,
	// carried straight from spec.caCert.
	CACert string `json:"ca_cert,omitempty"`
}

// ConnectionAggregator reads each active tenant's Connection CRs from its
// namespace, plus each referenced Secret, and renders the aggregated
// orbit-tenant-connections ConfigMap and orbit-tenant-connection-secrets Secret
// in orbit-system. Re-run on every reconcile (and on any Connection or
// referenced-Secret change) so a newly-applied Connection appears in the
// gateway's mount within a kubelet projection-update cycle.
type ConnectionAggregator struct {
	client client.Client
	logger logging.Logger
}

// NewConnectionAggregator creates a ConnectionAggregator backed by the provided
// client.
func NewConnectionAggregator(c client.Client, logger logging.Logger) *ConnectionAggregator {
	return &ConnectionAggregator{client: c, logger: logger}
}

// Sync collects every active tenant's Connection registry entries and secret
// material and upserts the aggregated ConfigMap + Secret. Connections whose
// referenced Secret (or key) is absent are skipped — logged and continued, never
// failing the whole batch — so one misconfigured connection cannot stall every
// other tenant's egress.
func (a *ConnectionAggregator) Sync(ctx context.Context) error {
	tenants, err := a.listActiveTenants(ctx)
	if err != nil {
		return fmt.Errorf("list active tenants: %w", err)
	}

	entries, secretData, err := a.collect(ctx, tenants)
	if err != nil {
		return fmt.Errorf("collect connections: %w", err)
	}

	registryJSON, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal connection registry: %w", err)
	}

	if err := a.upsertConfigMap(ctx, string(registryJSON), len(entries)); err != nil {
		return err
	}
	return a.upsertSecret(ctx, secretData)
}

// listActiveTenants returns all OrbitTenant resources whose Spec.Active is true.
// Suspended tenants are excluded so the gateway will not broker their
// connections — matching the CA aggregator's behavior.
func (a *ConnectionAggregator) listActiveTenants(ctx context.Context) ([]OrbitTenant, error) {
	list := &OrbitTenantList{}
	if err := a.client.List(ctx, list); err != nil {
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

// collect walks every active tenant's Connection CRs and their referenced
// Secrets, producing the registry entries and the flat secret keyspace. The two
// are kept consistent: an entry is only emitted when its secret material is
// successfully read, so connections.json never references a key the Secret
// lacks.
func (a *ConnectionAggregator) collect(ctx context.Context, tenants []OrbitTenant) ([]connectionRegistryEntry, map[string][]byte, error) {
	var entries []connectionRegistryEntry
	secretData := make(map[string][]byte)

	for i := range tenants {
		t := &tenants[i]
		ns := tenantNamespace(t)

		list := &ConnectionList{}
		if err := a.client.List(ctx, list, client.InNamespace(ns)); err != nil {
			return nil, nil, fmt.Errorf("list Connections in %q: %w", ns, err)
		}

		// Earlier connections win a conflict, matching the admission webhook.
		sort.Slice(list.Items, func(i, j int) bool { return createdBefore(&list.Items[i], &list.Items[j]) })
		var kept []*Connection
		for j := range list.Items {
			conn := &list.Items[j]

			if err := platformconns.ValidateBaseHost(conn.Spec.Adapter, conn.Spec.BaseHost); err != nil {
				a.logger.Warn(ctx, "connection has invalid base_host, skipping",
					logging.Field{Key: "connection", Value: conn.Name},
					logging.Field{Key: "namespace", Value: ns},
					logging.Field{Key: "baseHost", Value: conn.Spec.BaseHost},
					logging.Field{Key: "error", Value: err.Error()},
				)
				continue
			}

			if earlier := firstConflict(conn, kept); earlier != nil {
				a.logger.Warn(ctx, "connection conflicts with an earlier connection on the same host, skipping",
					logging.Field{Key: "connection", Value: conn.Name},
					logging.Field{Key: "conflictsWith", Value: earlier.Name},
					logging.Field{Key: "namespace", Value: ns},
				)
				continue
			}
			// Kept before the Secret read, so a late Secret never flips the winner.
			kept = append(kept, conn)

			// tenant_id MUST equal claims.Orbit.TenantID — the OrbitTenant CR
			// name, the identifier the CA aggregator keys on. Do not invent one.
			tenantID := t.Name
			secretRef := connectionSecretRef(tenantID, conn.Name)

			material, ok := a.readConnectionSecret(ctx, ns, conn)
			if !ok {
				// Missing/empty Secret or key — log + continue. The next
				// reconcile picks it up once the secret pipeline materializes it.
				continue
			}
			secretData[secretRef] = material

			entries = append(entries, connectionRegistryEntry{
				TenantID:  tenantID,
				Name:      conn.Name,
				Adapter:   conn.Spec.Adapter,
				BaseHost:  conn.Spec.BaseHost,
				SecretRef: secretRef,
				Config:    rawConfig(conn.Spec.Config),
				CACert:    conn.Spec.CACert,
			})
		}
	}

	// Stable order so the rendered ConfigMap/Secret don't churn on every
	// reconcile purely from map/list iteration order.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].TenantID != entries[j].TenantID {
			return entries[i].TenantID < entries[j].TenantID
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, secretData, nil
}

// firstConflict returns the first of kept that the egress gateway could not
// tell apart from conn, or nil.
func firstConflict(conn *Connection, kept []*Connection) *Connection {
	for _, k := range kept {
		if connectionsConflict(conn, k) {
			return k
		}
	}
	return nil
}

// readConnectionSecret resolves the secret material for one Connection from its
// referenced Secret in the tenant namespace. Returns (nil, false) — logging the
// reason — when the Secret is absent, the named key is missing, or the Secret
// has no single key to default to. Never returns an error: a missing secret is
// a skip, not a batch failure.
func (a *ConnectionAggregator) readConnectionSecret(ctx context.Context, ns string, conn *Connection) ([]byte, bool) {
	secret := &corev1.Secret{}
	err := a.client.Get(ctx, client.ObjectKey{Name: conn.Spec.SecretRef, Namespace: ns}, secret)
	if apierrors.IsNotFound(err) {
		a.logger.Warn(ctx, "connection Secret not yet provisioned, skipping",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "namespace", Value: ns},
			logging.Field{Key: "secretRef", Value: conn.Spec.SecretRef},
		)
		return nil, false
	}
	if err != nil {
		a.logger.Error(ctx, "failed to read connection Secret, skipping",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "namespace", Value: ns},
			logging.Field{Key: "secretRef", Value: conn.Spec.SecretRef},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, false
	}

	material, ok := selectSecretKey(secret.Data, conn.Spec.SecretKey)
	if !ok {
		a.logger.Warn(ctx, "connection Secret has no usable key, skipping",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "namespace", Value: ns},
			logging.Field{Key: "secretRef", Value: conn.Spec.SecretRef},
			logging.Field{Key: "secretKey", Value: conn.Spec.SecretKey},
		)
		return nil, false
	}
	return material, true
}

// selectSecretKey picks the material from a Secret's data. If key is set it must
// be present; if key is empty the Secret must have exactly one key, which is
// used. Returns false otherwise.
func selectSecretKey(data map[string][]byte, key string) ([]byte, bool) {
	if key != "" {
		v, ok := data[key]
		return v, ok
	}
	if len(data) != 1 {
		return nil, false
	}
	for _, v := range data {
		return v, true
	}
	return nil, false
}

// connectionSecretRef builds the opaque, flat, Secret-data-key-safe filename the
// registry entry references and the aggregated Secret is keyed by. Shape:
// sanitize(tenant)+"."+sanitize(name)+".key". secretRefSanitize replaces every
// "." inside each segment (see its doc for why), so the only dots in the result
// are the two structural separators — the (tenant, name) pair maps to a unique
// ref and cannot collide across tenants. The dotRun collapse below is a
// defense-in-depth backstop against any ".." path-traversal token reaching the
// gateway's k8sfile resolver; with intra-segment dots sanitized it is a no-op in
// practice.
func connectionSecretRef(tenant, name string) string {
	ref := secretRefSanitize.ReplaceAllString(tenant, "_") + "." +
		secretRefSanitize.ReplaceAllString(name, "_") + ".key"
	return dotRun.ReplaceAllString(ref, ".")
}

// dotRun matches a run of two or more dots, collapsed to one in secret_ref as a
// path-traversal backstop.
var dotRun = regexp.MustCompile(`\.{2,}`)

// rawConfig returns the connection's adapter config as JSON, normalizing an
// empty RawExtension to a "{}" object so the gateway always parses a valid
// object.
func rawConfig(ext runtime.RawExtension) json.RawMessage {
	if len(ext.Raw) == 0 {
		return json.RawMessage("{}")
	}
	return ext.Raw
}

// upsertConfigMap creates or patches the aggregated connections ConfigMap.
// Skipped if unchanged to avoid spurious kubelet projection updates.
func (a *ConnectionAggregator) upsertConfigMap(ctx context.Context, registryJSON string, count int) error {
	existing := &corev1.ConfigMap{}
	getErr := a.client.Get(ctx, client.ObjectKey{
		Namespace: systemNamespace,
		Name:      aggregatedConnectionsConfigMapName,
	}, existing)

	switch {
	case apierrors.IsNotFound(getErr):
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      aggregatedConnectionsConfigMapName,
				Namespace: systemNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "orbit-operator",
					"orbit.io/component":           "tenant-connections",
				},
			},
			Data: map[string]string{aggregatedConnectionsConfigMapKey: registryJSON},
		}
		if err := a.client.Create(ctx, cm); err != nil {
			return fmt.Errorf("create %s ConfigMap: %w", aggregatedConnectionsConfigMapName, err)
		}
		a.logger.Info(ctx, "created aggregated connections ConfigMap",
			logging.Field{Key: "configmap", Value: aggregatedConnectionsConfigMapName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "connections", Value: count},
		)
		return nil
	case getErr != nil:
		return fmt.Errorf("get %s ConfigMap: %w", aggregatedConnectionsConfigMapName, getErr)
	default:
		if existing.Data[aggregatedConnectionsConfigMapKey] == registryJSON {
			a.logger.Debug(ctx, "aggregated connections unchanged, skipping update")
			return nil
		}
		patch := client.MergeFrom(existing.DeepCopy())
		if existing.Data == nil {
			existing.Data = make(map[string]string)
		}
		existing.Data[aggregatedConnectionsConfigMapKey] = registryJSON
		if err := a.client.Patch(ctx, existing, patch); err != nil {
			return fmt.Errorf("patch %s ConfigMap: %w", aggregatedConnectionsConfigMapName, err)
		}
		a.logger.Info(ctx, "updated aggregated connections ConfigMap",
			logging.Field{Key: "configmap", Value: aggregatedConnectionsConfigMapName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "connections", Value: count},
		)
		return nil
	}
}

// upsertConnectionSecret-equivalent: creates or patches the aggregated
// connection-secrets Secret. Skipped if unchanged.
func (a *ConnectionAggregator) upsertSecret(ctx context.Context, data map[string][]byte) error {
	existing := &corev1.Secret{}
	getErr := a.client.Get(ctx, client.ObjectKey{
		Namespace: systemNamespace,
		Name:      aggregatedConnectionSecretsName,
	}, existing)

	switch {
	case apierrors.IsNotFound(getErr):
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      aggregatedConnectionSecretsName,
				Namespace: systemNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "orbit-operator",
					"orbit.io/component":           "tenant-connection-secrets",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}
		if err := a.client.Create(ctx, s); err != nil {
			return fmt.Errorf("create %s Secret: %w", aggregatedConnectionSecretsName, err)
		}
		a.logger.Info(ctx, "created aggregated connection-secrets Secret",
			logging.Field{Key: "secret", Value: aggregatedConnectionSecretsName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "secrets", Value: len(data)},
		)
		return nil
	case getErr != nil:
		return fmt.Errorf("get %s Secret: %w", aggregatedConnectionSecretsName, getErr)
	default:
		if dataEqual(existing.Data, data) {
			a.logger.Debug(ctx, "aggregated connection-secrets unchanged, skipping update")
			return nil
		}
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Data = data
		if err := a.client.Patch(ctx, existing, patch); err != nil {
			return fmt.Errorf("patch %s Secret: %w", aggregatedConnectionSecretsName, err)
		}
		a.logger.Info(ctx, "updated aggregated connection-secrets Secret",
			logging.Field{Key: "secret", Value: aggregatedConnectionSecretsName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "secrets", Value: len(data)},
		)
		return nil
	}
}
