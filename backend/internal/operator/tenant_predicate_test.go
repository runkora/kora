package operator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// TestTenantPredicate_StatusPatchDoesNotRequeue pins the reason the tenant
// controller registers GenerationChangedPredicate: markReady patches status on
// every pass, and without the predicate that patch re-enqueued the tenant in a
// loop (50 reconciles a minute on the local cluster). A status patch bumps
// resourceVersion but not generation; a spec change bumps both.
func TestTenantPredicate_StatusPatchDoesNotRequeue(t *testing.T) {
	p := predicate.GenerationChangedPredicate{}

	old := &KoraTenant{ObjectMeta: metav1.ObjectMeta{Name: "example", Generation: 3, ResourceVersion: "100"}}
	statusOnly := old.DeepCopy()
	statusOnly.ResourceVersion = "101"
	statusOnly.Status.Phase = TenantPhaseReady
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: statusOnly}) {
		t.Fatal("a status-only update must not trigger a reconcile")
	}

	specChange := old.DeepCopy()
	specChange.ResourceVersion = "102"
	specChange.Generation = 4
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: specChange}) {
		t.Fatal("a spec change must trigger a reconcile")
	}
}
