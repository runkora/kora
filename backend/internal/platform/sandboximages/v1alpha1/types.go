// Package v1alpha1 holds the SandboxImage API types. The operator validates
// and warms each image; orbit-agents reads its status when it starts a sandbox.
//
// See docs/adr/2026-10-03-sandbox-environments.md.
package v1alpha1

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the API group and version of SandboxImage.
var GroupVersion = schema.GroupVersion{Group: "orbit.io", Version: "v1alpha1"}

// Resource is the plural resource name, for dynamic clients.
var Resource = GroupVersion.WithResource("sandboximages")

// BaseLabel marks an image built on kora-sandbox-base. Its value is the base version.
const BaseLabel = "io.orbit.sandbox.base"

// BaseName is the SandboxImage the operator keeps in every tenant namespace for
// the base image. An environment without an image uses it.
const BaseName = "kora-base"

// SandboxUID is the user every sandbox image runs as.
const SandboxUID = "100"

// Condition types on SandboxImage status.
const (
	ConditionValid = "Valid"
	ConditionWarm  = "Warm"
)

// SandboxImage registers one sandbox image in a tenant namespace.
type SandboxImage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   Spec   `json:"spec"`
	Status Status `json:"status"`
}

// Spec names the image and how to pull it.
type Spec struct {
	Image         string         `json:"image"`
	Description   string         `json:"description,omitempty"`
	PullSecretRef *SecretNameRef `json:"pullSecretRef,omitempty"`
	// Warm keeps the image pulled on every node that runs sandboxes.
	Warm bool `json:"warm"`
}

// SecretNameRef names a Secret in the resource's namespace.
type SecretNameRef struct {
	Name string `json:"name"`
}

// Status is written by the operator. Image and Digest are the reference it
// last read from the registry and the digest that reference resolved to.
type Status struct {
	Image              string             `json:"image,omitempty"`
	Digest             string             `json:"digest,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// Valid reasons that reject an image. Any other not-valid state may still pass on a later read.
const (
	ReasonWrongUser        = "WrongUser"
	ReasonMissingBaseLabel = "MissingBaseLabel"
)

// Valid reports whether the operator accepted the image the spec names now.
func (s *SandboxImage) Valid() bool {
	c := meta.FindStatusCondition(s.Status.Conditions, ConditionValid)
	return s.Status.Digest != "" && s.Status.Image == s.Spec.Image && c != nil && c.Status == metav1.ConditionTrue
}

// Rejected reports whether the operator read the image the spec names and refused it.
func (s *SandboxImage) Rejected() bool {
	c := meta.FindStatusCondition(s.Status.Conditions, ConditionValid)
	return s.Status.Image == s.Spec.Image && c != nil && c.Status == metav1.ConditionFalse &&
		(c.Reason == ReasonWrongUser || c.Reason == ReasonMissingBaseLabel)
}

// PinnedRef is the validated image reference pinned to its digest.
func (s *SandboxImage) PinnedRef() string {
	return pin(s.Status.Image, s.Status.Digest)
}

// pin replaces any tag or digest on ref with digest.
func pin(ref, digest string) string {
	repo := ref
	if i := strings.IndexByte(repo, '@'); i >= 0 {
		repo = repo[:i]
	}
	// A colon after the last slash starts a tag; one before it is a registry port.
	if i, j := strings.LastIndexByte(repo, ':'), strings.LastIndexByte(repo, '/'); i > j {
		repo = repo[:i]
	}
	return repo + "@" + digest
}

// SandboxImageList is a list of SandboxImage resources.
type SandboxImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []SandboxImage `json:"items"`
}

// AddToScheme registers the types.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &SandboxImage{}, &SandboxImageList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}

// DeepCopyObject implements runtime.Object.
func (s *SandboxImage) DeepCopyObject() runtime.Object {
	if s == nil {
		return nil
	}
	return s.DeepCopy()
}

// DeepCopy returns a deep copy.
func (s *SandboxImage) DeepCopy() *SandboxImage {
	if s == nil {
		return nil
	}
	out := new(SandboxImage)
	s.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (s *SandboxImage) DeepCopyInto(out *SandboxImage) {
	*out = *s
	s.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	if s.Spec.PullSecretRef != nil {
		r := *s.Spec.PullSecretRef
		out.Spec.PullSecretRef = &r
	}
	if s.Status.Conditions != nil {
		out.Status.Conditions = make([]metav1.Condition, len(s.Status.Conditions))
		for i := range s.Status.Conditions {
			s.Status.Conditions[i].DeepCopyInto(&out.Status.Conditions[i])
		}
	}
}

// DeepCopyObject implements runtime.Object for SandboxImageList.
func (l *SandboxImageList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(SandboxImageList)
	*out = *l
	l.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]SandboxImage, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
	return out
}
