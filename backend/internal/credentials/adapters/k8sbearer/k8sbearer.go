// Package k8sbearer implements the credential adapter for a Kubernetes
// connection whose credential is a static, admin-supplied ServiceAccount
// bearer token. Unlike githubapp, there is no minting: the egress gateway
// calls Mint to read the token from the mounted secret and inject
// it verbatim on the re-originated leg. Rotation is a Secret update, picked
// up on the next resolve via the existing fsnotify reload, with no adapter-side
// cache to invalidate.
//
// Classify is resource-category-scoped rather than one coarse
// read/write pair: it parses the Kubernetes API request path to determine
// which resource category (workloads, config, secrets, networking, storage,
// rbac, or the cluster catch-all) a call touches, combined with a verb class
// derived from the HTTP method. See resourceCategories and categorizeRequest
// below, and
// docs/adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md,
// Amendment 1.
//
// Each connection-type lives in its own subpackage so adding a new external
// service is a new directory behind the same credentials.Adapter
// interface, never a change to the parent package.
package k8sbearer

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/runkora/kora/backend/internal/credentials"
	platformconns "github.com/runkora/kora/backend/internal/platform/connections"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Action constants returned by Classify. Sourced from the shared
// adapter metadata table so the classifier and the published vocabulary
// cannot drift. They combine with the connection name to form
// "connection:<name>:<action>".
const (
	// ActionClusterRead/Write is the catch-all for cluster-scoped/
	// administrative resources not covered by a more specific category below
	// (nodes, namespaces, events, resource quotas), and for API discovery
	// calls (kubectl/client-go always call these before any real request).
	ActionClusterRead     = platformconns.ActionClusterRead
	ActionClusterWrite    = platformconns.ActionClusterWrite
	ActionWorkloadsRead   = platformconns.ActionWorkloadsRead
	ActionWorkloadsWrite  = platformconns.ActionWorkloadsWrite
	ActionConfigRead      = platformconns.ActionConfigRead
	ActionConfigWrite     = platformconns.ActionConfigWrite
	ActionSecretsRead     = platformconns.ActionSecretsRead
	ActionSecretsWrite    = platformconns.ActionSecretsWrite
	ActionNetworkingRead  = platformconns.ActionNetworkingRead
	ActionNetworkingWrite = platformconns.ActionNetworkingWrite
	ActionStorageRead     = platformconns.ActionStorageRead
	ActionStorageWrite    = platformconns.ActionStorageWrite
	ActionRBACRead        = platformconns.ActionRBACRead
	ActionRBACWrite       = platformconns.ActionRBACWrite
)

// categoryCluster is the resource-category name for the catch-all bucket,
// both a literal category returned by categorizeRequest for well-known
// cluster-scoped/admin resources, and the category assigned to non-resource
// API discovery paths.
const categoryCluster = "cluster"

