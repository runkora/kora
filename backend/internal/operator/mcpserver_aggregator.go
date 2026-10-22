// MCP server aggregator (Phase 3 — MCP Tool Gateway).
//
// The tool-gateway runs in orbit-system but needs every MCP server's endpoint —
// the platform-global ones (orbit-system) and each tenant's own (tenant
// namespaces). Pods can only mount ConfigMaps from their own namespace, so the
// operator aggregates every MCPServer CR across all namespaces into one
// orbit-system ConfigMap the gateway mounts:
//
//   - ConfigMap orbit-mcp-servers — one data key, servers.json, a JSON array of
//     registry entries (tenant_id, name, endpoint, transport,
//     reintrospect_interval_seconds, caller_identity, auth_headers). Non-secret
//     metadata only.
//   - Secret orbit-mcp-server-secrets — the values auth_headers refer to,
//     copied from Secrets in each MCPServer's namespace (mcpserver_auth.go).
//   - ServiceEntries mcp-server-<host> — one per external endpoint host
//     (mcpserver_serviceentry.go).
//
// This mirrors connection_aggregator.go: the egress gateway already receives its
// per-tenant connection registry this way, so MCP servers take the same path
// rather than inventing a second one. The aggregator is purely Kubernetes-facing
// — it makes no MCP-protocol calls and no policy-service calls. Introspection
// and scope publishing are the gateway's job, not the operator's.
//
// See docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	// aggregatedMCPServersConfigMapName is the ConfigMap in orbit-system that
	// collects every active MCP server's registry metadata. Its single data key
	// is servers.json — the JSON array the tool-gateway's registry reader parses.
	aggregatedMCPServersConfigMapName = "orbit-mcp-servers"

	// aggregatedMCPServersConfigMapKey is the data key within the ConfigMap.
	aggregatedMCPServersConfigMapKey = "servers.json"
)

// Spec validation sentinels — surfaced in MCPServerStatus and used by the
// aggregator to exclude invalid servers from the registry.
var (
	// ErrMCPServerNoEndpoint is returned when spec.endpoint is empty.
	ErrMCPServerNoEndpoint = errors.New("spec.endpoint is required")
	// ErrMCPServerBadEndpoint is returned for an endpoint that does not parse
	// or carries userinfo or a query string. Credentials in the URL would reach
	// logs and errors. The message never repeats the endpoint.
	ErrMCPServerBadEndpoint = errors.New("spec.endpoint must be a URL with no userinfo and no query string")
	// ErrMCPServerBadTransport is returned for an unsupported transport.
	ErrMCPServerBadTransport = errors.New("unsupported transport (only streamable-http)")
	// ErrMCPServerForwardNotGlobal is returned when a tenant-namespaced CR sets
	// callerIdentity: forward — forward is global-only.
	ErrMCPServerForwardNotGlobal = errors.New("callerIdentity: forward is valid only on global (orbit-system) servers")
	// ErrMCPServerNoWorkloadSelector is returned when spec.workload is present
	// with no selector, which would classify every pod in the namespace.
	ErrMCPServerNoWorkloadSelector = errors.New("spec.workload.selector is required when spec.workload is set")
	// ErrMCPServerNameTooLong is returned for a name that cannot be a label
	// value. The generated policies carry the name in a label, so a longer one
	// would be rejected by the API server when the policy is written.
	ErrMCPServerNameTooLong = fmt.Errorf("metadata.name must be at most %d characters", maxLabelValueLength)
)

// maxLabelValueLength is the Kubernetes limit on a label value, which is
// shorter than the limit on an object name.
const maxLabelValueLength = 63

// mcpServerRegistryEntry is one element of servers.json — the runtime contract
// the tool-gateway's introspection worker agrees on. tenant_id is "" for global
// (orbit-system) servers and the owning OrbitTenant name for tenant servers.
type mcpServerRegistryEntry struct {
	TenantID                    string `json:"tenant_id"`
	Name                        string `json:"name"`
	DisplayName                 string `json:"display_name,omitempty"`
	Description                 string `json:"description,omitempty"`
	Endpoint                    string `json:"endpoint"`
	Transport                   string `json:"transport"`
	ReintrospectIntervalSeconds int    `json:"reintrospect_interval_seconds"`
	CallerIdentity              string `json:"caller_identity"`
	// AuthHeaders reference values in the orbit-mcp-server-secrets Secret.
	AuthHeaders []mcpAuthHeaderEntry `json:"auth_headers,omitempty"`

	namespace string
	auth      *MCPServerAuth
}

