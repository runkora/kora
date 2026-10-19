package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/runkora/kora/backend/internal/agents/manifestpatch"
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
	ServiceAccount ServiceAccountCustomization `json:"serviceAccount"`
}

// ServiceAccountCustomization customizes the ServiceAccount provisioned per
// agent type.
type ServiceAccountCustomization struct {
	Annotations map[string]string `json:"annotations,omitempty"`
}

// WorkloadsConfig is the full set of workload customizations. Common applies to
// both workloads; the per-workload entries win on a key both set.
type WorkloadsConfig struct {
	Common         CommonCustomization   `json:"common"`
	SessionManager WorkloadCustomization `json:"sessionManager"`
	Sandbox        WorkloadCustomization `json:"sandbox"`
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

// reservedAnnotations are set by the builders to wire a pod into the mesh, or
// are refused outright by the operator's pod validator. Overwriting one breaks
// sidecar startup ordering or the gRPC port exclusion; opting out of injection
// or interception makes the validator deny every provisioned pod, so it is
// rejected here rather than at admission.
var reservedAnnotations = []string{
	"proxy.istio.io/config",
	"traffic.sidecar.istio.io/excludeInboundPorts",
	"traffic.sidecar.istio.io/excludeOutboundPorts",
	"sidecar.istio.io/inject",
	"sidecar.istio.io/interceptionMode",
	serviceAccountHashAnnotation,
}

// reservedLabels select a pod for SPIRE attestation, sidecar injection and its
// StatefulSet. Setting one from config would move the pod off its attested
// identity, which is why manifest patches protect the same keys. Istio honors
// sidecar.istio.io/inject as a label too, and the operator denies pods that
// opt out.
var reservedLabels = append([]string{"app", "orbit.io/managed-agent", "sidecar.istio.io/inject"}, manifestpatch.IdentityLabels...)

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
// so an empty customization renders exactly what the builder produced. The
// chart renders every key, so "unset" includes the empty values a deployment
// that customized nothing produces.
func (w WorkloadCustomization) applyToPodSpec(spec *corev1.PodSpec) {
	if len(w.NodeSelector) > 0 {
		spec.NodeSelector = maps.Clone(w.NodeSelector)
	}
	if len(w.Tolerations) > 0 {
		spec.Tolerations = slices.Clone(w.Tolerations)
	}
	if w.Affinity != nil && *w.Affinity != (corev1.Affinity{}) {
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
		if claim.Spec.Resources.Requests == nil {
			claim.Spec.Resources.Requests = corev1.ResourceList{}
		}
		claim.Spec.Resources.Requests[corev1.ResourceStorage] = *c.Size
	}
}

// crossesNodes reports whether the workspace volume can be mounted from more
// than one node at a time.
func (c WorkspaceConfig) crossesNodes() bool {
	return slices.Contains(c.AccessModes, corev1.ReadWriteMany)
}

// requireCoLocation pins a pod to the node already running podName.
//
// A ReadWriteOnce workspace attaches to one node, and a sandbox mounts its
// session-manager's workspace, so the two have to share a node. Matching node
// selectors do not deliver that: a pool has many nodes. The term is appended,
// leaving any configured affinity in place, and required terms are ANDed.
func requireCoLocation(spec *corev1.PodSpec, podName string) {
	term := corev1.PodAffinityTerm{
		TopologyKey: "kubernetes.io/hostname",
		LabelSelector: &metav1.LabelSelector{
			// Set by Kubernetes on every StatefulSet pod, so it names the one
			// replica holding this run's workspace.
			MatchLabels: map[string]string{"statefulset.kubernetes.io/pod-name": podName},
		},
	}
	if spec.Affinity == nil {
		spec.Affinity = &corev1.Affinity{}
	}
	if spec.Affinity.PodAffinity == nil {
		spec.Affinity.PodAffinity = &corev1.PodAffinity{}
	}
	spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(
		spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, term)
}

// Validate rejects configuration that would break the pod identity or mesh
// wiring the platform owns.
func (w WorkloadsConfig) Validate() error {
	// Ordered, so a file with several problems always reports the same one.
	for _, workload := range []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
	}{
		{"common", w.Common.PodLabels, w.Common.PodAnnotations},
		{"sessionManager", w.SessionManager.PodLabels, w.SessionManager.PodAnnotations},
		{"sandbox", w.Sandbox.PodLabels, w.Sandbox.PodAnnotations},
	} {
		for _, label := range reservedLabels {
			if _, taken := workload.labels[label]; taken {
				return fmt.Errorf("agents.workloads.%s.podLabels sets %q, which identifies the pod to SPIRE, the sidecar injector or its StatefulSet", workload.name, label)
			}
		}
		for _, annotation := range reservedAnnotations {
			if _, taken := workload.annotations[annotation]; taken {
				return fmt.Errorf("agents.workloads.%s.podAnnotations sets %q, which wires the pod into the mesh", workload.name, annotation)
			}
		}
	}
	return nil
}

// serviceAccountHashAnnotation carries a digest of the agent ServiceAccount's
// annotations on the pod template.
const serviceAccountHashAnnotation = "orbit.io/service-account-hash"

// serviceAccountAnnotationHash digests the ServiceAccount annotations, empty
// when there are none.
//
// Cloud identity attaches when a pod is created: the provider's webhook reads
// the ServiceAccount then and mounts the token. Reconciling the annotation
// alone would leave running agents without it, so the digest rides on the pod
// template and a change rolls the StatefulSet.
func serviceAccountAnnotationHash(annotations map[string]string) string {
	if len(annotations) == 0 {
		return ""
	}
	sum := sha256.New()
	for _, k := range slices.Sorted(maps.Keys(annotations)) {
		fmt.Fprintf(sum, "%s=%s\n", k, annotations[k])
	}
	return hex.EncodeToString(sum.Sum(nil))[:16]
}

// cloneOrNil copies m, returning nil when it is empty so an unset field stays
// off the rendered manifest. The chart renders every key, so "unset" arrives
// here as an empty map rather than a missing one.
func cloneOrNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return maps.Clone(m)
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