// resourceCategories maps a Kubernetes resource's plural, lowercase REST name
// to the coarse category this adapter authorizes by. Only the adapter needs
// to know this shape. The broker and policy layer treat the resulting
// "<category>:<verb>" action as an opaque string. Unlisted resources
// (including CRDs) are NOT auto-bucketed into "cluster": categorizeRequest
// returns ok=false for them, and Classify denies the call outright
// (ErrUnsupportedAction) rather than guessing. Extend this map, one line per
// resource, as new categories or resources need coverage.
var resourceCategories = map[string]string{
	// workloads
	"pods":                   "workloads",
	"deployments":            "workloads",
	"replicasets":            "workloads",
	"statefulsets":           "workloads",
	"daemonsets":             "workloads",
	"jobs":                   "workloads",
	"cronjobs":               "workloads",
	"replicationcontrollers": "workloads",
	// HorizontalPodAutoscalers size a workload's replica count, so they belong
	// with the workload they scale rather than in a category of their own.
	// VerticalPodAutoscalers are deliberately absent: they are a CRD
	// (autoscaling.k8s.io), and this map covers built-in resources only.
	"horizontalpodautoscalers": "workloads",

	// config
	"configmaps": "config",

	// secrets, split out from config as materially more sensitive so an
	// admin can grant ConfigMap access without also granting Secret access.
	"secrets": "secrets",

	// networking
	"services":        "networking",
	"endpoints":       "networking",
	"endpointslices":  "networking",
	"ingresses":       "networking",
	"ingressclasses":  "networking",
	"networkpolicies": "networking",

	// storage
	"persistentvolumeclaims": "storage",
	"persistentvolumes":      "storage",
	"storageclasses":         "storage",
	"volumeattachments":      "storage",

	// rbac
	"roles":               "rbac",
	"rolebindings":        "rbac",
	"clusterroles":        "rbac",
	"clusterrolebindings": "rbac",
	"serviceaccounts":     "rbac",

	// cluster: well-known cluster-scoped and administrative resources not
	// covered by a more specific category above.
	"namespaces":     categoryCluster,
	"nodes":          categoryCluster,
	"events":         categoryCluster,
	"resourcequotas": categoryCluster,
	"limitranges":    categoryCluster,
}

// Adapter resolves the static ServiceAccount bearer token mounted for a
// Kubernetes connection. Adapter implements credentials.Adapter.
type Adapter struct {
	secrets credentials.SecretResolver
	logger  logging.Logger
}

// New constructs an Adapter backed by the given secret resolver.
func New(secrets credentials.SecretResolver, logger logging.Logger) *Adapter {
	return &Adapter{secrets: secrets, logger: logger}
}

// Type reports the adapter type served.
func (a *Adapter) Type() credentials.AdapterType { return credentials.AdapterKubernetesBearer }

// Actions enumerates every action Classify can return, delegating to
// the shared metadata table (internal/platform/connections) so the
// connection:<name>:<action> vocabulary the egress gateway publishes has
// exactly one source of truth.
func (a *Adapter) Actions() []credentials.ActionDeclaration {
	return credentials.DeclaredActions(platformconns.AdapterKubernetesBearer)
}

// Open binds the adapter to one connection. A cluster connection carries no
// adapter configuration: its certificate authority is a connection field the
// gateway reads, not an adapter key.
func (a *Adapter) Open(conn credentials.Connection) (credentials.Connector, error) {
	return &connector{adapter: a, conn: conn}, nil
}

// connector serves one cluster.
type connector struct {
	adapter *Adapter
	conn    credentials.Connection
}

