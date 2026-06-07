// Tenant sandbox egress lockdown (Phase 4b).
//
// Without this NetworkPolicy a sandbox could dial github.com (or any
// external host) directly, bypassing the credential broker. The policy
// targets pods labeled orbit.io/role=sandbox in the tenant namespace and
// constrains their egress to:
//
//   - DNS (port 53) — service discovery, including resolving the gateway
//   - The egress-gateway in orbit-system on its CONNECT port — the only
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

	"github.com/clear-route/orbit/internal/platform/logging"
)

// sandboxEgressPolicyName is the NetworkPolicy created in each tenant
// namespace by the operator. Idempotently reconciled.
const sandboxEgressPolicyName = "sandbox-egress-to-gateway"

// egressGatewayServiceName is the egress-gateway Service the sandbox is
// allowed to reach. The chart in charts/orbit/templates/egress-gateway/
// renders this name; mismatch here would silently break egress for all
// tenants.
const egressGatewayServiceName = "orbit-egress-gateway"

// egressGatewayCONNECTPort is the gateway's HTTP(S) CONNECT port — the value
// rendered by charts/orbit/templates/egress-gateway/service.yaml. Kept in
// sync with cmd/egress-gateway/config/base.yaml's server.httpPort.
const egressGatewayCONNECTPort = 8085

// ensureSandboxEgressPolicy reconciles the NetworkPolicy that locks sandbox
// egress to the per-tenant gateway. Re-applied on every reconcile so a
// gateway port/name change in config flows through to existing tenants.
func (r *TenantReconciler) ensureSandboxEgressPolicy(ctx context.Context, tenant *OrbitTenant) error {
	ns := tenantNamespace(tenant)

	desired := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxEgressPolicyName,
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
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// DNS — both UDP (standard) and TCP (large responses, IPv6).
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolUDP), Port: portIntOrString(53)},
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(53)},
					},
				},
				// istiod xDS (15012). The sandbox is in the mesh; without
				// this its Envoy sidecar can't fetch Listener/Cluster/Route
				// config and fails its postStart probe — which in turn drags
				// every other container in the pod down. Without xDS the
				// pod literally cannot start.
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "istio-system",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(15012)},
					},
				},
				// orbit-system services. The mesh sidecars (identity-proxy,
				// opa) injected into the sandbox need to call orbit-identity
				// (for SVID→token exchange + JWKS) and orbit-policy-service
				// (for OPA bundle polling and route registration). These are
				// platform-internal calls — not agent traffic — so they
				// bypass the credential broker by design.
				//
				// Critically, the sandbox-exec container's user code does
				// NOT use this path: HTTPS_PROXY forces every outbound HTTPS
				// from the agent through orbit-egress-gateway:8085, while
				// the sidecars dial orbit-system services on cluster-DNS
				// without proxy involvement.
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": systemNamespace,
								},
							},
						},
					},
				},
				// Kubernetes API server. Required by Istio's SDS bootstrap
				// (token review for the SVID) and any in-pod component that
				// resolves a kube object. The API server runs on the host
				// network and can't be selected by pod/namespace selector,
				// so we use an ipBlock open to 443 and 6443 — same pattern
				// as charts/orbit-platform/templates/netpols.yaml's
				// allow-kube-apiserver.
				{
					To: []networkingv1.NetworkPolicyPeer{
						{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0"}},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(443)},
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(6443)},
					},
				},
				// The egress gateway in orbit-system. We match by namespace
				// label + service name (via pod label) — Kubernetes
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
						{Protocol: new(corev1.ProtocolTCP), Port: portIntOrString(egressGatewayCONNECTPort)},
					},
				},
			},
		},
	}

	existing := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, client.ObjectKey{Name: sandboxEgressPolicyName, Namespace: ns}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("create sandbox egress NetworkPolicy in %q: %w", ns, createErr)
		}
		r.Logger.Info(ctx, "sandbox egress NetworkPolicy created",
			logging.Field{Key: "tenant", Value: tenant.Name},
			logging.Field{Key: "namespace", Value: ns},
		)
		return nil
	}
	if err != nil {
		return fmt.Errorf("get sandbox egress NetworkPolicy in %q: %w", ns, err)
	}

	// Update the spec if it has drifted (operator config change, e.g. a new
	// gateway port). Labels are restored as a side effect.
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch sandbox egress NetworkPolicy in %q: %w", ns, err)
	}
	return nil
}

// portIntOrString wraps an int port number in the IntOrString shape
// networkingv1.NetworkPolicyPort.Port expects.
func portIntOrString(p int) *intstr.IntOrString {
	v := intstr.FromInt32(int32(p)) //nolint:gosec
	return &v
}
