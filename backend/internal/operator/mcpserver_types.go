// MCPServer CRD Go types for MCP server registration (Phase 3 — MCP Tool Gateway).
//
// An MCPServer declares one MCP server Orbit governs. There are two types,
// distinguished only by where the CR is applied:
//
//   - Global: applied in orbit-system (e.g. the platform Orbit MCP). tenant_id
//     is "" and its tools' scopes appear in every tenant's catalog. Only a
//     global server may set callerIdentity: forward (act AS the caller against
//     first-party Orbit APIs).
//   - Tenant: applied in a tenant namespace (a tenant's own MCP server). tenant_id
//     is the owning tenant. callerIdentity: forward is REJECTED — otherwise a
//     tenant admin (who holds CR-apply RBAC in their namespace) could harvest
//     every caller's full-power platform token via a pod they control.
//
// The operator's MCPServerAggregator reads these CRs and renders the
// orbit-system "orbit-mcp-servers" ConfigMap the tool-gateway mounts. The
// gateway, a feature package, never imports this CRD type; it reads the mounted
// ConfigMap instead. Introspection health is gateway runtime state — NOT CR
// status (which reflects only operator reconciliation).
//
// See docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// MCP server lifecycle phases (operator reconciliation state).
const (
	// MCPServerPhasePending is the initial/unreconciled state.
	MCPServerPhasePending = "Pending"
	// MCPServerPhaseAggregated means the CR was accepted and folded into the
	// orbit-mcp-servers registry ConfigMap.
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
	// server acts AS the caller against first-party Orbit APIs. Valid ONLY on
	// global (orbit-system) servers.
	CallerIdentityForward = "forward"
)

// transportStreamableHTTP is the only MCP transport supported in the MVP.
const transportStreamableHTTP = "streamable-http"

// -----------------------------------------------------------------------------
// MCPServer — namespaced; orbit-system for global, tenant namespace for tenant
// -----------------------------------------------------------------------------

// MCPServer declares one MCP server Orbit governs.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=omcp
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.spec.endpoint`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPServerSpec   `json:"spec,omitempty"`
	Status MCPServerStatus `json:"status,omitempty"`
}

// MCPServerSpec is the declared intent for one MCP server.
type MCPServerSpec struct {
	// DisplayName for the catalog UI.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Endpoint is the in-mesh URL of the MCP server (Streamable HTTP).
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// Transport selects the MCP transport. MVP: "streamable-http" (the default
	// when empty).
	// +optional
	Transport string `json:"transport,omitempty"`

	// ReintrospectIntervalSeconds is read by the gateway's introspection worker
	// to re-sync the tool surface on a timer (besides on registry reload).
	// 0 = on-reload only.
	// +optional
	ReintrospectIntervalSeconds int `json:"reintrospectIntervalSeconds,omitempty"`

	// CallerIdentity controls whether the gateway conveys the CALLER's identity
	// to this upstream. Default "none". "forward" is ENFORCED GLOBAL-ONLY: a
	// tenant-namespaced CR that sets it is rejected at reconcile (Invalid).
	// +kubebuilder:validation:Enum=none;forward
	// +optional
	CallerIdentity string `json:"callerIdentity,omitempty"`
}

// MCPServerStatus is reconciliation state owned by the operator — whether the
// CR was accepted and aggregated into the registry. Introspection health
// (reachability, tool count, last sync) is gateway runtime state, surfaced via
// metrics + the catalog UI, NOT here.
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
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPServer `json:"items"`
}

// -----------------------------------------------------------------------------
// DeepCopy (hand-written, mirroring Connection/OrbitTenant)
// -----------------------------------------------------------------------------

// DeepCopyObject implements runtime.Object.
func (m *MCPServer) DeepCopyObject() runtime.Object {
	if m == nil {
		return nil
	}
	out := new(MCPServer)
	m.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (m *MCPServer) DeepCopyInto(out *MCPServer) {
	*out = *m
	out.TypeMeta = m.TypeMeta
	m.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = m.Spec
	m.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (m *MCPServer) DeepCopy() *MCPServer {
	if m == nil {
		return nil
	}
	out := new(MCPServer)
	m.DeepCopyInto(out)
	return out
}

// DeepCopyInto for MCPServerStatus — the pointer time and condition slice need
// deep copies.
func (s *MCPServerStatus) DeepCopyInto(out *MCPServerStatus) {
	*out = *s
	if s.LastReconciledAt != nil {
		out.LastReconciledAt = s.LastReconciledAt.DeepCopy()
	}
	if s.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(s.Conditions))
		for i := range s.Conditions {
			s.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

// DeepCopyObject implements runtime.Object for MCPServerList.
func (l *MCPServerList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(MCPServerList)
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *MCPServerList) DeepCopyInto(out *MCPServerList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]MCPServer, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
