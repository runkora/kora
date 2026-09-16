package agents

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/clear-route/orbit/internal/agents/manifestpatch"
	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
)

// RenderResources builds the patchable resources orbit-agents provisions for
// an agent type (ServiceAccount, StatefulSet with its PVC template, Service,
// HPA, PDB, and optionally a sample sandbox Pod), applies the manifest
// patches, and returns them in creation order. The Istio DestinationRule and
// AuthorizationPolicy are not patchable and are not rendered.
//
// It calls the same build helpers as the runtime ensure functions and needs
// no cluster or database. Owner references are omitted: the runtime resolves
// them from live OrbitTenant and StatefulSet lookups.
func RenderResources(cfg K8sProvisionerConfig, at AgentType, namespace string, renderSandbox bool) ([]runtime.Object, error) {
	p := &K8sProvisioner{cfg: cfg}
	labels := agentTypeLabels(at)
	var out []runtime.Object

	sa, err := manifestpatch.Apply(p.buildServiceAccount(at.Slug, namespace, labels), "ServiceAccount", cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("render ServiceAccount: %w", err)
	}
	out = append(out, &sa)

	ss, err := manifestpatch.Apply(p.buildSessionManagerStatefulSet(at, namespace, labels), "StatefulSet", cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("render StatefulSet: %w", err)
	}
	if len(ss.Spec.VolumeClaimTemplates) > 0 {
		ss.Spec.VolumeClaimTemplates[0], err = manifestpatch.Apply(ss.Spec.VolumeClaimTemplates[0], "PersistentVolumeClaim", cfg.Patches)
		if err != nil {
			return nil, fmt.Errorf("render PersistentVolumeClaim: %w", err)
		}
	}
	out = append(out, &ss)

	svc, err := manifestpatch.Apply(p.buildHeadlessService(at.Slug, namespace, labels), "Service", cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("render Service: %w", err)
	}
	out = append(out, &svc)

	hpa, err := manifestpatch.Apply(p.buildHPA(at.Slug, namespace, labels), "HorizontalPodAutoscaler", cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("render HorizontalPodAutoscaler: %w", err)
	}
	out = append(out, &hpa)

	pdb, err := manifestpatch.Apply(p.buildPDB(at.Slug, namespace, labels), "PodDisruptionBudget", cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("render PodDisruptionBudget: %w", err)
	}
	out = append(out, &pdb)

	if renderSandbox {
		pod, err := manifestpatch.Apply(*p.buildSandboxPod(Run{ID: "sample-run"}, at.Slug, namespace, []kubeconfig.Cluster{}), "Pod", cfg.Patches)
		if err != nil {
			return nil, fmt.Errorf("render sandbox Pod: %w", err)
		}
		if err := checkSplitPoolWorkspace(ss, pod); err != nil {
			return nil, err
		}
		out = append(out, &pod)
	}

	return out, nil
}

// checkSandboxPlacement runs the split-pool workspace check for a real agent
// type, applying the patches exactly as the ensure functions do.
//
// RenderResources covers the same ground at startup, but it renders a stand-in
// agent type, so a patch selected by an agent's own name or labels matches
// nothing there. Running it again per agent type catches those before anything
// is created, rather than on that agent's first run.
func (p *K8sProvisioner) checkSandboxPlacement(at AgentType, namespace string) error {
	labels := agentTypeLabels(at)

	ss, err := manifestpatch.Apply(p.buildSessionManagerStatefulSet(at, namespace, labels), "StatefulSet", p.cfg.Patches)
	if err != nil {
		return fmt.Errorf("apply StatefulSet patches: %w", err)
	}
	if len(ss.Spec.VolumeClaimTemplates) > 0 {
		ss.Spec.VolumeClaimTemplates[0], err = manifestpatch.Apply(ss.Spec.VolumeClaimTemplates[0], "PersistentVolumeClaim", p.cfg.Patches)
		if err != nil {
			return fmt.Errorf("apply PersistentVolumeClaim patches: %w", err)
		}
	}

	pod, err := manifestpatch.Apply(*p.buildSandboxPod(Run{ID: "placement-check"}, at.Slug, namespace, []kubeconfig.Cluster{}), "Pod", p.cfg.Patches)
	if err != nil {
		return fmt.Errorf("apply Pod patches: %w", err)
	}

	return checkSplitPoolWorkspace(ss, pod)
}

// checkSplitPoolWorkspace rejects patches that pin a sandbox and its
// session-manager to different node pools while leaving the workspace claim
// ReadWriteOnce. A sandbox mounts its session-manager's claim, so from another
// node ReadWriteOnce fails at attach time with a Multi-Attach error. Without
// this the patches apply cleanly and the deployment looks healthy until the
// first run asks for a sandbox.
//
// Both arguments are post-patch: callers apply every patch, the
// PersistentVolumeClaim one included, before calling. Checking the built
// objects instead would miss the placement and storage class the patches set,
// which is the whole subject of the check.
func checkSplitPoolWorkspace(ss appsv1.StatefulSet, pod corev1.Pod) error {
	split, reason := splitAcrossNodes(ss.Spec.Template.Spec, pod.Spec)
	if !split {
		return nil
	}
	if len(ss.Spec.VolumeClaimTemplates) == 0 {
		return nil
	}
	modes := ss.Spec.VolumeClaimTemplates[0].Spec.AccessModes
	if slices.Contains(modes, corev1.ReadWriteMany) {
		return nil
	}
	return fmt.Errorf(
		"sandbox pods and their session-manager are scheduled on different nodes (%s), but the workspace volume is %v: "+
			"a sandbox mounts its session-manager's workspace, which needs a ReadWriteMany storage class to cross nodes. "+
			"Patch the PersistentVolumeClaim kind with a ReadWriteMany class, or schedule both on the same nodes",
		reason, modes)
}

// splitAcrossNodes reports whether two pod specs are pinned to node pools that
// cannot overlap, and why.
//
// Only constraints present on both sides are conclusive. Where one side is
// unconstrained the scheduler may still place both pods on one node, so that
// combination is treated as unsplit. Preferred affinity is advisory and pod
// anti-affinity separates pods without naming a pool, so neither is inspected.
func splitAcrossNodes(agent, sandbox corev1.PodSpec) (bool, string) {
	if len(agent.NodeSelector) > 0 && len(sandbox.NodeSelector) > 0 && !maps.Equal(agent.NodeSelector, sandbox.NodeSelector) {
		return true, fmt.Sprintf("sandbox nodeSelector %v, session-manager nodeSelector %v", sandbox.NodeSelector, agent.NodeSelector)
	}
	agentNodes, sandboxNodes := requiredNodeAffinity(agent), requiredNodeAffinity(sandbox)
	if agentNodes != nil && sandboxNodes != nil && !reflect.DeepEqual(agentNodes, sandboxNodes) {
		return true, "their required node affinities select different nodes"
	}
	return false, ""
}

func requiredNodeAffinity(spec corev1.PodSpec) *corev1.NodeSelector {
	if spec.Affinity == nil || spec.Affinity.NodeAffinity == nil {
		return nil
	}
	return spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
}
