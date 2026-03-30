package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// AddToScheme registers the Orbit CRD types into a runtime.Scheme.
// Call this on the controller-runtime manager's scheme before starting.
func AddToScheme(s *runtime.Scheme) error {
	// Register the concrete CRD types for orbit.io/v1alpha1.
	s.AddKnownTypes(GroupVersion,
		&OrbitPlatform{},
		&OrbitPlatformList{},
		&OrbitTenant{},
		&OrbitTenantList{},
	)

	// Wire up metav1 types (ListOptions, WatchEvent, etc.) to our group version.
	// Without this, the reflector/informer machinery cannot list or watch our CRDs
	// and produces: "v1.ListOptions is not suitable for converting to orbit.io/v1alpha1".
	metav1.AddToGroupVersion(s, GroupVersion)

	return nil
}

// NewScheme returns a runtime.Scheme with the Orbit types and core Kubernetes
// types registered. Use this in the controller-runtime manager constructor.
func NewScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(AddToScheme(s))
	return s
}
