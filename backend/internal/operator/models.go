// +kubebuilder:object:generate=false
// +groupName=runkora.io
// +versionName=v1alpha1

// Package operator reconciles tenant resources and enforces tenant workload prerequisites.
package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

// GroupVersion is the API group and version for kora CRDs.
var GroupVersion = schema.GroupVersion{Group: "runkora.io", Version: "v1alpha1"}

// -----------------------------------------------------------------------------
// KoraTenant: cluster-scoped, one per tenant
// -----------------------------------------------------------------------------

// KoraTenant declares a tenant's existence, OIDC configuration, and namespace.
// The CR name (metadata.name) is the tenant ID, so no separate tenantId field
// exists. The operator creates the tenant namespace, auto-derived as
// "kora-tenant-{name}" when spec.namespace is omitted, and registers the tenant
// in the kora-tenants ConfigMap consumed by the identity service. All tenant
// configuration lives in this single CR.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=kt
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="OIDC",type=string,JSONPath=`.spec.oidc.issuerURL`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="has(oldSelf.spec.__namespace__) ? (has(self.spec.__namespace__) && self.spec.__namespace__ == oldSelf.spec.__namespace__) : (!has(self.spec.__namespace__) || self.spec.__namespace__ == 'kora-tenant-' + self.metadata.name)",message="spec.namespace is immutable. Recreate the KoraTenant to use another namespace."
// +kubebuilder:object:generate=true
type KoraTenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec   KoraTenantSpec   `json:"spec,omitempty"`
	Status KoraTenantStatus `json:"status,omitempty"`
}

// KoraTenantSpec defines the desired state of a tenant.
//
// +kubebuilder:object:generate=true
type KoraTenantSpec struct {
	// DisplayName is the human-readable tenant name surfaced in the identity
	// service tenant registry.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`

	// Active controls whether the tenant is active. When false, the operator
	// sets state:suspended in the registry without deleting cluster resources.
	// +kubebuilder:default=true
	// +optional
	Active bool `json:"active"`

	// Namespace selects the tenant workload namespace, defaulting to
	// kora-tenant-<name>. The operator labels it but never deletes it.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// OIDC configures the identity provider for this tenant.
	OIDC TenantOIDCSpec `json:"oidc"`
}

// SecretRef is a reference to a Kubernetes Secret.
//
// +kubebuilder:object:generate=true
type SecretRef struct {
	// Name is the Secret name.
	Name string `json:"name"`
	// Namespace defaults to the system namespace if omitted.
	// +required
	Namespace string `json:"namespace,omitempty"`
}

// TenantOIDCSpec defines OIDC configuration for a tenant.
//
// +kubebuilder:object:generate=true
type TenantOIDCSpec struct {
	// IssuerURL is the OIDC issuer URL.
	IssuerURL string `json:"issuerURL"`

	// ClientID is the OIDC client ID registered with the identity provider.
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

	// ClaimBinding restricts membership to people whose ID token claim matches.
	// Tenants that share an email domain must share issuerURL and clientID and
	// each set a claimBinding.
	// See docs/adr/2026-10-26-tenant-claim-binding-and-shared-email-domains.md
	// +optional
	ClaimBinding *ClaimBinding `json:"claimBinding,omitempty"`
}

// ClaimBinding names an ID token claim and the values that bind a person to the
// tenant. A scalar claim must equal one of Values. An array claim such as
// groups must contain one of them.
//
// +kubebuilder:object:generate=true
type ClaimBinding struct {
	// Claim is the ID token claim name, for example groups.
	// +kubebuilder:validation:MinLength=1
	Claim string `json:"claim"`

	// Values are the claim values that bind a person to the tenant.
	// +kubebuilder:validation:MinItems=1
	Values []string `json:"values"`
}

// KoraTenantStatus records observed tenant state.
//
// +kubebuilder:object:generate=true
type KoraTenantStatus struct {
	// Phase summarizes tenant lifecycle state.
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
	ConditionEmailDomainsValid      = "EmailDomainsValid"
)

// Tenant phase values.
const (
	TenantPhasePending      = "Pending"
	TenantPhaseProvisioning = "Provisioning"
	TenantPhaseReady        = "Ready"
	TenantPhaseDegraded     = "Degraded"
	TenantPhaseSuspended    = "Suspended"
	TenantPhaseInvalid      = "Invalid"
)

// KoraTenantList is a list of KoraTenant resources.
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
type KoraTenantList struct {
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
