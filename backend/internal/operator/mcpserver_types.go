// MCPServer CRD Go types for MCP server registration (Phase 3 — MCP Tool Gateway).
//
// An MCPServer declares one MCP server kora governs. There are two types,
// distinguished only by where the CR is applied:
//
//   - Global: applied in kora-system (e.g. the platform kora MCP). tenant_id
//     is "" and its tools' scopes appear in every tenant's catalog. Only a
//     global server may set callerIdentity: forward (act AS the caller against
//     first-party kora APIs).
//   - Tenant: applied in a tenant namespace (a tenant's own MCP server). tenant_id
//     is the owning tenant. callerIdentity: forward is REJECTED — otherwise a
//     tenant admin (who holds CR-apply RBAC in their namespace) could harvest
//     every caller's full-power platform token via a pod they control.
//
// The operator's MCPServerAggregator reads these CRs and renders the
// kora-system "kora-mcp-servers" ConfigMap the tool-gateway mounts. The
// gateway, a feature package, never imports this CRD type; it reads the mounted
// ConfigMap instead. Introspection health is gateway runtime state — NOT CR
// status (which reflects only operator reconciliation).
//
// See docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCP server lifecycle phases (operator reconciliation state).
const (
	// MCPServerPhasePending is the initial/unreconciled state.
	MCPServerPhasePending = "Pending"
	// MCPServerPhaseAggregated means the CR was accepted and folded into the
	// kora-mcp-servers registry ConfigMap.
	MCPServerPhaseAggregated = "Aggregated"
	// MCPServerPhaseInvalid means the spec was rejected (bad spec, or a
	// tenant-namespaced CR setting callerIdentity: forward). The server is NOT
	// in the registry.
	MCPServerPhaseInvalid = "Invalid"
)

// callerIdentity modes — whether the gateway conveys the CALLER's identity to
// the upstream on a tool call.
const (
	// CallerIdentityNone (default): the caller's token is NEVER forwarded; the
	// server acts as itself with its own backend credential.
	CallerIdentityNone = "none"
	// CallerIdentityForward: the gateway forwards the caller's bearer so the
	// server acts AS the caller against first-party kora APIs. Valid ONLY on
	// global (kora-system) servers.
	CallerIdentityForward = "forward"
)

// transportStreamableHTTP is the only MCP transport supported in the MVP.
const transportStreamableHTTP = "streamable-http"

// -----------------------------------------------------------------------------
// MCPServer — namespaced; kora-system for global, tenant namespace for tenant
// -----------------------------------------------------------------------------

// MCPServer declares one MCP server kora governs. Apply it in kora-system for a
// platform-global server or in a tenant namespace for a tenant's own server.
// The operator aggregates every MCPServer into the kora-mcp-servers ConfigMap
// the tool-gateway mounts. The gateway introspects each server's tools and
// registers them as mcp:<server>:<tool>:use scopes. The CR name
// (metadata.name) is the stable server slug used in the scope path.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kmcp
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.spec.endpoint`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:object:generate=true
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec   MCPServerSpec   `json:"spec,omitempty"`
	Status MCPServerStatus `json:"status,omitempty"`
}

// MCPServerSpec is the declared intent for one MCP server.
//
// +kubebuilder:object:generate=true
type MCPServerSpec struct {
	// DisplayName for the catalog UI.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Description is a one-line summary of what the server does, shown in the
	// catalog UI.
	// +optional
	Description string `json:"description,omitempty"`

	// Endpoint is the URL of the MCP server (Streamable HTTP).
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// Transport selects the MCP transport. MVP: "streamable-http" (the default
	// when empty).
	// +kubebuilder:validation:Enum=streamable-http
	// +optional
	Transport string `json:"transport,omitempty"`

	// ReintrospectIntervalSeconds is read by the gateway's introspection worker
	// to re-sync the tool surface on a timer (besides on registry reload).
	// 0 = on-reload only.
	// +kubebuilder:validation:Minimum=0
	// +optional
	ReintrospectIntervalSeconds int `json:"reintrospectIntervalSeconds,omitempty"`

	// CallerIdentity controls whether the gateway conveys the CALLER's identity
	// to this upstream. Default "none". "forward" is ENFORCED GLOBAL-ONLY: a
	// tenant-namespaced CR that sets it is rejected at reconcile (Invalid).
	// +kubebuilder:validation:Enum=none;forward
	// +optional
	CallerIdentity string `json:"callerIdentity,omitempty"`

	// Workload names the pods in this CR's namespace that serve Endpoint. It
	// classifies them as MCP workloads: the pod webhook skips the identity-proxy
	// and OPA injection an agent pod requires, and the operator generates the
	// policies that let the tool-gateway reach them.
	//
	// Omit it for a global server or one reached at an external URL; neither has
	// pods in this namespace to classify.
	// See docs/adr/2026-09-17-mcp-workload-class.md.
	// +optional
	Workload *MCPServerWorkload `json:"workload,omitempty"`

	// Auth declares static credentials the tool-gateway sends to the server.
	// See docs/adr/2026-10-22-mcp-server-static-credentials.md.
	// +optional
	Auth *MCPServerAuth `json:"auth,omitempty"`
}

// MCPServerAuth lists the credential headers sent on every upstream request.
//
// +kubebuilder:object:generate=true
type MCPServerAuth struct {
	// Headers are the request headers, each set to the prefix followed by a Secret value.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Headers []MCPServerAuthHeader `json:"headers"`
}

// MCPServerAuthHeader is one header set to Prefix followed by a Secret value.
//
// +kubebuilder:object:generate=true
type MCPServerAuthHeader struct {
	// Name is the header name, e.g. Authorization or x-api-key. Letters, digits and hyphens only.
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	Name string `json:"name"`
	// Prefix is sent before the value, e.g. "Bearer ".
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Prefix string `json:"prefix,omitempty"`
	// SecretRef is the Secret and key holding the value, in the CR's namespace.
	SecretRef MCPServerSecretRef `json:"secretRef"`
}

// MCPServerSecretRef names a key in a Secret in the CR's namespace.
//
// +kubebuilder:object:generate=true
type MCPServerSecretRef struct {
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	Key string `json:"key"`
}

// MCPServerWorkload identifies the pods serving an MCPServer's endpoint.
//
// +kubebuilder:object:generate=true
type MCPServerWorkload struct {
	// Selector matches the serving pods by label, in the CR's own namespace.
	// +optional
	Selector map[string]string `json:"selector,omitempty"`
}

// MCPServerStatus is reconciliation state owned by the operator — whether the
// CR was accepted and aggregated into the registry. Introspection health
// (reachability, tool count, last sync) is gateway runtime state, surfaced via
// metrics + the catalog UI, NOT here.
//
// +kubebuilder:object:generate=true
type MCPServerStatus struct {
	// Phase is one of Pending|Aggregated|Invalid.
	// +optional
	Phase string `json:"phase,omitempty"`
	// LastReconciledAt is when the operator last reconciled this CR.
	// +optional
	LastReconciledAt *metav1.Time `json:"lastReconciledAt,omitempty"`
	// Conditions carries the Aggregated condition.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// MCPServerList is a list of MCPServer resources.
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPServer `json:"items"`
}
