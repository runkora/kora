package operator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// Namespace labels set by TenantReconciler.ensureNamespace.
	nsLabelRole   = "orbit.io/role"
	nsLabelTenant = "orbit.io/tenant"

	// Pod labels that activate the Envoy sidecar EnvoyFilters.
	podLabelOPAPolicy     = "orbit.io/opa-policy"
	podLabelIdentityProxy = "orbit.io/identity-proxy"

	// Pod annotations consumed by the OPA sidecar and Istio injection webhook.
	podAnnotationTenantID  = "orbit.io/tenant-id"
	podAnnotationTemplates = "inject.istio.io/templates"
	requiredTemplates      = "sidecar,spire,identity-proxy,opa-policy"
)

// PodMutator injects the identity-proxy and OPA policy sidecar labels and
// annotations onto pods created in tenant namespaces (orbit.io/role=tenant).
//
// The two sidecars are a coupled pair for Layer 1 enforcement:
//   - identity-proxy injects Authorization: Bearer <platform-token> on outbound
//     requests using the pod's SPIFFE SVID
//   - OPA sidecar validates that token via Envoy ext_authz on localhost:9191
//
// Without both present, Layer 1 enforcement cannot function. The webhook
// injects both unconditionally so no per-pod opt-in is required.
//
// Failure policy on the MutatingWebhookConfiguration is Ignore — if the
// webhook is unavailable, pods start without Layer 1 sidecars. Layer 2
// (in-process OPA in owned services) continues to enforce independently.
type PodMutator struct {
	Client  client.Client
	Decoder admission.Decoder
}

// Handle is called by controller-runtime for every pod CREATE in a watched namespace.
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := m.Decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	ns := &corev1.Namespace{}
	if err := m.Client.Get(ctx, types.NamespacedName{Name: req.Namespace}, ns); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if ns.Labels[nsLabelRole] != "tenant" {
		return admission.Allowed("not a tenant namespace")
	}

	tenantName := ns.Labels[nsLabelTenant]
	if tenantName == "" {
		return admission.Allowed("namespace missing orbit.io/tenant label")
	}

	mutated := pod.DeepCopy()
	if mutated.Labels == nil {
		mutated.Labels = make(map[string]string)
	}
	if mutated.Annotations == nil {
		mutated.Annotations = make(map[string]string)
	}

	mutated.Labels[podLabelOPAPolicy] = "enabled"
	mutated.Labels[podLabelIdentityProxy] = "enabled"
	mutated.Annotations[podAnnotationTenantID] = tenantName
	mutated.Annotations[podAnnotationTemplates] = mergeIstioTemplates(mutated.Annotations[podAnnotationTemplates])

	marshaledPod, err := json.Marshal(mutated)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// mergeIstioTemplates ensures all required Istio injection templates are present
// while preserving any additional templates the pod has already declared.
func mergeIstioTemplates(existing string) string {
	if existing == "" {
		return requiredTemplates
	}

	present := make(map[string]bool)
	parts := strings.Split(existing, ",")
	for _, p := range parts {
		present[strings.TrimSpace(p)] = true
	}

	for r := range strings.SplitSeq(requiredTemplates, ",") {
		if !present[r] {
			parts = append(parts, r)
		}
	}
	return strings.Join(parts, ",")
}