// Classify maps a Kubernetes API request to the action that authorizes it: a
// resource category (workloads, config, secrets, networking, storage, rbac, or
// the cluster catch-all) combined with a read or write verb from the method.
//
// Two shapes are denied outright rather than misclassified. kubectl exec and
// port-forward are SPDY or WebSocket upgrades, not REST. A resource absent from
// resourceCategories, a CRD or anything not yet enumerated, is denied rather
// than folded into the catch-all.
//
// Authorization within a granted category, by namespace, resource name or
// verb, is the target cluster's own RBAC on the injected identity.
func (c *connector) Classify(req *http.Request) (credentials.Classification, error) {
	if req == nil || req.URL == nil {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
	if isUpgradeRequest(req) {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}

	// Classify the resolved path. The API server resolves dot segments, so
	// classifying the raw path would let "/api/v1/.../pods/../../../secrets"
	// be authorized as workloads while the upstream serves it as secrets.
	category, ok := categorizeRequest(path.Clean(req.URL.Path))
	if !ok {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}

	// The namespace qualifier instance is deferred to an amendment of the
	// Kubernetes connection ADR. Until then no qualifier is extracted, per
	// docs/adr/2026-07-22-connection-scope-qualifiers.md (Instances).
	switch strings.ToUpper(req.Method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return credentials.Classification{Action: category + ":read"}, nil
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return credentials.Classification{Action: category + ":write"}, nil
	default:
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
}

// categorizeRequest maps a Kubernetes API server request path to a resource
// category. It recognizes the two REST prefixes the API server serves:
//
//	/api/v1/...                     the core (unnamed) API group
//	/apis/{group}/{version}/...     every named API group
//
// and, within either, both namespaced (".../namespaces/{ns}/{resource}/...")
// and cluster-scoped (".../{resource}/...") resource shapes. Anything that
// isn't a resource path at all (bare /api, /apis, /apis/{group}, /version,
// /openapi/*, /healthz/livez/readyz, or the root) is API discovery, which
// kubectl/client-go always call before any real request; it is bucketed into
// the "cluster" category rather than denied (mirroring upstream Kubernetes'
// own RBAC, which binds discovery to every authenticated user regardless of
// role). Returns ok=false when the path names a resource type this adapter
// does not recognize.
func categorizeRequest(path string) (category string, ok bool) {
	segments := pathSegments(path)

	var rest []string
	switch {
	case len(segments) >= 2 && segments[0] == "api" && segments[1] == "v1":
		rest = segments[2:]
	case len(segments) >= 1 && segments[0] == "apis":
		if len(segments) < 3 {
			return categoryCluster, true // bare /apis or /apis/{group}: discovery
		}
		rest = segments[3:]
	default:
		return categoryCluster, true // /api, /version, /openapi/*, /healthz, root, ...
	}

	if len(rest) == 0 {
		return categoryCluster, true // bare group/version root: discovery
	}
	if rest[0] == "watch" { // deprecated /api/v1/watch/... form
		rest = rest[1:]
		if len(rest) == 0 {
			return categoryCluster, true
		}
	}

	resource := rest[0]
	if resource == "namespaces" {
		if len(rest) <= 2 {
			// ["namespaces"] or ["namespaces", name]: the Namespace object
			// itself, not a namespaced resource beneath it.
			resource = "namespaces"
		} else {
			// ["namespaces", ns, resource, ...]: the actual resource is the
			// third segment; "namespaces/{ns}/" is just the scoping prefix.
			resource = rest[2]
		}
	}

	cat, known := resourceCategories[resource]
	return cat, known
}

// pathSegments splits a URL path into non-empty segments.
func pathSegments(path string) []string {
	var out []string
	for s := range strings.SplitSeq(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isUpgradeRequest reports whether req is asking the server to switch
// protocols (the shape kubectl exec/attach/port-forward use to establish a
// bidirectional SPDY or WebSocket stream), which this adapter does not
// support brokering.
func isUpgradeRequest(req *http.Request) bool {
	conn := req.Header.Get("Connection")
	if conn == "" || req.Header.Get("Upgrade") == "" {
		return false
	}
	for part := range strings.SplitSeq(conn, ",") {
		if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
			return true
		}
	}
	return false
}

// Resolve reads the static bearer token from the mounted secret and returns
// it as-is. There is no minting and no cache, since the token is already an
// in-memory string once read, so caching it buys nothing k8sfile's own
// mount-read doesn't already give for free.
func (c *connector) Mint(ctx context.Context, id credentials.CallerIdentity) (credentials.Credential, error) {
	tok, err := c.adapter.secrets.Resolve(ctx, c.conn.SecretRef)
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("resolve k8s bearer secret: %w", err)
	}

	c.adapter.logger.Debug(ctx, "resolved k8s bearer credential",
		logging.Field{Key: "connection", Value: c.conn.Name},
		logging.Field{Key: "tenant_id", Value: id.TenantID},
		logging.Field{Key: "run_id", Value: id.RunID},
	)

	return credentials.Credential{
		Token:  string(tok),
		Header: "Authorization",
		Value:  "Bearer " + string(tok),
		// ExpiresAt left zero: the token is static; rotation is a Secret
		// update picked up on the gateway's next resolve.
	}, nil
}