// mcpAuthHeaderEntry is one credential header in servers.json. It never holds
// the value.
type mcpAuthHeaderEntry struct {
	Name      string `json:"name"`
	Prefix    string `json:"prefix,omitempty"`
	SecretKey string `json:"secret_key"`
}

// MCPServerAggregator reads every MCPServer CR across all namespaces and renders
// the aggregated orbit-mcp-servers ConfigMap in orbit-system. Re-run on every
// MCPServer reconcile so a newly-applied server appears in the gateway's mount
// within a kubelet projection-update cycle.
type MCPServerAggregator struct {
	client client.Client
	logger logging.Logger
}

// NewMCPServerAggregator creates an MCPServerAggregator backed by the provided
// client.
func NewMCPServerAggregator(c client.Client, logger logging.Logger) *MCPServerAggregator {
	return &MCPServerAggregator{client: c, logger: logger}
}

// Sync collects every valid MCP server's registry entry and upserts the
// aggregated ConfigMap. Servers with an invalid spec, or in a namespace with no
// active owning tenant, are skipped — logged and continued, never failing the
// whole batch — so one misconfigured server cannot stall every other server's
// registration.
func (a *MCPServerAggregator) Sync(ctx context.Context) error {
	nsToTenant, err := a.activeTenantNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("map tenant namespaces: %w", err)
	}

	entries, err := a.collect(ctx, nsToTenant)
	if err != nil {
		return fmt.Errorf("collect MCP servers: %w", err)
	}

	credentials, err := a.collectCredentials(ctx, entries)
	if err != nil {
		return fmt.Errorf("collect MCP server credentials: %w", err)
	}
	// The Secret goes first, so the registry rarely names a key the gateway has
	// not been given. A failed Secret write still lets the registry update.
	secretErr := a.upsertMCPServerSecret(ctx, credentials)
	if err := a.upsertConfigMap(ctx, entries); err != nil {
		return err
	}
	if err := a.syncServiceEntries(ctx, entries); err != nil {
		return err
	}
	return secretErr
}

// collectCredentials reads every registered server's credential values. An
// unavailable value is left out and logged. The server stays registered and
// the gateway fails its calls closed.
func (a *MCPServerAggregator) collectCredentials(ctx context.Context, entries []mcpServerRegistryEntry) (map[string][]byte, error) {
	data := make(map[string][]byte)
	for _, e := range entries {
		if e.auth == nil {
			continue
		}
		for i, h := range e.auth.Headers {
			v, err := readMCPCredential(ctx, a.client, e.namespace, h.SecretRef)
			if errors.Is(err, ErrMCPCredentialUnavailable) {
				a.logger.Warn(ctx, "MCPServer credential unavailable",
					logging.Field{Key: "mcpserver", Value: e.Name},
					logging.Field{Key: "namespace", Value: e.namespace},
					logging.Field{Key: "reason", Value: err.Error()},
				)
				continue
			}
			if err != nil {
				return nil, err
			}
			data[e.AuthHeaders[i].SecretKey] = v
		}
	}
	return data, nil
}

// activeTenantNamespaces builds a namespace -> tenant_id map for active tenants
// only. Suspended tenants are excluded so the gateway will not introspect their
// servers — matching the connection aggregator's behavior. The orbit-system
// namespace is handled separately (global, tenant_id "").
func (a *MCPServerAggregator) activeTenantNamespaces(ctx context.Context) (map[string]string, error) {
	list := &KoraTenantList{}
	if err := a.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list OrbitTenants: %w", err)
	}
	out := make(map[string]string, len(list.Items))
	for i := range list.Items {
		t := &list.Items[i]
		if t.Spec.Active {
			out[tenantNamespace(t)] = t.Name
		}
	}
	return out, nil
}

