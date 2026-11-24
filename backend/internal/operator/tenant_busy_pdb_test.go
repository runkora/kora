package operator

import (
	"maps"
	"testing"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The reconcile creates both budgets, and a later reconcile patches a changed budget back to the desired spec.
func TestEnsureBusyPDBs(t *testing.T) {
	ctx := t.Context()
	c := fake.NewClientBuilder().WithScheme(NewScheme()).Build()
	r := &TenantReconciler{Client: c}
	tenant := &KoraTenant{ObjectMeta: metav1.ObjectMeta{Name: "acme"}}
	if err := r.ensureBusyPDBs(ctx, tenant); err != nil {
		t.Fatalf("create: %v", err)
	}
	get := func(name string) *policyv1.PodDisruptionBudget {
		pdb := &policyv1.PodDisruptionBudget{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: tenantNamespace(tenant), Name: name}, pdb); err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		return pdb
	}
	want := map[string]map[string]string{
		agentBusyPDBName:   {"runkora.io/busy": "true"},
		sandboxBusyPDBName: {"runkora.io/role": "sandbox"},
	}
	check := func() {
		t.Helper()
		for name, selector := range want {
			pdb := get(name)
			if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.IntValue() != 0 || pdb.Spec.MinAvailable != nil {
				t.Errorf("%s budget = %+v, want maxUnavailable 0", name, pdb.Spec)
			}
			if pdb.Spec.Selector == nil || !maps.Equal(pdb.Spec.Selector.MatchLabels, selector) {
				t.Errorf("%s selector = %v, want %v", name, pdb.Spec.Selector, selector)
			}
			if pdb.Labels["runkora.io/tenant"] != "acme" || pdb.Labels["app.kubernetes.io/managed-by"] != "kora-operator" {
				t.Errorf("%s labels = %v", name, pdb.Labels)
			}
		}
	}
	check()

	pdb := get(agentBusyPDBName)
	one := intstr.FromInt32(1)
	pdb.Spec.MaxUnavailable = &one
	pdb.Spec.Selector.MatchLabels = map[string]string{"app": "x"}
	if err := c.Update(ctx, pdb); err != nil {
		t.Fatal(err)
	}
	if err := r.ensureBusyPDBs(ctx, tenant); err != nil {
		t.Fatalf("patch: %v", err)
	}
	check()
}
