package operator

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// errNoTrustDomain means no trust domain is set to pin kora-identity's SPIFFE ID.
var errNoTrustDomain = errors.New("the agent identity DestinationRule needs a trust domain to pin kora-identity: set global.trustDomain")

// agentIdentityRuleName is the DestinationRule that gives each token exchange
// from a tenant pod its own connection to kora-identity.
const agentIdentityRuleName = "agent-identity-exchange"

// ensureAgentIdentityRule reconciles a DestinationRule that opens a new
// connection to kora-identity for every request from a tenant pod. A bound pool
// pod can start its first exchange before Envoy holds the agent certificate,
// and a connection opened on the pool certificate keeps that identity
// (docs/adr/2026-11-22-agent-cold-tier.md).
func (r *TenantReconciler) ensureAgentIdentityRule(ctx context.Context, tenant *KoraTenant) error {
	if trustDomain == "" {
		return errNoTrustDomain
	}
	desired := agentIdentityRule(tenantNamespace(tenant), tenant.Name)
	err := r.Create(ctx, desired)
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	// The merge patch is the whole object and leaves keys it omits in place, so
	// workloadSelector is sent as null to remove it from rules created with one.
	desired.Object["spec"].(map[string]any)["workloadSelector"] = nil
	if err := r.Patch(ctx, desired, client.Merge, client.FieldOwner("kora-operator")); err != nil {
		return fmt.Errorf("patch DestinationRule %q in %q: %w", desired.GetName(), desired.GetNamespace(), err)
	}
	return nil
}

// agentIdentityRule keeps the SAN pin of the chart's kora-identity rule. Istio
// prefers a rule in the client's namespace over the one in kora-system, so the
// rule has no workload selector: every pod in the tenant namespace, sandbox pods
// included, gets the pin.
func agentIdentityRule(ns, tenantName string) *unstructured.Unstructured {
	rule := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": agentIdentityRuleName, "namespace": ns, "labels": toAnyMap(tenantPolicyLabels(tenantName))},
		"spec": map[string]any{
			"host": fmt.Sprintf("kora-identity.%s.svc.cluster.local", systemNamespace),
			"trafficPolicy": map[string]any{
				"connectionPool": map[string]any{"http": map[string]any{"maxRequestsPerConnection": int64(1)}},
				"tls": map[string]any{
					"mode":            "ISTIO_MUTUAL",
					"subjectAltNames": []any{fmt.Sprintf("spiffe://%s/system/identity", trustDomain)},
				},
			},
		},
	}}
	rule.SetGroupVersionKind(destinationRuleGVK)
	return rule
}
