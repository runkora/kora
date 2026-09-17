// MCP workload classification and the policies that front an MCP server.
//
// An MCPServer naming its serving pods makes them a workload class of their
// own: the pod webhook skips the identity-proxy and OPA an agent pod requires,
// and the policies below admit the tool-gateway and nothing else. See
// docs/adr/2026-09-17-mcp-workload-class.md.
package operator

import (
	"fmt"
	"net/url"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// podLabelMCPServer marks a pod the webhook matched to an MCPServer. The
	// webhook sets it from the CR; a pod that carries it without a matching CR
	// is treated as an agent pod, so the label grants nothing on its own.
	podLabelMCPServer = "orbit.io/mcp-server"

	// mcpToolGatewayApp is the tool-gateway pod label, set by its chart.
	mcpToolGatewayApp = "orbit-tool-gateway"

	// mcpToolGatewayComponent is the tool-gateway's spiffe.io/component, which
	// the platform ClusterSPIFFEID renders into
	// spiffe://<trustDomain>/system/<component>.
	mcpToolGatewayComponent = "tool-gateway"
)

// authorizationPolicyGVK is Istio's, which is not in the operator's scheme, so
// the policy is written as unstructured.
var authorizationPolicyGVK = schema.GroupVersionKind{
	Group:   "security.istio.io",
	Version: "v1beta1",
	Kind:    "AuthorizationPolicy",
}

// classifies reports whether this MCPServer names pods, and if so whether the
// given pod labels match. A CR without a workload selector classifies nothing.
func (m *MCPServer) classifies(podLabels map[string]string) bool {
	if m.Spec.Workload == nil || len(m.Spec.Workload.Selector) == 0 {
		return false
	}
	for k, want := range m.Spec.Workload.Selector {
		if podLabels[k] != want {
			return false
		}
	}
	return true
}

// endpointPort is the TCP port the tool-gateway dials, taken from the endpoint
// URL and falling back to the scheme's default.
func (m *MCPServer) endpointPort() (int32, error) {
	u, err := url.Parse(m.Spec.Endpoint)
	if err != nil {
		return 0, fmt.Errorf("parse endpoint %q: %w", m.Spec.Endpoint, err)
	}
	if p := u.Port(); p != "" {
		n, convErr := strconv.Atoi(p)
		if convErr != nil || n < 1 || n > 65535 {
			return 0, fmt.Errorf("endpoint %q has an invalid port", m.Spec.Endpoint)
		}
		return int32(n), nil //nolint:gosec // bounds checked above
	}
	switch u.Scheme {
	case "http":
		return 80, nil
	case "https":
		return 443, nil
	default:
		return 0, fmt.Errorf("endpoint %q has no port and scheme %q has no default", m.Spec.Endpoint, u.Scheme)
	}
}

// ingressPolicyName is the tenant-namespace policy admitting the gateway.
func (m *MCPServer) ingressPolicyName() string { return "mcp-" + m.Name + "-allow-tool-gateway" }

// authorizationPolicyName is the tenant-namespace policy naming the caller.
func (m *MCPServer) authorizationPolicyName() string { return "mcp-" + m.Name + "-tool-gateway-only" }

// managedPolicyLabels mark a policy as generated for one MCPServer.
func (m *MCPServer) managedPolicyLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "orbit-operator",
		"orbit.io/mcp-server":          m.Name,
		"orbit.io/mcp-server-ns":       m.Namespace,
	}
}

// ingressPolicy admits the system namespace to the serving pods on the endpoint
// port, and nothing else.
func (m *MCPServer) ingressPolicy(port int32) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ingressPolicyName(),
			Namespace: m.Namespace,
			Labels:    m.managedPolicyLabels(),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: m.Spec.Workload.Selector},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"kubernetes.io/metadata.name": systemNamespace},
					},
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": mcpToolGatewayApp},
					},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{
					Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(int(port)),
				}},
			}},
		},
	}
}

// authorizationPolicy allows only the tool-gateway's principal to call the
// serving pods. Istio prepends "spiffe://" to the principal, so the value here
// omits it. Without a trust domain the policy is skipped rather than written
// with a principal that matches nothing.
func (m *MCPServer) authorizationPolicy(port int32) *unstructured.Unstructured {
	if trustDomain == "" {
		return nil
	}
	policy := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"selector": map[string]any{
				"matchLabels": toAnyMap(m.Spec.Workload.Selector),
			},
			"action": "ALLOW",
			"rules": []any{map[string]any{
				"from": []any{map[string]any{
					"source": map[string]any{
						"principals": []any{fmt.Sprintf("%s/system/%s", trustDomain, mcpToolGatewayComponent)},
					},
				}},
				"to": []any{map[string]any{
					"operation": map[string]any{
						"ports": []any{strconv.Itoa(int(port))},
					},
				}},
			}},
		},
	}}
	policy.SetGroupVersionKind(authorizationPolicyGVK)
	policy.SetName(m.authorizationPolicyName())
	policy.SetNamespace(m.Namespace)
	policy.SetLabels(m.managedPolicyLabels())
	return policy
}

func toAnyMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
