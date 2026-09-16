package agents

import (
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clear-route/orbit/internal/agents/manifestpatch"
)

// WorkloadCustomization is the operator-supplied customization for one kind of
// provisioned workload, the session-manager StatefulSet or the sandbox Pod.
// It is applied to the built manifest before manifest patches, so a patch
// still wins on any field both set.
// See docs/adr/2026-09-16-first-class-workload-placement.md.
type WorkloadCustomization struct {
	NodeSelector              map[string]string                 `json:"nodeSelector,omitempty"`
	Tolerations               []corev1.Toleration               `json:"tolerations,omitempty"`
	Affinity                  *corev1.Affinity                  `json:"affinity,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	PriorityClassName         string                            `json:"priorityClassName,omitempty"`
	PodLabels                 map[string]string                 `json:"podLabels,omitempty"`
	PodAnnotations            map[string]string                 `json:"podAnnotations,omitempty"`
	// ServiceAccount annotations carry cloud identity (IRSA, Workload Identity)
	// and apply to the agent's ServiceAccount, which only session-manager pods
	// use. Sandbox pods run under the tenant's default account.
	ServiceAccount ServiceAccountCustomization `json:"serviceAccount,omitempty"`
}

// ServiceAccountCustomization customizes the ServiceAccount provisioned per
// agent type.
type ServiceAccountCustomization struct {
	Annotations map[string]string `json:"annotations,omitempty"`
}

// WorkloadsConfig is the full set of workload customizations. Common applies to
// both workloads; the per-workload entries win on a key both set.
type WorkloadsConfig struct {
	Common         CommonCustomization   `json:"common,omitempty"`
	SessionManager WorkloadCustomization `json:"sessionManager,omitempty"`
	Sandbox        WorkloadCustomization `json:"sandbox,omitempty"`
}

// CommonCustomization is the pod metadata applied to every provisioned pod.
// Placement is deliberately absent: putting both workloads on one pool is the
// default already, and a shared selector is more often a mistake than intent.
type CommonCustomization struct {
	PodLabels      map[string]string `json:"podLabels,omitempty"`
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
}

// WorkspaceConfig is the agent workspace volume, mounted by the session-manager
// and by every sandbox of that agent's runs. Empty fields keep the built
// defaults: the cluster's default StorageClass, ReadWriteOnce, 10Gi.
// Size is a *resource.Quantity so an unparseable value fails when the config
// file is decoded, naming the field, rather than at agent creation.
type WorkspaceConfig struct {
	StorageClassName string                              `json:"storageClassName,omitempty"`
	AccessModes      []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
	Size             *resource.Quantity                  `json:"size,omitempty"`
}

// reservedAnnotations are set by the builders to wire a pod into the mesh.
// Overwriting one breaks sidecar startup ordering or the gRPC port exclusion.
var reservedAnnotations = []string{
	"proxy.istio.io/config",
	"traffic.sidecar.istio.io/excludeInboundPorts",
	"traffic.sidecar.istio.io/excludeOutboundPorts",
}

// reservedLabels select a pod for SPIRE attestation, sidecar injection and its
// StatefulSet. Setting one from config would move the pod off its attested
// identity, which is why manifest patches protect the same keys.
func reservedLabels() []string {
	return append([]string{"app", "orbit.io/managed-agent"}, manifestpatch.IdentityLabels...)
}

// ForSessionManager returns the session-manager's customization with the common
// pod metadata merged underneath it.
func (w WorkloadsConfig) ForSessionManager() WorkloadCustomization {
	return w.SessionManager.withCommon(w.Common)
}

// ForSandbox returns the sandbox's customization with the common pod metadata
// merged underneath it.
func (w WorkloadsConfig) ForSandbox() WorkloadCustomization {
	return w.Sandbox.withCommon(w.Common)
}

func (w WorkloadCustomization) withCommon(common CommonCustomization) WorkloadCustomization {
	w.PodLabels = mergeMaps(common.PodLabels, w.PodLabels)
	w.PodAnnotations = mergeMaps(common.PodAnnotations, w.PodAnnotations)
	return w
}

// applyToPodMeta adds the configured labels and annotations, leaving the
// platform's own keys in place. Validation rejects a collision at startup, so
// reaching one here means config was loaded without it.
func (w WorkloadCustomization) applyToPodMeta(meta *metav1.ObjectMeta) {
	meta.Labels = mergeMaps(w.PodLabels, meta.Labels)
	meta.Annotations = mergeMaps(w.PodAnnotations, meta.Annotations)
}

// applyToPodSpec sets the scheduling fields. Each is left untouched when unset,
// so an empty customization renders exactly what the builder produced.
func (w WorkloadCustomization) applyToPodSpec(spec *corev1.PodSpec) {
	if len(w.NodeSelector) > 0 {
		spec.NodeSelector = maps.Clone(w.NodeSelector)
	}
	if len(w.Tolerations) > 0 {
		spec.Tolerations = slices.Clone(w.Tolerations)
	}
	if w.Affinity != nil {
		spec.Affinity = w.Affinity.DeepCopy()
	}
	if len(w.TopologySpreadConstraints) > 0 {
		spec.TopologySpreadConstraints = slices.Clone(w.TopologySpreadConstraints)
	}
	if w.PriorityClassName != "" {
		spec.PriorityClassName = w.PriorityClassName
	}
}

// applyToClaim sets the workspace volume's storage class, access modes and size.
func (c WorkspaceConfig) applyToClaim(claim *corev1.PersistentVolumeClaim) {
	if c.StorageClassName != "" {
		claim.Spec.StorageClassName = &c.StorageClassName
	}
	if len(c.AccessModes) > 0 {
		claim.Spec.AccessModes = slices.Clone(c.AccessModes)
	}
	if c.Size != nil {
		claim.Spec.Resources.Requests[corev1.ResourceStorage] = *c.Size
	}
}

// Validate rejects configuration that would break the pod identity or mesh
// wiring the platform owns, and a workspace size Kubernetes cannot parse.
func (w WorkloadsConfig) Validate() error {
	for name, workload := range map[string]WorkloadCustomization{
		"common":         {PodLabels: w.Common.PodLabels, PodAnnotations: w.Common.PodAnnotations},
		"sessionManager": w.SessionManager,
		"sandbox":        w.Sandbox,
	} {
		for _, label := range reservedLabels() {
			if _, taken := workload.PodLabels[label]; taken {
				return fmt.Errorf("agents.workloads.%s.podLabels sets %q, which identifies the pod to SPIRE, the sidecar injector or its StatefulSet", name, label)
			}
		}
		for _, annotation := range reservedAnnotations {
			if _, taken := workload.PodAnnotations[annotation]; taken {
				return fmt.Errorf("agents.workloads.%s.podAnnotations sets %q, which wires the pod into the mesh", name, annotation)
			}
		}
	}
	return nil
}

// mergeMaps returns base overlaid with overrides, and nil when both are empty
// so an unset field stays off the rendered manifest.
func mergeMaps(base, overrides map[string]string) map[string]string {
	if len(base) == 0 && len(overrides) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overrides))
	maps.Copy(out, base)
	maps.Copy(out, overrides)
	return out
}
