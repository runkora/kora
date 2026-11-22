package operator

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAgentIdentityRule(t *testing.T) {
	prev := trustDomain
	trustDomain = "runkora.test"
	t.Cleanup(func() { trustDomain = prev })

	rule := agentIdentityRule("kora-tenant-acme", "acme")
	if rule.GetKind() != "DestinationRule" || rule.GetNamespace() != "kora-tenant-acme" || rule.GetName() != agentIdentityRuleName {
		t.Fatalf("object = %s %s/%s", rule.GetKind(), rule.GetNamespace(), rule.GetName())
	}
	spec := rule.Object["spec"].(map[string]any)
	if host := spec["host"]; host != "kora-identity."+systemNamespace+".svc.cluster.local" {
		t.Errorf("host = %v", host)
	}
	if sel := field(spec, "workloadSelector", "matchLabels").(map[string]any); sel["runkora.io/managed-agent"] != "true" || len(sel) != 1 {
		t.Errorf("workload selector = %v, want agent pods", sel)
	}
	policy := spec["trafficPolicy"].(map[string]any)
	if n := field(policy, "connectionPool", "http", "maxRequestsPerConnection"); n != int64(1) {
		t.Errorf("maxRequestsPerConnection = %v, want 1", n)
	}
	if mode := field(policy, "tls", "mode"); mode != "ISTIO_MUTUAL" {
		t.Errorf("tls mode = %v", mode)
	}
	sans := field(policy, "tls", "subjectAltNames").([]any)
	if len(sans) != 1 || sans[0] != "spiffe://runkora.test/system/identity" {
		t.Errorf("subjectAltNames = %v", sans)
	}
	if _, ok := policy["portLevelSettings"]; ok {
		t.Error("portLevelSettings would replace the connection limit for its port")
	}
}

func TestEnsureAgentIdentityRuleNeedsTrustDomain(t *testing.T) {
	prev := trustDomain
	trustDomain = ""
	t.Cleanup(func() { trustDomain = prev })
	r := &TenantReconciler{}
	if err := r.ensureAgentIdentityRule(t.Context(), &KoraTenant{}); err == nil {
		t.Error("expected an error with no trust domain")
	}
}

func field(m any, path ...string) any {
	for _, p := range path {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[p]
	}
	return m
}

// The rule is created, and a later reconcile patches it back to the rendered spec.
func TestEnsureAgentIdentityRule_CreateThenPatch(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(NewScheme()).Build()
	r := &TenantReconciler{Client: c}
	tenant := &KoraTenant{ObjectMeta: metav1.ObjectMeta{Name: "acme"}}
	if err := r.ensureAgentIdentityRule(ctx, tenant); err != nil {
		t.Fatalf("create: %v", err)
	}
	get := func() *unstructured.Unstructured {
		rule := &unstructured.Unstructured{}
		rule.SetGroupVersionKind(destinationRuleGVK)
		if err := c.Get(ctx, client.ObjectKey{Namespace: tenantNamespace(tenant), Name: agentIdentityRuleName}, rule); err != nil {
			t.Fatalf("get rule: %v", err)
		}
		return rule
	}
	rule := get()
	if err := unstructured.SetNestedField(rule.Object, int64(100), "spec", "trafficPolicy", "connectionPool", "http", "maxRequestsPerConnection"); err != nil {
		t.Fatal(err)
	}
	if err := c.Update(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if err := r.ensureAgentIdentityRule(ctx, tenant); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if n, _, _ := unstructured.NestedInt64(get().Object, "spec", "trafficPolicy", "connectionPool", "http", "maxRequestsPerConnection"); n != 1 {
		t.Errorf("maxRequestsPerConnection after reconcile = %d, want 1", n)
	}
}
