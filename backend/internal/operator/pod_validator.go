package operator

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// PodValidator rejects tenant pods that lack Istio, identity-proxy, or OPA enforcement.
type PodValidator struct {
	Client  client.Client
	Decoder admission.Decoder

	// IstioRevision is the revision tenant namespaces must be labeled for;
	// empty means the default revision (istio-injection=enabled).
	IstioRevision string
}

// Handle is called for every pod CREATE in a tenant namespace.
func (v *PodValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := v.Decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	ns := &corev1.Namespace{}
	if err := v.Client.Get(ctx, types.NamespacedName{Name: req.Namespace}, ns); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if ns.Labels[nsLabelRole] != "tenant" {
		return admission.Allowed("not a tenant namespace")
	}

	if reason := v.rejectionReason(pod, ns); reason != "" {
		return admission.Denied(reason)
	}
	return admission.Allowed("orbit enforcement attached")
}

// rejectionReason returns why the pod must not run, or "" when it may.
func (v *PodValidator) rejectionReason(pod *corev1.Pod, ns *corev1.Namespace) string {
	if !namespaceInjects(ns, v.IstioRevision) {
		return fmt.Sprintf("namespace %q is not labeled for Istio injection (%s)", ns.Name, injectionLabelWanted(v.IstioRevision))
	}
	// Istio's injector honors the label as well as the annotation.
	if pod.Annotations["sidecar.istio.io/inject"] == "false" || pod.Labels["sidecar.istio.io/inject"] == "false" {
		return "pod opts out of Istio sidecar injection (sidecar.istio.io/inject=false)"
	}
	if pod.Annotations["sidecar.istio.io/interceptionMode"] == "NONE" {
		return "pod disables Istio traffic interception (interceptionMode=NONE)"
	}
	// A container named istio-proxy proves nothing; the injector's status
	// annotation is written only when Istio itself rendered the sidecar.
	if !strings.Contains(pod.Annotations["sidecar.istio.io/status"], `"istio-proxy"`) {
		return "pod carries no sidecar.istio.io/status listing istio-proxy; Istio injection did not run"
	}
	if !hasContainer(pod, "istio-proxy") {
		return "pod has no istio-proxy sidecar; Istio injection did not run"
	}
	// An MCP server pod carries neither container by design: identity-proxy
	// would overwrite the credential it authenticates to its upstream with, and
	// OPA governs agent egress, which this pod does not perform. The mutating
	// webhook sets the label only for a pod a registered MCPServer selects, so
	// a pod cannot claim the class itself
	// (docs/adr/2026-09-17-mcp-workload-class.md).
	if pod.Labels[podLabelMCPServer] != "" {
		return ""
	}
	if !hasContainer(pod, containerIdentityProxy) {
		return "pod has no identity-proxy container; kora injection did not run"
	}
	if !hasContainer(pod, containerOPA) {
		return "pod has no opa container; kora injection did not run"
	}
	return ""
}

func namespaceInjects(ns *corev1.Namespace, revision string) bool {
	if ns.Labels["istio.io/dataplane-mode"] == "ambient" {
		return false
	}
	if revision != "" {
		return ns.Labels["istio.io/rev"] == revision
	}
	return ns.Labels["istio-injection"] == "enabled" || ns.Labels["istio.io/rev"] != ""
}

func injectionLabelWanted(revision string) string {
	if revision != "" {
		return "istio.io/rev=" + revision
	}
	return "istio-injection=enabled"
}

// hasContainer looks in containers and init containers; Istio renders the
// sidecar as a native init container on current Kubernetes.
func hasContainer(pod *corev1.Pod, name string) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name == name {
			return true
		}
	}
	for _, c := range pod.Spec.InitContainers {
		if c.Name == name {
			return true
		}
	}
	return false
}
