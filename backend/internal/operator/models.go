// Package operator reconciles tenant resources and enforces tenant workload prerequisites.
package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the API group and version for Orbit CRDs.
var GroupVersion = schema.GroupVersion{Group: "orbit.io", Version: "v1alpha1"}

// -----------------------------------------------------------------------------
// OrbitTenant: cluster-scoped, one per tenant
// -----------------------------------------------------------------------------

// OrbitTenant declares a tenant's existence, OIDC configuration, and namespace.
//
// Scope is chart-driven, not fixed by this marker: charts/orbit-crds renders
// scope: Cluster or scope: Namespaced from global.tenantScope (default
// "cluster", preserving today's behavior), so a single cluster can run either
// mode — never both at once, since a CustomResourceDefinition's scope is a
// single cluster-wide property. `make manifests` regenerates this marker's
// scope as Cluster; that is the tool's default output, not a statement that
// namespaced mode is unsupported. See settings.go's tenantScope /
// namespacedTenants for the corresponding runtime behavior.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type OrbitTenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OrbitTenantSpec   `json:"spec,omitempty"`
	Status OrbitTenantStatus `json:"status,omitempty"`
}

// OrbitTenantSpec defines the desired state of a tenant.
type OrbitTenantSpec struct {
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

// OrbitTenantStatus records observed tenant state.
type OrbitTenantStatus struct {
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

// OrbitTenantList is a list of OrbitTenant resources.
// +kubebuilder:object:root=true
type OrbitTenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OrbitTenant `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (t *OrbitTenant) DeepCopyObject() runtime.Object {
	if t == nil {
		return nil
	}
	out := new(OrbitTenant)
	t.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (t *OrbitTenant) DeepCopyInto(out *OrbitTenant) {
	*out = *t
	out.TypeMeta = t.TypeMeta
	t.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	t.Spec.DeepCopyInto(&out.Spec)
	t.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (t *OrbitTenant) DeepCopy() *OrbitTenant {
	if t == nil {
		return nil
	}
	out := new(OrbitTenant)
	t.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object for OrbitTenantList.
func (l *OrbitTenantList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(OrbitTenantList)
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *OrbitTenantList) DeepCopyInto(out *OrbitTenantList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]OrbitTenant, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopyInto for OrbitTenantSpec.
func (s *OrbitTenantSpec) DeepCopyInto(out *OrbitTenantSpec) {
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

// DeepCopyInto for OrbitTenantStatus.
func (s *OrbitTenantStatus) DeepCopyInto(out *OrbitTenantStatus) {
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
