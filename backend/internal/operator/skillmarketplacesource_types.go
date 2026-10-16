// SkillMarketplaceSource CRD Go types.
//
// A SkillMarketplaceSource registers one GitHub repository as a skill
// marketplace for the tenant that owns the namespace it is applied in. The
// tenant comes from the namespace, never the spec. The operator's
// SkillMarketplaceAggregator renders it into the orbit-system
// orbit-skill-marketplace-sources ConfigMap skills-service mounts, but only
// once its owner/repo is on the tenant's spec.allowedMarketplaceRepos, and
// reports the verdict on its status.
//
// See docs/adr/2026-10-13-skill-marketplace-source-registration.md.
package operator

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// SkillMarketplaceSource phases.
const (
	// SkillMarketplaceSourcePhasePending means the source is valid but not
	// served yet: its tenant is inactive, or its repository is not approved.
	SkillMarketplaceSourcePhasePending = "Pending"
	// SkillMarketplaceSourcePhaseAggregated means the source was rendered into
	// orbit-skill-marketplace-sources for its tenant.
	SkillMarketplaceSourcePhaseAggregated = "Aggregated"
	// SkillMarketplaceSourcePhaseInvalid means the source can never be served
	// as applied: it clashes with an older sibling, or it is in orbit-system.
	SkillMarketplaceSourcePhaseInvalid = "Invalid"
)

// Reasons on a SkillMarketplaceSource's Aggregated condition.
const (
	MarketplaceReasonAggregated                = "Aggregated"
	MarketplaceReasonTenantNotActive           = "TenantNotActive"
	MarketplaceReasonNotAllowlisted            = "NotAllowlisted"
	MarketplaceReasonGlobalSourcesNotSupported = "GlobalSourcesNotSupported"
	MarketplaceReasonDuplicateName             = "DuplicateName"
	MarketplaceReasonDuplicateRepository       = "DuplicateRepository"
)

// defaultMarketplaceBranch is the branch a source without spec.github.branch
// is read from. The CRD schema defaults it too; this covers objects the API
// server did not default, such as test fixtures.
const defaultMarketplaceBranch = "main"

// SkillMarketplaceSource registers a GitHub repository as a skill marketplace
// for the tenant that owns its namespace.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=osms
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.github.owner`
// +kubebuilder:printcolumn:name="Repo",type=string,JSONPath=`.spec.github.repo`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Aggregated")].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SkillMarketplaceSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkillMarketplaceSourceSpec   `json:"spec,omitempty"`
	Status SkillMarketplaceSourceStatus `json:"status,omitempty"`
}

// SkillMarketplaceSourceSpec is the declared marketplace.
type SkillMarketplaceSourceSpec struct {
	// DisplayName is shown in the dashboard. Its slug is the marketplace's URL
	// segment, so it must contain an ASCII letter or digit and be unique within
	// the tenant.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=100
	// +kubebuilder:validation:XValidation:rule="self.matches('[A-Za-z0-9]')"
	DisplayName string `json:"displayName"`

	// GitHub is the repository to read skills from.
	GitHub SkillMarketplaceSourceGitHub `json:"github"`
}

// SkillMarketplaceSourceGitHub names a GitHub repository and branch.
type SkillMarketplaceSourceGitHub struct {
	// Owner is the organization or user account that owns the repository.
	// +kubebuilder:validation:MaxLength=39
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`
	Owner string `json:"owner"`

	// Repo is the repository name.
	// +kubebuilder:validation:MaxLength=100
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9._-]+$`
	// +kubebuilder:validation:XValidation:rule="self != '.' && self != '..'"
	Repo string `json:"repo"`

	// Branch is the branch to read. Defaults to main.
	// +kubebuilder:default=main
	// +kubebuilder:validation:MaxLength=255
	// +optional
	Branch string `json:"branch,omitempty"`
}

// repoKey is the lowercase owner/repo this source names. GitHub treats owner
// and repository names case-insensitively, so comparisons use this form.
func (g SkillMarketplaceSourceGitHub) repoKey() string {
	return strings.ToLower(g.Owner + "/" + g.Repo)
}

// effectiveBranch defaults an empty branch to main.
func (g SkillMarketplaceSourceGitHub) effectiveBranch() string {
	if g.Branch == "" {
		return defaultMarketplaceBranch
	}
	return g.Branch
}

// SkillMarketplaceSourceStatus is the operator's verdict on the source.
// Whether GitHub can actually serve the repository is skills-service runtime
// state, not reported here.
type SkillMarketplaceSourceStatus struct {
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

// SkillMarketplaceSourceList is a list of SkillMarketplaceSource resources.
// +kubebuilder:object:root=true
type SkillMarketplaceSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkillMarketplaceSource `json:"items"`
}

// -----------------------------------------------------------------------------
// DeepCopy (hand-written, mirroring MCPServer)
// -----------------------------------------------------------------------------

// DeepCopyObject implements runtime.Object.
func (s *SkillMarketplaceSource) DeepCopyObject() runtime.Object {
	if s == nil {
		return nil
	}
	out := new(SkillMarketplaceSource)
	s.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out. The spec holds only strings.
func (s *SkillMarketplaceSource) DeepCopyInto(out *SkillMarketplaceSource) {
	*out = *s
	out.TypeMeta = s.TypeMeta
	s.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = s.Spec
	s.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (s *SkillMarketplaceSource) DeepCopy() *SkillMarketplaceSource {
	if s == nil {
		return nil
	}
	out := new(SkillMarketplaceSource)
	s.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the status, including the time pointer and conditions.
func (s *SkillMarketplaceSourceStatus) DeepCopyInto(out *SkillMarketplaceSourceStatus) {
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

// DeepCopyObject implements runtime.Object for SkillMarketplaceSourceList.
func (l *SkillMarketplaceSourceList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(SkillMarketplaceSourceList)
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *SkillMarketplaceSourceList) DeepCopyInto(out *SkillMarketplaceSourceList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]SkillMarketplaceSource, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
