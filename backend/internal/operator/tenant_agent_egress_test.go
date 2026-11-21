package operator

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/ports"
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
			if peer.IPBlock != nil {
				ns = peer.IPBlock.CIDR
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
		{name: "external collector without CIDRs", collector: AgentCollector{Host: "otel.example.com", Port: 4317}},
		{
			name:      "external collector",
			collector: AgentCollector{Host: "otel.example.com", Port: 4317, CIDRs: []string{"203.0.113.0/24", "198.51.100.7/32"}},
			extra:     []string{"203.0.113.0/24/:4317/TCP", "198.51.100.7/32/:4317/TCP"},
		},
		{
			name:      "in-cluster wins over CIDRs",
			collector: AgentCollector{Namespace: "observability", Host: "otel.observability.svc", Port: 4317, CIDRs: []string{"203.0.113.0/24"}},
			extra:     []string{"observability/:4317/TCP"},
		},
		{
			name:      "in-cluster collector",
			collector: AgentCollector{Namespace: "observability", Port: 4317},
			extra:     []string{"observability/:4317/TCP"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := agentEgressPolicy("kora-tenant-acme", "acme", tc.collector)

			if p.Spec.PodSelector.MatchLabels["runkora.io/managed-agent"] != "true" {
				t.Errorf("pod selector = %v, want agent pods", p.Spec.PodSelector.MatchLabels)
			}
			if len(p.Spec.PolicyTypes) != 1 || p.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
				t.Errorf("policy types = %v, want Egress only", p.Spec.PolicyTypes)
			}
			want := append([]string{
				"kube-system/k8s-app=kube-dns:53/UDP",
				"kube-system/k8s-app=kube-dns:53/TCP",
				istioNamespace + "/:15012/TCP",
				systemNamespace + "/app=kora-identity:8080/TCP",
				systemNamespace + "/app=kora-agents:8080/TCP",
				systemNamespace + "/app=kora-inference-gateway:8081/TCP",
				systemNamespace + "/app=kora-tool-gateway:8096/TCP",
				systemNamespace + "/app=kora-policy-service:8083/TCP",
				"/runkora.io/role=sandbox:" + strconv.Itoa(ports.SandboxExecGRPC) + "/TCP",
			}, tc.extra...)
			slices.Sort(want)
			if got := allowedPorts(p); !slices.Equal(got, want) {
				t.Errorf("allowed =\n%v\nwant\n%v", got, want)
			}
		})
	}
}

func TestAgentEgressSidecar(t *testing.T) {
	s := agentEgressSidecar("kora-tenant-acme", "acme", AgentCollector{Namespace: "observability", Port: 4317})

	if s.GetKind() != "Sidecar" || s.GetNamespace() != "kora-tenant-acme" || s.GetName() != agentEgressName {
		t.Fatalf("object = %s %s/%s", s.GetKind(), s.GetNamespace(), s.GetName())
	}
	spec := s.Object["spec"].(map[string]any)
	if mode := spec["outboundTrafficPolicy"].(map[string]any)["mode"]; mode != "REGISTRY_ONLY" {
		t.Errorf("mode = %v, want REGISTRY_ONLY", mode)
	}
	if sel := spec["workloadSelector"].(map[string]any)["labels"].(map[string]any); sel["runkora.io/managed-agent"] != "true" {
		t.Errorf("workload selector = %v, want agent pods", sel)
	}
	hosts := spec["egress"].([]any)[0].(map[string]any)["hosts"].([]any)
	want := []any{
		systemNamespace + "/kora-identity." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/kora-agents." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/kora-inference-gateway." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/kora-tool-gateway." + systemNamespace + ".svc.cluster.local",
		systemNamespace + "/kora-policy-service." + systemNamespace + ".svc.cluster.local",
		"observability/*",
	}
	if !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}

	external := agentEgressSidecar("kora-tenant-acme", "acme", AgentCollector{Host: "otel.example.com", Port: 4317, CIDRs: []string{"203.0.113.0/24"}})
	extHosts := external.Object["spec"].(map[string]any)["egress"].([]any)[0].(map[string]any)["hosts"].([]any)
	if last := extHosts[len(extHosts)-1]; last != "*/otel.example.com" {
		t.Errorf("external collector host = %v, want */otel.example.com", last)
	}

	noCollector := agentEgressSidecar("kora-tenant-acme", "acme", AgentCollector{})
	if n := len(noCollector.Object["spec"].(map[string]any)["egress"].([]any)[0].(map[string]any)["hosts"].([]any)); n != len(agentPlatformServices) {
		t.Errorf("hosts without a collector = %d, want %d", n, len(agentPlatformServices))
	}
}

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
