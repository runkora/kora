package agents

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func destinationRuleTestConfig() K8sProvisionerConfig {
	return K8sProvisionerConfig{SystemNamespace: "kora-system", TrustDomain: "runkora.test"}
}

func getDestinationRule(t *testing.T, p *K8sProvisioner, name string) (*unstructured.Unstructured, bool) {
	t.Helper()
	dr, err := p.dynamicClient.Resource(destinationRuleGVR).Namespace(p.cfg.SystemNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("get destinationrule %s: %v", name, err)
	}
	return dr, true
}

func destinationRuleTarget(t *testing.T, dr *unstructured.Unstructured) (host, san string) {
	t.Helper()
	host, _, _ = unstructured.NestedString(dr.Object, "spec", "host")
	settings, _, _ := unstructured.NestedSlice(dr.Object, "spec", "trafficPolicy", "portLevelSettings")
	if len(settings) != 1 {
		t.Fatalf("portLevelSettings = %v, want one entry", settings)
	}
	sans, _, _ := unstructured.NestedStringSlice(settings[0].(map[string]any), "tls", "subjectAltNames")
	if len(sans) != 1 {
		t.Fatalf("subjectAltNames = %v, want one", sans)
	}
	return host, sans[0]
}

func seedLegacyDestinationRule(t *testing.T, p *K8sProvisioner, slug, host string) {
	t.Helper()
	legacy := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "networking.istio.io/v1beta1",
		"kind":       "DestinationRule",
		"metadata":   map[string]any{"name": "agent-" + slug, "namespace": p.cfg.SystemNamespace},
		"spec":       map[string]any{"host": host},
	}}
	if _, err := p.dynamicClient.Resource(destinationRuleGVR).Namespace(p.cfg.SystemNamespace).Create(context.Background(), legacy, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed legacy destinationrule: %v", err)
	}
}

// Two tenants with an agent of the same slug each get a DestinationRule for
// their own service and SPIFFE ID.
func TestEnsureDestinationRule_SameSlugInTwoTenants(t *testing.T) {
	p, _, _ := ownerRefFixture(t, destinationRuleTestConfig(), "acme")
	ctx := context.Background()
	for _, ns := range []string{"kora-tenant-acme", "kora-tenant-globex"} {
		at := AgentType{ID: "cm-" + ns, Slug: "change-mapper", TenantID: "acme"}
		if err := p.ensureDestinationRule(ctx, at, ns, metav1.OwnerReference{}, true); err != nil {
			t.Fatalf("ensureDestinationRule in %s: %v", ns, err)
		}
	}
	for _, ns := range []string{"kora-tenant-acme", "kora-tenant-globex"} {
		dr, ok := getDestinationRule(t, p, agentDestinationRuleName("change-mapper", ns))
		if !ok {
			t.Fatalf("no destinationrule for %s", ns)
		}
		host, san := destinationRuleTarget(t, dr)
		if want := agentServiceHost("change-mapper", ns); host != want {
			t.Errorf("%s host = %q, want %q", ns, host, want)
		}
		at := AgentType{Slug: "change-mapper"}
		if want := p.agentSpiffeID(at, ns); san != want {
			t.Errorf("%s SAN = %q, want %q", ns, san, want)
		}
	}
}

// A rule under the earlier agent-<slug> name is removed when it is this
// agent's, and left alone when it belongs to another tenant's agent.
func TestEnsureDestinationRule_RemovesOnlyOwnLegacyRule(t *testing.T) {
	ctx := context.Background()
	at := AgentType{ID: "cm", Slug: "change-mapper", TenantID: "acme"}

	p, _, _ := ownerRefFixture(t, destinationRuleTestConfig(), "acme")
	seedLegacyDestinationRule(t, p, "change-mapper", agentServiceHost("change-mapper", "kora-tenant-acme"))
	if err := p.ensureDestinationRule(ctx, at, "kora-tenant-acme", metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureDestinationRule: %v", err)
	}
	if _, ok := getDestinationRule(t, p, "agent-change-mapper"); ok {
		t.Error("own legacy rule kept; Istio would see two rules for one host")
	}

	other, _, _ := ownerRefFixture(t, destinationRuleTestConfig(), "acme")
	seedLegacyDestinationRule(t, other, "change-mapper", agentServiceHost("change-mapper", "kora-tenant-globex"))
	if err := other.ensureDestinationRule(ctx, at, "kora-tenant-acme", metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureDestinationRule: %v", err)
	}
	if _, ok := getDestinationRule(t, other, "agent-change-mapper"); !ok {
		t.Error("another tenant's legacy rule was deleted")
	}
}

// Deleting an agent removes its own rule and never another tenant's.
func TestDeleteDestinationRule_LeavesOtherTenants(t *testing.T) {
	p, _, _ := ownerRefFixture(t, destinationRuleTestConfig(), "acme")
	ctx := context.Background()
	for _, ns := range []string{"kora-tenant-acme", "kora-tenant-globex"} {
		at := AgentType{ID: "cm-" + ns, Slug: "change-mapper", TenantID: "acme"}
		if err := p.ensureDestinationRule(ctx, at, ns, metav1.OwnerReference{}, true); err != nil {
			t.Fatalf("ensureDestinationRule in %s: %v", ns, err)
		}
	}
	seedLegacyDestinationRule(t, p, "change-mapper", agentServiceHost("change-mapper", "kora-tenant-globex"))

	if err := p.deleteDestinationRule(ctx, "change-mapper", "kora-tenant-acme"); err != nil {
		t.Fatalf("deleteDestinationRule: %v", err)
	}
	if _, ok := getDestinationRule(t, p, agentDestinationRuleName("change-mapper", "kora-tenant-acme")); ok {
		t.Error("deleted agent's rule kept")
	}
	if _, ok := getDestinationRule(t, p, agentDestinationRuleName("change-mapper", "kora-tenant-globex")); !ok {
		t.Error("other tenant's rule deleted")
	}
	if _, ok := getDestinationRule(t, p, "agent-change-mapper"); !ok {
		t.Error("other tenant's legacy rule deleted")
	}
}

// A tenant and slug whose names render another agent's rule name fail instead
// of adopting or deleting that agent's rule.
func TestDestinationRule_NameCollisionIsRefused(t *testing.T) {
	p, _, _ := ownerRefFixture(t, destinationRuleTestConfig(), "acme")
	ctx := context.Background()
	first := AgentType{ID: "1", Slug: "b-c", TenantID: "acme"}
	if err := p.ensureDestinationRule(ctx, first, "kora-tenant-a", metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureDestinationRule: %v", err)
	}
	second := AgentType{ID: "2", Slug: "c", TenantID: "acme"}
	if agentDestinationRuleName("c", "kora-tenant-a-b") != agentDestinationRuleName("b-c", "kora-tenant-a") {
		t.Fatal("fixture no longer collides")
	}
	if err := p.ensureDestinationRule(ctx, second, "kora-tenant-a-b", metav1.OwnerReference{}, true); err == nil {
		t.Error("colliding name was accepted")
	}
	if err := p.deleteDestinationRule(ctx, "c", "kora-tenant-a-b"); err != nil {
		t.Fatalf("deleteDestinationRule: %v", err)
	}
	dr, ok := getDestinationRule(t, p, agentDestinationRuleName("b-c", "kora-tenant-a"))
	if !ok {
		t.Fatal("first agent's rule was deleted")
	}
	if host, _ := destinationRuleTarget(t, dr); host != agentServiceHost("b-c", "kora-tenant-a") {
		t.Errorf("host = %q, want the first agent's", host)
	}
}
