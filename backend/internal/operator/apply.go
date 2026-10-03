package operator

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// applier server-side applies objects.
type applier interface {
	Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error
}

// apply server-side applies obj as orbit-operator. A field the operator applied
// before and leaves out now is removed. obj must carry its TypeMeta.
func apply(ctx context.Context, c applier, obj client.Object) error {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("convert %s %q: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
	}
	delete(u, "status")
	unstructured.RemoveNestedField(u, "metadata", "creationTimestamp")
	cfg := client.ApplyConfigurationFromUnstructured(&unstructured.Unstructured{Object: u})
	if err := c.Apply(ctx, cfg, client.FieldOwner("orbit-operator"), client.ForceOwnership); err != nil {
		return fmt.Errorf("apply %s %q in %q: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), obj.GetNamespace(), err)
	}
	return nil
}
