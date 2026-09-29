// OAuthClient aggregator for the identity authorization server (GitOps
// hosted-app registration).
//
// The identity service runs in orbit-system but needs every tenant's
// pre-registered OAuth clients to serve the authorize endpoint. Pods can only
// mount ConfigMaps from their own namespace, so the operator aggregates each
// tenant's OAuthClient CRs (in the tenant namespace) into one orbit-system
// ConfigMap the identity service mounts:
//
//   - ConfigMap orbit-tenant-oauth-clients — one data key, oauth-clients.json,
//     a JSON array of registry entries (tenant, client_id, client_name,
//     redirect_uris). All non-secret: OAuth clients are public (PKCE only).
//
// This mirrors connection_aggregator.go exactly, minus the Secret half —
// OAuth clients carry no secret material. The identity service reads the
// mounted file (see internal/identity/oauth_static_clients.go) and never
// imports this CRD type.
//
// See docs/adr/2026-07-28-oauth2-web-redirect-clients.md.
package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	// aggregatedOAuthClientsConfigMapName is the ConfigMap in orbit-system that
	// collects every tenant's OAuth client registrations. Its single data key
	// is oauth-clients.json — the JSON array the identity service's mounted
	// static-client source parses.
	aggregatedOAuthClientsConfigMapName = "orbit-tenant-oauth-clients"

	// aggregatedOAuthClientsConfigMapKey is the data key within the ConfigMap.
	aggregatedOAuthClientsConfigMapKey = "oauth-clients.json"
)

// oauthClientRegistryEntry is one element of oauth-clients.json — the runtime
// contract the identity service's static-client source agrees on. TenantID is
// carried for provenance (which tenant declared the client); the authorization
// server itself looks clients up by ClientID alone.
type oauthClientRegistryEntry struct {
	TenantID     string   `json:"tenant_id"`
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name,omitempty"`
	RedirectURIs []string `json:"redirect_uris"`
}

// OAuthClientAggregator reads each active tenant's OAuthClient CRs from its
// namespace and renders the aggregated orbit-tenant-oauth-clients ConfigMap in
// orbit-system. Re-run on every reconcile (and on any OAuthClient change) so a
// newly-applied client appears in the identity service's mount within a
// kubelet projection-update cycle.
type OAuthClientAggregator struct {
	client client.Client
	logger logging.Logger
}

// NewOAuthClientAggregator creates an OAuthClientAggregator backed by the
// provided client.
func NewOAuthClientAggregator(c client.Client, logger logging.Logger) *OAuthClientAggregator {
	return &OAuthClientAggregator{client: c, logger: logger}
}

// Sync collects every active tenant's OAuthClient registry entries and upserts
// the aggregated ConfigMap. A client_id declared by more than one CR (across
// tenants or within one) is emitted once — first by sort order — and the
// collision is logged, never failing the batch, so one duplicate cannot stall
// every other tenant's logins.
func (a *OAuthClientAggregator) Sync(ctx context.Context) error {
	tenants, err := a.listActiveTenants(ctx)
	if err != nil {
		return fmt.Errorf("list active tenants: %w", err)
	}

	entries, err := a.collect(ctx, tenants)
	if err != nil {
		return fmt.Errorf("collect oauth clients: %w", err)
	}

	registryJSON, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal oauth client registry: %w", err)
	}

	return a.upsertConfigMap(ctx, string(registryJSON), len(entries))
}

