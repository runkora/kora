package operator

import (
	"context"
	"io"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// TestMarkReady_PatchesNamespaceEvenWhenAlreadyReady covers a steady-state
// reconcile: the phase is already Ready, so a patch built from setTenantPhase
// alone would no-op and never write Namespace or LastReconciledAt.
func TestMarkReady_PatchesNamespaceEvenWhenAlreadyReady(t *testing.T) {
	tenant := &OrbitTenant{
		ObjectMeta: metav1.ObjectMeta{Name: "example"},
		Spec:       OrbitTenantSpec{Active: true},
		Status:     OrbitTenantStatus{Phase: TenantPhaseReady},
	}
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(tenant).WithStatusSubresource(tenant).Build()
	r := &TenantReconciler{Client: c, Logger: logging.NewLoggerWithOutput(io.Discard)}

	ctx := context.Background()
	if err := r.markReady(ctx, tenant, "orbit-tenant-example"); err != nil {
		t.Fatalf("markReady: %v", err)
	}

	got := &OrbitTenant{}
	if err := c.Get(ctx, client.ObjectKey{Name: "example"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Namespace != "orbit-tenant-example" {
		t.Errorf("Status.Namespace = %q, want %q", got.Status.Namespace, "orbit-tenant-example")
	}
	if got.Status.LastReconciledAt == nil {
		t.Error("Status.LastReconciledAt not set")
	}
	if got.Status.Phase != TenantPhaseReady {
		t.Errorf("Status.Phase = %q, want %q", got.Status.Phase, TenantPhaseReady)
	}
}