// collect lists every MCPServer CR cluster-wide and delegates to the pure
// buildMCPRegistry, logging the servers it excluded.
func (a *MCPServerAggregator) collect(ctx context.Context, nsToTenant map[string]string) ([]mcpServerRegistryEntry, error) {
	list := &MCPServerList{}
	if err := a.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list MCPServers: %w", err)
	}

	entries, skipped := buildMCPRegistry(list.Items, nsToTenant)
	for _, s := range skipped {
		a.logger.Warn(ctx, "MCPServer excluded from registry",
			logging.Field{Key: "mcpserver", Value: s.Name},
			logging.Field{Key: "namespace", Value: s.Namespace},
			logging.Field{Key: "reason", Value: s.Reason},
		)
	}
	return entries, nil
}

// mcpServerSkip records an MCPServer left out of the registry and why.
type mcpServerSkip struct {
	Name      string
	Namespace string
	Reason    string
}

// buildMCPRegistry is the pure core of aggregation: given every MCPServer CR and
// the active-tenant namespace map, it resolves each server's tenant_id (global
// "" for orbit-system, the owning tenant otherwise), validates the spec, and
// returns the sorted registry entries plus the excluded servers (with reasons).
// A server is excluded when its namespace has no active owning tenant
// (orphaned/suspended) or its spec is invalid.
func buildMCPRegistry(servers []MCPServer, nsToTenant map[string]string) ([]mcpServerRegistryEntry, []mcpServerSkip) {
	var entries []mcpServerRegistryEntry
	var skipped []mcpServerSkip

	for i := range servers {
		srv := &servers[i]

		tenantID, ok := resolveTenantID(srv.Namespace, nsToTenant)
		if !ok {
			skipped = append(skipped, mcpServerSkip{srv.Name, srv.Namespace, "no active tenant for namespace"})
			continue
		}

		if err := validateMCPServerSpec(srv.Spec, srv.Name, srv.Namespace); err != nil {
			skipped = append(skipped, mcpServerSkip{srv.Name, srv.Namespace, err.Error()})
			continue
		}

		entries = append(entries, mcpServerRegistryEntry{
			TenantID:                    tenantID,
			Name:                        srv.Name,
			DisplayName:                 srv.Spec.DisplayName,
			Description:                 srv.Spec.Description,
			Endpoint:                    srv.Spec.Endpoint,
			Transport:                   effectiveTransport(srv.Spec.Transport),
			ReintrospectIntervalSeconds: srv.Spec.ReintrospectIntervalSeconds,
			CallerIdentity:              effectiveCallerIdentity(srv.Spec.CallerIdentity),
			AuthHeaders:                 authHeaderEntries(srv),
			namespace:                   srv.Namespace,
			auth:                        srv.Spec.Auth,
		})
	}

	// Stable order so the rendered ConfigMap doesn't churn on every reconcile
	// purely from list iteration order.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].TenantID != entries[j].TenantID {
			return entries[i].TenantID < entries[j].TenantID
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, skipped
}

// authHeaderEntries lists srv's credential headers for servers.json.
func authHeaderEntries(srv *MCPServer) []mcpAuthHeaderEntry {
	if srv.Spec.Auth == nil {
		return nil
	}
	out := make([]mcpAuthHeaderEntry, 0, len(srv.Spec.Auth.Headers))
	for _, h := range srv.Spec.Auth.Headers {
		out = append(out, mcpAuthHeaderEntry{Name: h.Name, Prefix: h.Prefix, SecretKey: mcpSecretKey(srv.Namespace, srv.Name, h.Name)})
	}
	return out
}

// resolveTenantID returns the tenant_id for an MCPServer namespace: "" for the
// global orbit-system namespace, the owning tenant's name for a tenant
// namespace. Returns (_, false) for a namespace with no active owning tenant.
func resolveTenantID(namespace string, nsToTenant map[string]string) (string, bool) {
	if namespace == systemNamespace {
		return "", true
	}
	tenantID, ok := nsToTenant[namespace]
	return tenantID, ok
}

