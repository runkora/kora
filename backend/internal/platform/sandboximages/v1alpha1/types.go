// +kubebuilder:object:generate=true
// +groupName=runkora.io

// Package v1alpha1 holds the SandboxImage API types. The operator validates
// and warms each image. kora-agents reads its status when it starts a sandbox.
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
var GroupVersion = schema.GroupVersion{Group: "runkora.io", Version: "v1alpha1"}

// Resource is the plural resource name that dynamic clients use.
var Resource = GroupVersion.WithResource("sandboximages")

// BaseLabel marks an image built on kora-sandbox-base. Its value is the base version.
const BaseLabel = "io.runkora.sandbox.base"

// BaseName is the SandboxImage the operator keeps in every tenant namespace for
// the base image. An environment without an image uses it.
const BaseName = "kora-base"

// BaseDescription describes the base image wherever it is listed.
func BaseDescription(product string) string {
	return "The " + product + " base image: general command-line tools, Go and Python."
}

// BundledComponent is the runkora.io/component label value on the bundled
// SandboxImages the operator keeps in tenant namespaces. Together with the
// managed-by label it marks the resources the operator may patch and delete.
const BundledComponent = "bundled-sandbox-image"

// BundledImage is one sandbox image kora ships and the chart registers in
// every tenant (docs/adr/2026-10-07-bundled-sandbox-images.md).
type BundledImage struct {
	// Key is the image's key under agents.sandboxImages.bundled in the chart values.
	Key  string
	Name string
	// Extras is what the image adds to the base image, for Description.
	Extras string
}

// Description describes the image wherever it is listed.
func (b BundledImage) Description(product string) string {
	return "The " + product + " base image plus " + b.Extras + "."
}

// Bundled lists the bundled sandbox images.
var Bundled = []BundledImage{
	{Key: "go", Name: "kora-go", Extras: "golangci-lint, buf and protoc"},
	{Key: "python", Name: "kora-python", Extras: "ruff and pytest"},
	{Key: "node", Name: "kora-node", Extras: "Node LTS and pnpm"},
}

// SandboxUID is the user every sandbox image runs as.
const SandboxUID = 100

// Condition types on SandboxImage status.
const (
	ConditionValid = "Valid"
	ConditionWarm  = "Warm"
)

// SandboxImage registers one sandbox image for the tenant whose namespace holds
// it. The image must be built on kora-sandbox-base: it runs as uid 100 and
// carries the io.runkora.sandbox.base label. The operator validates it and
// resolves its digest. With warm set, it keeps the image pulled on the nodes
// that run sandboxes. Environments select it by name. The operator owns
// kora-base, the base image, in every tenant namespace, and the enabled bundled
// images kora-go, kora-python and kora-node.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ksi
// +kubebuilder:printcolumn:name=Image,type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name=Valid,type=string,JSONPath=`.status.conditions[?(@.type=="Valid")].status`
// +kubebuilder:printcolumn:name=Warm,type=string,JSONPath=`.status.conditions[?(@.type=="Warm")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must be a DNS label; it names the image's pre-pull DaemonSet."
type SandboxImage struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	Spec Spec `json:"spec"`
	// +optional
	Status Status `json:"status"`
}

// Spec names the image and how to pull it.
type Spec struct {
	// Image is the image reference, by tag or by digest.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`
	// Description is shown to people choosing an image for an environment.
	Description string `json:"description,omitempty"`
	// PullSecretRef is a docker-registry Secret in this namespace. The operator
	// reads the registry with it and sandboxes pull with it.
	PullSecretRef *SecretNameRef `json:"pullSecretRef,omitempty"`
	// Warm keeps the image pulled on every node that runs sandboxes, so a
	// sandbox starts without a pull.
	// +kubebuilder:default=false
	// +optional
	Warm bool `json:"warm"`
}

// SecretNameRef names a Secret in the resource's namespace.
type SecretNameRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Status is written by the operator. Image and Digest are the reference it
// last read from the registry and the digest that reference resolved to.
type Status struct {
	// Image is the image reference the operator last read from the registry.
	// Digest is what it resolved to.
	Image string `json:"image,omitempty"`
	// Digest is the resolved image digest. Sandboxes pull the image by it.
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
	// A colon after the last slash starts a tag. A colon before it is a registry port.
	if i, j := strings.LastIndexByte(repo, ':'), strings.LastIndexByte(repo, '/'); i > j {
		repo = repo[:i]
	}
	return repo + "@" + digest
}

// SandboxImageList is a list of SandboxImage resources.
//
// +kubebuilder:object:root=true
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
