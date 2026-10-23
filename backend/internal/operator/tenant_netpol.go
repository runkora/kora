// Tenant sandbox egress lockdown (Phase 4b).
//
// Without this NetworkPolicy a sandbox could dial github.com (or any
// external host) directly, bypassing the credential broker. The policy
// targets pods labeled orbit.io/role=sandbox in the tenant namespace and
// constrains their egress to:
//
//   - DNS (port 53): service discovery, including resolving the gateway
//   - The egress-gateway in orbit-system on its CONNECT port: the only
//     governed egress path
//
// All other egress is denied. The default-deny posture is implicit: a
// NetworkPolicy that selects a pod and declares `policyTypes: [Egress]`
// without matching a destination drops the traffic. Combining this with the
// "allow gateway + DNS" rules below produces the exact deny-everything-else
// shape we want.
package operator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/ports"
)

// sandboxEgressPolicyName is the NetworkPolicy created in each tenant
// namespace by the operator. Idempotently reconciled.
const sandboxEgressPolicyName = "sandbox-egress-to-gateway"

// sandboxIngressPolicyName locks inbound access to a sandbox's exec port.
const sandboxIngressPolicyName = "sandbox-ingress-from-agents"

// egressGatewayServiceName is the egress-gateway Service the sandbox is
// allowed to reach. The chart in charts/orbit/templates/egress-gateway/
// renders this name; mismatch here would silently break egress for all
// tenants.
const egressGatewayServiceName = "orbit-egress-gateway"

// ensureSandboxEgressPolicy reconciles the NetworkPolicy that locks sandbox
// egress to the per-tenant gateway. Re-applied on every reconcile so a
// gateway port/name change in config flows through to existing tenants.
func (r *TenantReconciler) ensureSandboxEgressPolicy(ctx context.Context, tenant *KoraTenant) error {
	return r.ensureNetworkPolicy(ctx, tenant, sandboxEgressPolicy(tenantNamespace(tenant), tenant.Name))
}

// sandboxEgressPolicy is the sandbox egress allowlist: DNS, istiod, identity, policy and the egress gateway.
func sandboxEgressPolicy(ns, tenantName string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxEgressPolicyName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "orbit-operator",
				"orbit.io/tenant":              tenantName,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"orbit.io/role": "sandbox",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// DNS: to CoreDNS only (kube-system, k8s-app=kube-dns), NOT
				// 53-to-anywhere. An open 53 lets a hostile agent tunnel data
				// out via an arbitrary upstream resolver; restricting to the
				// in-cluster resolver removes that path (residual: CoreDNS ->
				// upstream is still possible, a narrower, accepted risk).
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"k8s-app": "kube-dns"},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolUDP), Port: portIntOrString(53)},
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(53)},
					},
				},
				// istiod xDS (15012). The sandbox is in the mesh, so its
				// Envoy sidecar needs xDS to fetch Listener/Cluster/Route
				// config. Without it the sidecar never becomes ready and the
				// pod cannot start.
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": istioNamespace,
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(15012)},
					},
				},
				// orbit-identity and orbit-policy-service, on their own ports
				// only: the mesh sidecars (identity-proxy for SVID→token
				// exchange and JWKS, opa for bundle polling and route
				// registration) call these platform-internal endpoints
				// directly, bypassing the credential broker by design. Not
				// agent traffic.
				//
				// Critically, the sandbox-exec container's user code does
				// NOT reach orbit-system this way: the sandbox Envoy
				// transparently rewrites the agent's external egress into a
				// CONNECT to orbit-egress-gateway:8085 (see
				// charts/orbit/templates/egress-gateway/transparent-egress.yaml),
				// while the sidecars dial orbit-system services on cluster-DNS.
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": systemNamespace,
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "orbit-identity"},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(8080)},
					},
				},
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": systemNamespace,
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "orbit-policy-service"},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(8083)},
					},
				},
				// NOTE: there is deliberately NO kube-API egress rule here. A
				// sandbox does not call the API server: the Istio sidecar gets
				// its config from istiod (15012, above) and its SVID from the
				// SPIRE CSI socket, and TokenReview for the SVID happens at
				// istiod, not in the pod. An earlier `ipBlock 0.0.0.0/0` on
				// 443/6443 (added to reach the host-networked API server)
				// reopened the entire internet on 443 and let tools bypass the
				// egress gateway entirely, defeating the chokepoint. If a
				// future component genuinely needs the API server, scope it to
				// the resolved `kubernetes` Service ClusterIP (/32 on 443) and
				// Endpoints (/32 on 6443), never 0.0.0.0/0.
				//
				// The egress gateway in orbit-system. We match by namespace
				// label + service name (via pod label): Kubernetes
				// NetworkPolicy does not have a native "Service" matcher,
				// but the gateway's pods carry an app=orbit-egress-gateway
				// label set by its chart so this selects them.
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": systemNamespace,
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": egressGatewayServiceName,
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(ports.EgressGatewayCONNECT)},
					},
				},
			},
		},
	}
}

// ensureSandboxIngressPolicy reconciles the NetworkPolicy that decides who may
// reach a sandbox.
//
// The exec port is excluded from the inbound mesh, so no AuthorizationPolicy
// covers it and sandbox-exec's per-run token is the only check on the receiving
// side. This narrows who can present a token to the tenant's own agent pods.
func (r *TenantReconciler) ensureSandboxIngressPolicy(ctx context.Context, tenant *KoraTenant) error {
	ns := tenantNamespace(tenant)

	desired := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxIngressPolicyName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "orbit-operator",
				"orbit.io/tenant":              tenant.Name,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"orbit.io/role": "sandbox",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// This tenant's agent pods, on the exec port: kora-shell and the
				// egress canary run there and are the only callers. No
				// NamespaceSelector, which scopes the peer to this namespace.
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"orbit.io/managed-agent": "true"},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(ports.SandboxExecGRPC)},
					},
				},
			},
		},
	}

	return r.ensureNetworkPolicy(ctx, tenant, desired)
}

// ensureNetworkPolicy creates the policy, or patches an existing one back onto
// the desired spec so config changes reach existing tenants.
func (r *TenantReconciler) ensureNetworkPolicy(ctx context.Context, tenant *KoraTenant, desired *networkingv1.NetworkPolicy) error {
	ns := desired.Namespace

	existing := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: ns}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("create NetworkPolicy %q in %q: %w", desired.Name, ns, createErr)
		}
		r.Logger.Info(ctx, "sandbox NetworkPolicy created",
			logging.Field{Key: "policy", Value: desired.Name},
			logging.Field{Key: "tenant", Value: tenant.Name},
			logging.Field{Key: "namespace", Value: ns},
		)
		return nil
	}
	if err != nil {
		return fmt.Errorf("get NetworkPolicy %q in %q: %w", desired.Name, ns, err)
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch NetworkPolicy %q in %q: %w", desired.Name, ns, err)
	}
	return nil
}

// portIntOrString wraps an int port number in the IntOrString shape
// networkingv1.NetworkPolicyPort.Port expects.
func portIntOrString(p int) *intstr.IntOrString {
	v := intstr.FromInt32(int32(p)) //nolint:gosec
	return &v
}
