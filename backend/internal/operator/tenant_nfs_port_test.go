package operator

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
)

// sandboxMountPort is the chart's default sandboxMount.port, the node agent's nfsd port.
const sandboxMountPort = 20490

// Agent and sandbox pods must not reach the node agent's nfsd. The egress policies are
// allowlists, so the port is denied when no rule opens it and every rule names its ports.
func TestTenantEgressDeniesSandboxMountPort(t *testing.T) {
	for name, policy := range map[string]*networkingv1.NetworkPolicy{
		"agent":   agentEgressPolicy("ns", "acme", AgentCollector{Namespace: "observability", Port: 4317}),
		"sandbox": sandboxEgressPolicy("ns", "acme"),
	} {
		for _, rule := range policy.Spec.Egress {
			if len(rule.Ports) == 0 {
				t.Errorf("%s egress rule %+v allows every port", name, rule.To)
			}
			for _, p := range rule.Ports {
				if p.Port != nil && p.Port.IntValue() == sandboxMountPort {
					t.Errorf("%s egress allows TCP %d", name, sandboxMountPort)
				}
				if p.EndPort != nil && p.Port != nil && p.Port.IntValue() <= sandboxMountPort && int(*p.EndPort) >= sandboxMountPort {
					t.Errorf("%s egress range %d-%d covers %d", name, p.Port.IntValue(), *p.EndPort, sandboxMountPort)
				}
			}
		}
	}
}
