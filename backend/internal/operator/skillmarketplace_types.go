// SkillMarketplace CRD Go types.
//
// A SkillMarketplace registers one GitHub repository, and the GitHub App that
// reads it, as a skill Marketplace for the tenant that owns the namespace it is
// applied in. The SkillMarketplaceAggregator renders it into the kora-system
// ConfigMap and Secret skills-service mounts and reports the verdict on its
// status.
//
// See docs/adr/2026-08-17-agent-skills.md.
package operator

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SkillMarketplace phases.
const (
	// SkillMarketplacePhasePending means no active tenant owns the namespace.
	SkillMarketplacePhasePending = "Pending"
	// SkillMarketplacePhaseAggregated means the Marketplace is served.
	SkillMarketplacePhaseAggregated = "Aggregated"
	// SkillMarketplacePhaseInvalid means the Marketplace cannot be served as
	// applied.
	SkillMarketplacePhaseInvalid = "Invalid"
)

// Reasons on a SkillMarketplace's Aggregated condition.
const (
	MarketplaceReasonAggregated                = "Aggregated"
	MarketplaceReasonCredentialUnavailable     = "CredentialUnavailable"
	MarketplaceReasonTenantNotActive           = "TenantNotActive"
	MarketplaceReasonGlobalSourcesNotSupported = "GlobalSourcesNotSupported"
	MarketplaceReasonDuplicateRepository       = "DuplicateRepository"
)

// SkillMarketplace registers a GitHub repository as a skill Marketplace for
// the tenant that owns its namespace.
//
// The name is the Marketplace slug and part of the aggregated Secret's data
// key, which Kubernetes caps at 253 characters.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kskm
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 63",message="metadata.name must be at most 63 characters"
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.github.owner`
// +kubebuilder:printcolumn:name="Repo",type=string,JSONPath=`.spec.github.repo`
// +kubebuilder:printcolumn:name="Ref",type=string,JSONPath=`.spec.github.ref`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Aggregated")].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:object:generate=true
type SkillMarketplace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkillMarketplaceSpec   `json:"spec,omitempty"`
	Status SkillMarketplaceStatus `json:"status,omitempty"`
}

// SkillMarketplaceSpec is the declared Marketplace.
// +kubebuilder:object:generate=true
type SkillMarketplaceSpec struct {
	// Origin states whether the content is the organization's own (internal)
	// or third-party (external), whatever the repository's visibility.
	// +kubebuilder:validation:Enum=internal;external
	Origin string `json:"origin"`

	// GitHub is the repository to read skills from and the App that reads it.
	GitHub SkillMarketplaceGitHub `json:"github"`
}

// SkillMarketplaceGitHub names a GitHub repository and its App.
// +kubebuilder:object:generate=true
type SkillMarketplaceGitHub struct {
	// Owner is the organization or user account that owns the repository.
	// +kubebuilder:validation:MaxLength=39
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`
	Owner string `json:"owner"`

	// Repo is the repository name.
	// +kubebuilder:validation:MaxLength=100
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9._-]+$`
	// +kubebuilder:validation:XValidation:rule="self != '.' && self != '..'"
	Repo string `json:"repo"`

	// Ref selects what is served. A 40-character hex value pins a commit SHA,
	// any other value tracks a branch, and no value tracks the default branch.
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._/-]*$`
	// +optional
	Ref string `json:"ref,omitempty"`

	// App is the GitHub App installation that reads the repository.
	App SkillMarketplaceApp `json:"app"`
}

// SkillMarketplaceApp is a GitHub App installation and its private key.
// +kubebuilder:object:generate=true
type SkillMarketplaceApp struct {
	// AppID is the GitHub App ID.
	// +kubebuilder:validation:MaxLength=20
	// +kubebuilder:validation:Pattern=`^[0-9]+$`
	AppID string `json:"appID"`

	// InstallationID is the App's installation on the repository owner.
	// +kubebuilder:validation:Minimum=1
	InstallationID int64 `json:"installationID"`

	// PrivateKeySecretRef is the App private key in a Secret in this namespace.
	PrivateKeySecretRef SkillMarketplaceSecretRef `json:"privateKeySecretRef"`
}

// SkillMarketplaceSecretRef names one key of a Secret in the Marketplace's
// namespace.
// +kubebuilder:object:generate=true
type SkillMarketplaceSecretRef struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	Key string `json:"key"`
}

// repoKey is the lowercase owner/repo. GitHub treats both names
// case-insensitively, so comparisons use this form.
func (g SkillMarketplaceGitHub) repoKey() string {
	return strings.ToLower(g.Owner + "/" + g.Repo)
}

// SkillMarketplaceStatus is the operator's verdict on the Marketplace. Whether
// GitHub accepts the credential shows on the Skills page, not here.
// +kubebuilder:object:generate=true
type SkillMarketplaceStatus struct {
	// Phase is one of Pending|Aggregated|Invalid.
	// +optional
	Phase string `json:"phase,omitempty"`
	// LastReconciledAt is when the operator last changed this status.
	// +optional
	LastReconciledAt *metav1.Time `json:"lastReconciledAt,omitempty"`
	// Conditions carries the Aggregated condition, whose reason explains Phase.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// SkillMarketplaceList is a list of SkillMarketplace resources.
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
type SkillMarketplaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkillMarketplace `json:"items"`
}
