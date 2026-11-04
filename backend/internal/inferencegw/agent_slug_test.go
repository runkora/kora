package inferencegw

import "testing"

func TestAgentSlugFromPrincipal(t *testing.T) {
	t.Parallel()
	const ns = "spiffe://runkora.test/tenant/kora-tenant-example/agent/"
	tests := []struct {
		name, principalType, principalID, want string
	}{
		{"agent", "AGENT", ns + "token-probe", "token-probe"},
		{"sandbox svid records the agent", "AGENT", ns + "token-probe/sandbox/run-1", "token-probe"},
		{"other trust domain", "AGENT", "spiffe://example.com/tenant/kora-tenant-acme/agent/probe", "probe"},
		{"service principal", "SERVICE", "spiffe://runkora.test/tenant/kora-tenant-example/service/probe", ""},
		{"agent type on non-agent path", "AGENT", "spiffe://runkora.test/tenant/kora-tenant-example/service/probe", ""},
		{"human", "HUMAN", "CgNkZXYSCmxvY2FsLWxkYXA", ""},
		{"not spiffe", "AGENT", "agent:probe", ""},
		{"empty slug", "AGENT", ns, ""},
		{"too short", "AGENT", "spiffe://runkora.test/tenant/ns", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := agentSlugFromPrincipal(tc.principalType, tc.principalID); got != tc.want {
				t.Errorf("agentSlugFromPrincipal(%q, %q) = %q, want %q", tc.principalType, tc.principalID, got, tc.want)
			}
		})
	}
}