// validateMCPServerSpec checks an MCPServer spec against the name and namespace
// it was applied under. It is the single source of truth for both the per-CR
// Status the reconciler sets and the aggregator's include/exclude decision.
func validateMCPServerSpec(spec MCPServerSpec, name, namespace string) error {
	if len(name) > maxLabelValueLength {
		return ErrMCPServerNameTooLong
	}
	if spec.Endpoint == "" {
		return ErrMCPServerNoEndpoint
	}
	if u, err := url.Parse(spec.Endpoint); err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery {
		return ErrMCPServerBadEndpoint
	}
	if spec.Transport != "" && spec.Transport != transportStreamableHTTP {
		return fmt.Errorf("%w: %q", ErrMCPServerBadTransport, spec.Transport)
	}
	// forward is global-only: a tenant admin holds CR-apply RBAC in their own
	// namespace, so a tenant CR setting forward could harvest every caller's
	// full-power token via a pod they control. Reject it structurally.
	if spec.CallerIdentity == CallerIdentityForward && namespace != systemNamespace {
		return ErrMCPServerForwardNotGlobal
	}
	// A workload block with no selector would classify every pod in the
	// namespace, stripping enforcement from the agents there.
	if spec.Workload != nil && len(spec.Workload.Selector) == 0 {
		return ErrMCPServerNoWorkloadSelector
	}
	return validateMCPServerAuth(spec)
}

// effectiveTransport defaults an empty transport to streamable-http.
func effectiveTransport(t string) string {
	if t == "" {
		return transportStreamableHTTP
	}
	return t
}

// effectiveCallerIdentity defaults an empty caller identity to "none".
func effectiveCallerIdentity(c string) string {
	if c == "" {
		return CallerIdentityNone
	}
	return c
}

// upsertConfigMap creates or patches the aggregated MCP servers ConfigMap.
// Skipped if unchanged to avoid spurious kubelet projection updates.
func (a *MCPServerAggregator) upsertConfigMap(ctx context.Context, entries []mcpServerRegistryEntry) error {
	// Normalize nil to an empty slice so an empty registry renders "[]" rather
	// than "null" — the gateway's reader always parses a JSON array.
	if entries == nil {
		entries = []mcpServerRegistryEntry{}
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal MCP server registry: %w", err)
	}
	registryJSON := string(raw)

	existing := &corev1.ConfigMap{}
	getErr := a.client.Get(ctx, client.ObjectKey{
		Namespace: systemNamespace,
		Name:      aggregatedMCPServersConfigMapName,
	}, existing)

	switch {
	case apierrors.IsNotFound(getErr):
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      aggregatedMCPServersConfigMapName,
				Namespace: systemNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "orbit-operator",
					"orbit.io/component":           "mcp-servers",
				},
			},
			Data: map[string]string{aggregatedMCPServersConfigMapKey: registryJSON},
		}
		if err := a.client.Create(ctx, cm); err != nil {
			return fmt.Errorf("create %s ConfigMap: %w", aggregatedMCPServersConfigMapName, err)
		}
		a.logger.Info(ctx, "created aggregated MCP servers ConfigMap",
			logging.Field{Key: "configmap", Value: aggregatedMCPServersConfigMapName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "servers", Value: len(entries)},
		)
		return nil
	case getErr != nil:
		return fmt.Errorf("get %s ConfigMap: %w", aggregatedMCPServersConfigMapName, getErr)
	default:
		if existing.Data[aggregatedMCPServersConfigMapKey] == registryJSON {
			a.logger.Debug(ctx, "aggregated MCP servers unchanged, skipping update")
			return nil
		}
		// Two controllers share this aggregator; the lock turns a stale
		// writer into a conflict and requeue instead of a silent overwrite.
		patch := client.MergeFromWithOptions(existing.DeepCopy(), client.MergeFromWithOptimisticLock{})
		if existing.Data == nil {
			existing.Data = make(map[string]string)
		}
		existing.Data[aggregatedMCPServersConfigMapKey] = registryJSON
		if err := a.client.Patch(ctx, existing, patch); err != nil {
			return fmt.Errorf("patch %s ConfigMap: %w", aggregatedMCPServersConfigMapName, err)
		}
		a.logger.Info(ctx, "updated aggregated MCP servers ConfigMap",
			logging.Field{Key: "configmap", Value: aggregatedMCPServersConfigMapName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "servers", Value: len(entries)},
		)
		return nil
	}
}
