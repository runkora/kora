package config_test

import "testing"

// A pool pod gets a pool SPIFFE ID until the bind sets spiffe.io/agent, and the
// agent entry selects only bound pods (docs/adr/2026-11-22-agent-cold-tier.md).
func TestChartPoolSPIFFEIDExcludesBoundPods(t *testing.T) {
	ids := map[string]map[string]any{}
	for _, doc := range renderDocs(t, "templates/platform/clusterspiffeids.yaml") {
		if doc["kind"] == "ClusterSPIFFEID" {
			ids[field(doc, "metadata", "name").(string)] = doc
		}
	}
	pool, ok := ids["kora-tenant-pool"]
	if !ok {
		t.Fatal("no kora-tenant-pool ClusterSPIFFEID")
	}
	if got, want := field(pool, "spec", "spiffeIDTemplate"), "spiffe://example.com/tenant/{{ .PodMeta.Namespace }}/pool/{{ .PodMeta.Name }}"; got != want {
		t.Errorf("pool template = %v, want %v", got, want)
	}
	exprs := map[string]string{}
	for _, e := range field(pool, "spec", "podSelector", "matchExpressions").([]any) {
		m := e.(map[string]any)
		exprs[m["key"].(string)] = m["operator"].(string)
	}
	want := map[string]string{"runkora.io/pool": "Exists", "spiffe.io/agent": "DoesNotExist", "spiffe.io/service": "DoesNotExist", "runkora.io/role": "NotIn"}
	for k, op := range want {
		if exprs[k] != op {
			t.Errorf("pool pod selector %s = %q, want %q", k, exprs[k], op)
		}
	}
	if got := field(pool, "spec", "namespaceSelector", "matchLabels", "runkora.io/role"); got != "tenant" {
		t.Errorf("pool namespace selector role = %v, want tenant", got)
	}
}
