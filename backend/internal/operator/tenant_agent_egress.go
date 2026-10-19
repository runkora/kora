// Agent pod egress lockdown: the agent pod reaches only the platform services
// it needs. See docs/adr/2026-09-30-agent-pod-egress-lockdown.md.
package operator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/platform/ports"
)

// agentEgressName names both the NetworkPolicy and the Istio Sidecar.
const agentEgressName = "agent-egress"

// agentPodLabels select agent pods; orbit-agents sets them on every one.
var agentPodLabels = map[string]string{"orbit.io/managed-agent": "true"}

// sidecarGVK is Istio's, which is not in the operator's scheme.
var sidecarGVK = schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1", Kind: "Sidecar"}

// agentPlatformService is a system-namespace service an agent pod calls. The
// port is the container port, which is what a NetworkPolicy matches.
type agentPlatformService struct {
	app  string
	port int
}

var agentPlatformServices = []agentPlatformService{
	{app: "orbit-identity", port: 8080},          // identity-proxy token exchange, OPA JWKS
	{app: "orbit-agents", port: 8080},            // run events, state, summaries, sandbox provisioning
	{app: "orbit-inference-gateway", port: 8081}, // model calls
	{app: "orbit-tool-gateway", port: 8096},      // governed MCP servers
	{app: "orbit-policy-service", port: 8083},    // OPA bundles
}

// AgentCollector is the OTLP collector agent pods export to: in the cluster
// (Namespace), or outside it at CIDRs, reached by Host through a ServiceEntry.
// Neither set opens nothing.
type AgentCollector struct {
	Namespace string
	Host      string
	Port      int
	CIDRs     []string
}

func (c AgentCollector) inCluster() bool { return c.Namespace != "" && c.Port != 0 }

func (c AgentCollector) external() bool {
	return !c.inCluster() && c.Host != "" && c.Port != 0 && len(c.CIDRs) > 0
}

// ensureAgentEgressPolicy reconciles the NetworkPolicy that limits agent pod
// egress to the platform services, the tenant's sandboxes and the collector.
func (r *TenantReconciler) ensureAgentEgressPolicy(ctx context.Context, tenant *KoraTenant) error {
	return r.ensureNetworkPolicy(ctx, tenant, agentEgressPolicy(tenantNamespace(tenant), tenant.Name, r.Collector))
}

func agentEgressPolicy(ns, tenantName string, collector AgentCollector) *networkingv1.NetworkPolicy {
	egress := []networkingv1.NetworkPolicyEgressRule{
		{
			To: namespacePeer("kube-system", map[string]string{"k8s-app": "kube-dns"}),
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: new(corev1.ProtocolUDP), Port: portIntOrString(53)},
				{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(53)},
			},
		},
		{To: namespacePeer(istioNamespace, nil), Ports: tcpPort(15012)},
	}
	for _, svc := range agentPlatformServices {
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			To:    namespacePeer(systemNamespace, map[string]string{"app": svc.app}),
			Ports: tcpPort(svc.port),
		})
	}
	// No NamespaceSelector: the peer is this namespace's sandboxes only.
	egress = append(egress, networkingv1.NetworkPolicyEgressRule{
		To:    []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"orbit.io/role": "sandbox"}}}},
		Ports: tcpPort(ports.SandboxExecGRPC),
	})
	switch {
	case collector.inCluster():
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			To:    namespacePeer(collector.Namespace, nil),
			Ports: tcpPort(collector.Port),
		})
	case collector.external():
		peers := make([]networkingv1.NetworkPolicyPeer, 0, len(collector.CIDRs))
		for _, cidr := range collector.CIDRs {
			peers = append(peers, networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: cidr}})
		}
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{To: peers, Ports: tcpPort(collector.Port)})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: agentEgressName, Namespace: ns, Labels: tenantPolicyLabels(tenantName)},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: agentPodLabels},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      egress,
		},
	}
}

// ensureAgentEgressSidecar reconciles the Istio Sidecar that has the agent
// pod's Envoy refuse any other host at once, instead of the NetworkPolicy
// leaving the connection to time out.
func (r *TenantReconciler) ensureAgentEgressSidecar(ctx context.Context, tenant *KoraTenant) error {
	desired := agentEgressSidecar(tenantNamespace(tenant), tenant.Name, r.Collector)
	err := r.Create(ctx, desired)
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := r.Patch(ctx, desired, client.Merge, client.FieldOwner("orbit-operator")); err != nil {
		return fmt.Errorf("patch Sidecar %q in %q: %w", desired.GetName(), desired.GetNamespace(), err)
	}
	return nil
}

func agentEgressSidecar(ns, tenantName string, collector AgentCollector) *unstructured.Unstructured {
	hosts := make([]any, 0, len(agentPlatformServices)+1)
	for _, svc := range agentPlatformServices {
		hosts = append(hosts, fmt.Sprintf("%s/%s.%s.svc.cluster.local", systemNamespace, svc.app, systemNamespace))
	}
	switch {
	case collector.inCluster():
		hosts = append(hosts, collector.Namespace+"/*")
	case collector.external():
		// The install's ServiceEntry for the collector, in whichever namespace it lives.
		hosts = append(hosts, "*/"+collector.Host)
	}
	sidecar := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": agentEgressName, "namespace": ns, "labels": toAnyMap(tenantPolicyLabels(tenantName))},
		"spec": map[string]any{
			"workloadSelector":      map[string]any{"labels": toAnyMap(agentPodLabels)},
			"outboundTrafficPolicy": map[string]any{"mode": "REGISTRY_ONLY"},
			"egress":                []any{map[string]any{"hosts": hosts}},
		},
	}}
	sidecar.SetGroupVersionKind(sidecarGVK)
	return sidecar
}

func tenantPolicyLabels(tenantName string) map[string]string {
	return map[string]string{"app.kubernetes.io/managed-by": "orbit-operator", "orbit.io/tenant": tenantName}
}

func namespacePeer(namespace string, pods map[string]string) []networkingv1.NetworkPolicyPeer {
	peer := networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}},
	}
	if pods != nil {
		peer.PodSelector = &metav1.LabelSelector{MatchLabels: pods}
	}
	return []networkingv1.NetworkPolicyPeer{peer}
}

func tcpPort(port int) []networkingv1.NetworkPolicyPort {
	return []networkingv1.NetworkPolicyPort{{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(port)}}
}