// listActiveTenants returns all OrbitTenant resources whose Spec.Active is true,
// mirroring the connection aggregator: suspended tenants' clients are not served.
func (a *OAuthClientAggregator) listActiveTenants(ctx context.Context) ([]OrbitTenant, error) {
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

// collect walks every active tenant's OAuthClient CRs and returns the registry
// entries, sorted by client_id. Tenants and the CRs within each namespace are
// visited in name order so the dedup winner for a duplicated client_id is stable
// across reconciles — the Kubernetes list order is not guaranteed, and an
// unstable winner would churn the ConfigMap.
func (a *OAuthClientAggregator) collect(ctx context.Context, tenants []OrbitTenant) ([]oauthClientRegistryEntry, error) {
	sorted := append([]OrbitTenant(nil), tenants...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var entries []oauthClientRegistryEntry
	seen := make(map[string]string) // client_id -> owning tenant (first to claim it)

	for i := range sorted {
		t := &sorted[i]
		clients, err := a.tenantClients(ctx, t)
		if err != nil {
			return nil, err
		}
		for j := range clients {
			if entry, ok := a.entryFor(ctx, t, &clients[j], seen); ok {
				entries = append(entries, entry)
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].ClientID < entries[j].ClientID })
	return entries, nil
}

// tenantClients lists a tenant's OAuthClient CRs, name-sorted so dedup ordering
// is deterministic.
func (a *OAuthClientAggregator) tenantClients(ctx context.Context, t *OrbitTenant) ([]OAuthClient, error) {
	ns := tenantNamespace(t)
	list := &OAuthClientList{}
	if err := a.client.List(ctx, list, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list OAuthClients in %q: %w", ns, err)
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
	return list.Items, nil
}

// entryFor validates one OAuthClient CR and returns its registry entry, or
// (_, false) when it is skipped: an empty client_id or no redirect URIs, an
// invalid redirect URI, or a client_id already claimed by an earlier tenant.
// Every skip is logged and never fatal; a kept entry records its client_id ->
// tenant ownership in seen.
func (a *OAuthClientAggregator) entryFor(ctx context.Context, t *OrbitTenant, oc *OAuthClient, seen map[string]string) (oauthClientRegistryEntry, bool) {
	id := oc.Spec.ClientID
	cr := []logging.Field{
		{Key: "oauthclient", Value: oc.Name},
		{Key: "namespace", Value: tenantNamespace(t)},
	}

	if id == "" || len(oc.Spec.RedirectURIs) == 0 {
		a.logger.Warn(ctx, "oauth client missing clientId or redirectUris, skipping", cr...)
		return oauthClientRegistryEntry{}, false
	}
	if bad, ok := firstInvalidRedirectURI(oc.Spec.RedirectURIs); !ok {
		a.logger.Warn(ctx, "oauth client has an invalid redirect URI, skipping",
			append(cr, logging.Field{Key: "clientId", Value: id}, logging.Field{Key: "redirectUri", Value: bad})...)
		return oauthClientRegistryEntry{}, false
	}
	if kept := seen[id]; kept != "" {
		a.logger.Warn(ctx, "duplicate oauth client_id across CRs, skipping later one",
			append(cr, logging.Field{Key: "clientId", Value: id}, logging.Field{Key: "keptTenant", Value: kept})...)
		return oauthClientRegistryEntry{}, false
	}

	seen[id] = t.Name
	return oauthClientRegistryEntry{
		TenantID:     t.Name,
		ClientID:     id,
		ClientName:   oc.Spec.ClientName,
		RedirectURIs: append([]string(nil), oc.Spec.RedirectURIs...),
	}, true
}

// firstInvalidRedirectURI returns the first redirect URI that is not a
// well-formed http(s) URL (and false), or ("", true) when all are valid. A
// non-loopback host must use https — a plaintext redirect to a remote host
// would leak the authorization code — while loopback may use http (the RFC 8252
// native-app case). A malformed CR is skipped by collect so it never becomes a
// registered redirect target.
func firstInvalidRedirectURI(uris []string) (string, bool) {
	for _, raw := range uris {
		if !validRedirectURI(raw) {
			return raw, false
		}
	}
	return "", true
}

func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return false
	}
	return true
}

func isLoopbackHost(h string) bool {
	switch h {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

// upsertConfigMap creates or patches the aggregated oauth-clients ConfigMap.
// Skipped if unchanged to avoid spurious kubelet projection updates.
func (a *OAuthClientAggregator) upsertConfigMap(ctx context.Context, registryJSON string, count int) error {
	existing := &corev1.ConfigMap{}
	getErr := a.client.Get(ctx, client.ObjectKey{
		Namespace: systemNamespace,
		Name:      aggregatedOAuthClientsConfigMapName,
	}, existing)

	switch {
	case apierrors.IsNotFound(getErr):
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      aggregatedOAuthClientsConfigMapName,
				Namespace: systemNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "orbit-operator",
					"orbit.io/component":           "tenant-oauth-clients",
				},
			},
			Data: map[string]string{aggregatedOAuthClientsConfigMapKey: registryJSON},
		}
		if err := a.client.Create(ctx, cm); err != nil {
			return fmt.Errorf("create %s ConfigMap: %w", aggregatedOAuthClientsConfigMapName, err)
		}
		a.logger.Info(ctx, "created aggregated oauth-clients ConfigMap",
			logging.Field{Key: "configmap", Value: aggregatedOAuthClientsConfigMapName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "clients", Value: count},
		)
		return nil
	case getErr != nil:
		return fmt.Errorf("get %s ConfigMap: %w", aggregatedOAuthClientsConfigMapName, getErr)
	default:
		if existing.Data[aggregatedOAuthClientsConfigMapKey] == registryJSON {
			a.logger.Debug(ctx, "aggregated oauth-clients unchanged, skipping update")
			return nil
		}
		patch := client.MergeFrom(existing.DeepCopy())
		if existing.Data == nil {
			existing.Data = make(map[string]string)
		}
		existing.Data[aggregatedOAuthClientsConfigMapKey] = registryJSON
		if err := a.client.Patch(ctx, existing, patch); err != nil {
			return fmt.Errorf("patch %s ConfigMap: %w", aggregatedOAuthClientsConfigMapName, err)
		}
		a.logger.Info(ctx, "updated aggregated oauth-clients ConfigMap",
			logging.Field{Key: "configmap", Value: aggregatedOAuthClientsConfigMapName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "clients", Value: count},
		)
		return nil
	}
}
