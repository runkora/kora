// Package operator reconciles tenant resources and enforces tenant workload prerequisites.
package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the API group and version for kora CRDs.
var GroupVersion = schema.GroupVersion{Group: "runkora.io", Version: "v1alpha1"}

// -----------------------------------------------------------------------------
// KoraTenant: cluster-scoped, one per tenant
// -----------------------------------------------------------------------------

// KoraTenant declares a tenant's existence, OIDC configuration, and namespace.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type KoraTenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KoraTenantSpec   `json:"spec,omitempty"`
	Status KoraTenantStatus `json:"status,omitempty"`
}

// KoraTenantSpec defines the desired state of a tenant.
type KoraTenantSpec struct {
	// DisplayName is the human-readable tenant name surfaced in the identity
	// service tenant registry.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`

	// Active controls whether the tenant is active. When false, the operator
	// sets state:suspended in the registry without deleting cluster resources.
	// +kubebuilder:default=true
	Active bool `json:"active"`

	// Namespace selects the tenant workload namespace, defaulting to
	// orbit-tenant-<name>. The operator labels it but never deletes it.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// OIDC configures the identity provider for this tenant.
	OIDC TenantOIDCSpec `json:"oidc"`
}

// SecretRef is a reference to a Kubernetes Secret.
type SecretRef struct {
	// Name is the Secret name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace defaults to the system namespace if omitted.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// TenantOIDCSpec defines OIDC configuration for a tenant.
type TenantOIDCSpec struct {
	// IssuerURL is the OIDC issuer URL.
	// +kubebuilder:validation:Pattern=`^https?://`
	IssuerURL string `json:"issuerURL"`

	// ClientID is the OIDC client ID registered with the identity provider.
	// +kubebuilder:validation:MinLength=1
	ClientID string `json:"clientID"`

	// ClientSecretRef references the OIDC client-secret Secret. Omit it for public clients.
	// +optional
	ClientSecretRef *SecretRef `json:"clientSecretRef,omitempty"`

	// Scopes are the OAuth2 scopes to request. Defaults to [openid, profile, email].
	// +kubebuilder:default={openid,profile,email}
	Scopes []string `json:"scopes,omitempty"`

	// EmailDomains lists the email address domains routed to this tenant by the
	// Home Realm Discovery endpoint (e.g. ["acme.com", "acme.io"]).
	// See docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md
	// +optional
	EmailDomains []string `json:"emailDomains,omitempty"`
}

// KoraTenantStatus records observed tenant state.
type KoraTenantStatus struct {
	// Phase summarizes tenant lifecycle state.
	// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Degraded;Suspended
	Phase string `json:"phase,omitempty"`

	// Namespace is the provisioned tenant namespace name.
	Namespace string `json:"namespace,omitempty"`

	// LastReconciledAt is the timestamp of the last successful reconciliation.
	LastReconciledAt *metav1.Time `json:"lastReconciledAt,omitempty"`

	// Conditions lists detailed status conditions.
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Tenant status condition types.
const (
	ConditionNamespaceReady         = "NamespaceReady"
	ConditionRegistryUpdated        = "RegistryUpdated"
	ConditionPrerequisitesSatisfied = "PrerequisitesSatisfied"
)

// Tenant phase values.
const (
	TenantPhasePending      = "Pending"
	TenantPhaseProvisioning = "Provisioning"
	TenantPhaseReady        = "Ready"
	TenantPhaseDegraded     = "Degraded"
	TenantPhaseSuspended    = "Suspended"
)

// KoraTenantList is a list of KoraTenant resources.
// +kubebuilder:object:root=true
type KoraTenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KoraTenant `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (t *KoraTenant) DeepCopyObject() runtime.Object {
	if t == nil {
		return nil
	}
	out := new(KoraTenant)
	t.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (t *KoraTenant) DeepCopyInto(out *KoraTenant) {
	*out = *t
	out.TypeMeta = t.TypeMeta
	t.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	t.Spec.DeepCopyInto(&out.Spec)
	t.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (t *KoraTenant) DeepCopy() *KoraTenant {
	if t == nil {
		return nil
	}
	out := new(KoraTenant)
	t.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object for KoraTenantList.
func (l *KoraTenantList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(KoraTenantList)
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *KoraTenantList) DeepCopyInto(out *KoraTenantList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]KoraTenant, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopyInto for KoraTenantSpec.
func (s *KoraTenantSpec) DeepCopyInto(out *KoraTenantSpec) {
	*out = *s
	out.OIDC = s.OIDC
	if s.OIDC.ClientSecretRef != nil {
		ref := *s.OIDC.ClientSecretRef
		out.OIDC.ClientSecretRef = &ref
	}
	if s.OIDC.Scopes != nil {
		out.OIDC.Scopes = make([]string, len(s.OIDC.Scopes))
		copy(out.OIDC.Scopes, s.OIDC.Scopes)
	}
}

// DeepCopyInto for KoraTenantStatus.
func (s *KoraTenantStatus) DeepCopyInto(out *KoraTenantStatus) {
	*out = *s
	if s.LastReconciledAt != nil {
		t := *s.LastReconciledAt
		out.LastReconciledAt = &t
	}
	if s.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(s.Conditions))
		copy(out.Conditions, s.Conditions)
	}
}
