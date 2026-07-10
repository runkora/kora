package orbitapi

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// GroupVersion is the API group and version OrbitDevEnvironment belongs to.
// Must match internal/operator's own GroupVersion (orbit.io/v1alpha1) —
// both packages register types for the same CRD group, just from different
// binaries: the operator reconciles OrbitDevEnvironment, cmd/identity's
// devenvironments feature only creates/reads/deletes it.
var GroupVersion = schema.GroupVersion{Group: "orbit.io", Version: "v1alpha1"}

// AddToScheme registers OrbitDevEnvironment into a runtime.Scheme. Callers
// that also need the operator's other CRDs (OrbitPlatform, OrbitTenant, ...)
// should use internal/operator's own AddToScheme instead — it registers this
// same type (internal/operator type-aliases it from here) alongside its own.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &OrbitDevEnvironment{}, &OrbitDevEnvironmentList{})

	// Wire up metav1 types (ListOptions, WatchEvent, etc.) to our group
	// version. Without this, the reflector/informer machinery cannot
	// list/watch OrbitDevEnvironment and produces: "v1.ListOptions is not
	// suitable for converting to orbit.io/v1alpha1".
	metav1.AddToGroupVersion(s, GroupVersion)

	return nil
}

// NewScheme returns a runtime.Scheme with OrbitDevEnvironment and core
// Kubernetes types registered — everything a devenvironments feature client
// needs, and nothing more (decision 5's single-CRD RBAC posture, mirrored in
// the scheme it can even construct objects for).
func NewScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(AddToScheme(s))
	return s
}
