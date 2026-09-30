package operator

import (
	"slices"
	"strconv"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/clear-route/orbit/internal/platform/ports"
)

// allowedPorts lists every "namespace/app:port" the policy admits. An empty
// namespace is the policy's own; an empty app is any pod.
func allowedPorts(p *networkingv1.NetworkPolicy) []string {
	var out []string
	for _, rule := range p.Spec.Egress {
		for _, peer := range rule.To {
			ns, app := "", ""
			if peer.NamespaceSelector != nil {
				ns = peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
			}
			if peer.PodSelector != nil {
				for k, v := range peer.PodSelector.MatchLabels {
					app = k + "=" + v
				}
			}
			for _, port := range rule.Ports {
				out = append(out, ns+"/"+app+":"+port.Port.String()+"/"+string(*port.Protocol))
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestAgentEgressPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		collector AgentCollector
		extra     []string
	}{
		{name: "no collector"},
		{name: "namespace without a port", collector: AgentCollector{Namespace: "observability"}},
		{
			name:      "in-cluster collector",
			collector: AgentCollector{Namespace: "observability", Port: 4317},
			extra:     []string{"observability/:4317/TCP"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := agentEgressPolicy("orbit-tenant-acme", "acme", tc.collector)

			if p.Spec.PodSelector.MatchLabels["orbit.io/managed-agent"] != "true" {
				t.Errorf("pod selector = %v, want agent pods", p.Spec.PodSelector.MatchLabels)
			}
			if len(p.Spec.PolicyTypes) != 1 || p.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
				t.Errorf("policy types = %v, want Egress only", p.Spec.PolicyTypes)
			}
			want := append([]string{
				"kube-system/k8s-app=kube-dns:53/UDP",
				"kube-system/k8s-app=kube-dns:53/TCP",
				istioNamespace + "/:15012/TCP",
				systemNamespace + "/app=orbit-identity:8080/TCP",
				systemNamespace + "/app=orbit-agents:8080/TCP",
				systemNamespace + "/app=orbit-inference-gateway:8081/TCP",
				systemNamespace + "/app=orbit-tool-gateway:8096/TCP",
				systemNamespace + "/app=orbit-policy-service:8083/TCP",
				"/orbit.io/role=sandbox:" + strconv.Itoa(ports.SandboxExecGRPC) + "/TCP",
			}, tc.extra...)
			slices.Sort(want)
			if got := allowedPorts(p); !slices.Equal(got, want) {
				t.Errorf("allowed =\n%v\nwant\n%v", got, want)
			}
		})
	}
}

func TestAgentEgressSidecar(t *testing.T) {
	s := agentEgressSidecar("orbit-tenant-acme", "acme", AgentCollector{Namespace: "observability", Port: 4317})

	if s.GetKind() != "Sidecar" || s.GetNamespace() != "orbit-tenant-acme" || s.GetName() != agentEgressName {
		t.Fatalf("object = %s %s/%s", s.GetKind(), s.GetNamespace(), s.GetName())
	}
	spec := s.Object["spec"].(map[string]any)
	if mode := spec["outboundTrafficPolicy"].(map[string]any)["mode"]; mode != "REGISTRY_ONLY" {
		t.Errorf("mode = %v, want REGISTRY_ONLY", mode)
	}
	if sel := spec["workloadSelector"].(map[string]any)["labels"].(map[string]any); sel["orbit.io/managed-agent"] != "true" {
		t.Errorf("workload selector = %v, want agent pods", sel)
	}
	hosts := spec["egress"].([]any)[0].(map[string]any)["hosts"].([]any)
	want := []any{
		systemNamespace + "/orbit-identity." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/orbit-agents." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/orbit-inference-gateway." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/orbit-tool-gateway." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/orbit-policy-service." + systemNamespace + ".svc.cluster.local",
		"observability/*",
	}
	if !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}

	noCollector := agentEgressSidecar("orbit-tenant-acme", "acme", AgentCollector{})
	if n := len(noCollector.Object["spec"].(map[string]any)["egress"].([]any)[0].(map[string]any)["hosts"].([]any)); n != len(agentPlatformServices) {
		t.Errorf("hosts without a collector = %d, want %d", n, len(agentPlatformServices))
	}
}
