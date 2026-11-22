package operator

import (
	"context"
	"reflect"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSandboxEgressSidecar(t *testing.T) {
	s := sandboxEgressSidecar("kora-tenant-acme", "acme")

	if s.GetKind() != "Sidecar" || s.GetNamespace() != "kora-tenant-acme" || s.GetName() != sandboxEgressSidecarName {
		t.Fatalf("object = %s %s/%s", s.GetKind(), s.GetNamespace(), s.GetName())
	}
	if s.GetLabels()["runkora.io/tenant"] != "acme" {
		t.Errorf("labels = %v, want the tenant label", s.GetLabels())
	}
	spec := s.Object["spec"].(map[string]any)
	if _, ok := spec["outboundTrafficPolicy"]; ok {
		t.Errorf("outboundTrafficPolicy = %v, want the mesh default", spec["outboundTrafficPolicy"])
	}
	if sel := spec["workloadSelector"].(map[string]any)["labels"].(map[string]any); len(sel) != 1 || sel["runkora.io/role"] != "sandbox" {
		t.Errorf("workload selector = %v, want sandbox pods only", sel)
	}
	hosts := spec["egress"].([]any)[0].(map[string]any)["hosts"].([]any)
	want := []any{
		systemNamespace + "/kora-identity." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/kora-policy-service." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/kora-egress-gateway." + systemNamespace + ".svc.cluster.local",
		"kube-system/kube-dns.kube-system.svc.cluster.local",
	}
	if !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}
}

func TestEnsureSandboxEgressSidecarPatchesExisting(t *testing.T) {
	stale := sandboxEgressSidecar("kora-tenant-acme", "acme")
	stale.Object["spec"].(map[string]any)["egress"] = []any{map[string]any{"hosts": []any{"*/*"}}}
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(stale).Build()
	r := &TenantReconciler{Client: c}
	tenant := &KoraTenant{ObjectMeta: metav1.ObjectMeta{Name: "acme"}}

	if err := r.ensureSandboxEgressSidecar(context.Background(), tenant); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(sidecarGVK)
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "kora-tenant-acme", Name: sandboxEgressSidecarName}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	hosts, _, _ := unstructured.NestedSlice(got.Object, "spec", "egress")
	want := sandboxEgressSidecar("kora-tenant-acme", "acme").Object["spec"].(map[string]any)["egress"].([]any)
	if !reflect.DeepEqual(hosts, want) {
		t.Errorf("egress = %v, want %v", hosts, want)
	}
}
