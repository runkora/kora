package operator

import "testing"

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
