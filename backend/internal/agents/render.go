package agents

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/runkora/kora/backend/internal/agents/manifestpatch"
	sandboxfolder "github.com/runkora/kora/backend/internal/agents/sandbox/folder"
	"github.com/runkora/kora/backend/internal/agents/sandbox/kubeconfig"
)

// RenderResources builds the patchable resources kora-agents provisions for
// an agent type (its Service, the tenant pool's ServiceAccount and Deployment,
// and optionally a sample sandbox Pod), applies the manifest patches, and
// returns them in creation order. The pool pod template is the session-manager
// StatefulSet render with the StatefulSet and PersistentVolumeClaim patches
// applied (docs/adr/2026-11-22-agent-cold-tier.md). The Istio DestinationRule
// and AuthorizationPolicy are not patchable and are not rendered.
//
// It calls the same build helpers as the runtime ensure functions and needs
// no cluster or database. Owner references are omitted: the runtime resolves
// them from live KoraTenant and pod lookups.
func RenderResources(cfg K8sProvisionerConfig, at AgentType, namespace string, renderSandbox bool) ([]runtime.Object, error) {
	p := &K8sProvisioner{cfg: cfg}
	labels := agentTypeLabels(at)
	var out []runtime.Object

	svc, err := manifestpatch.Apply(p.buildHeadlessService(at.Slug, namespace, labels), "Service", cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("render Service: %w", err)
	}
	out = append(out, &svc)

	pool, err := p.buildTenantPool(at.TenantID, namespace)
	if err != nil {
		return nil, err
	}
	sa, err := manifestpatch.Apply(p.buildServiceAccount(poolName, namespace, pool.Labels), "ServiceAccount", cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("render ServiceAccount: %w", err)
	}
	out = append(out, &sa, &pool)

	if renderSandbox {
		pod, err := p.sampleSandboxPod("sample-run", at.Slug, namespace)
		if err != nil {
			return nil, err
		}
		if err := checkSplitPoolWorkspace(pool, pod); err != nil {
			return nil, err
		}
		out = append(out, &pod)
	}

	return out, nil
}

// applyStatefulSetPatches applies the manifest patches to a built StatefulSet
// and to its workspace claim template, which patches target as a
// PersistentVolumeClaim of its own.
func (p *K8sProvisioner) applyStatefulSetPatches(ss appsv1.StatefulSet) (appsv1.StatefulSet, error) {
	ss, err := manifestpatch.Apply(ss, "StatefulSet", p.cfg.Patches)
	if err != nil {
		return ss, fmt.Errorf("apply StatefulSet patches: %w", err)
	}
	if len(ss.Spec.VolumeClaimTemplates) > 0 {
		ss.Spec.VolumeClaimTemplates[0], err = manifestpatch.Apply(ss.Spec.VolumeClaimTemplates[0], "PersistentVolumeClaim", p.cfg.Patches)
		if err != nil {
			return ss, fmt.Errorf("apply PersistentVolumeClaim patches: %w", err)
		}
	}
	return ss, nil
}

// sampleSandboxPod renders a stand-in sandbox Pod for preview and validation.
//
// Not for provisioning: it claims the workspace of a stand-in pool pod, has
// no granted Kubernetes clusters, so it has no kubeconfig, no owner or node
// affinity, which ProvisionSandbox reads from the live session-manager pod, and
// a placeholder exec token, since nothing will call it. ProvisionSandbox builds
// the real pod from the run.
func (p *K8sProvisioner) sampleSandboxPod(runID, agentTypeSlug, namespace string) (corev1.Pod, error) {
	spec := SandboxSpec{Environment: DefaultEnvironmentName, Image: p.cfg.SandboxImage, Size: p.cfg.DefaultEnvironmentSize, KubeClusters: []kubeconfig.Cluster{}}
	built, err := p.buildSandboxPod(Run{ID: runID, PodName: poolName + "-sample"}, agentTypeSlug, namespace, spec, "sample-not-a-real-token")
	if err != nil {
		return corev1.Pod{}, err
	}
	pod, err := manifestpatch.Apply(*built, "Pod", p.cfg.Patches)
	if err != nil {
		return pod, fmt.Errorf("apply Pod patches: %w", err)
	}
	return pod, nil
}

// checkSandboxPlacement runs the split-pool workspace check for a real agent
// type and its tenant's pool, applying the patches exactly as the ensure functions do.
//
// Only patches need this. Workload values apply to every agent type, so the
// startup validation already renders them; a patch can be selected by one
// tenant's labels or one agent's sandbox labels, which the stand-in there never matches.
func (p *K8sProvisioner) checkSandboxPlacement(at AgentType, namespace string) error {
	if len(p.cfg.Patches) == 0 {
		return nil
	}

	pool, err := p.buildTenantPool(at.TenantID, namespace)
	if err != nil {
		return err
	}
	pod, err := p.sampleSandboxPod("placement-check", at.Slug, namespace)
	if err != nil {
		return err
	}
	return checkSplitPoolWorkspace(pool, pod)
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
func checkSplitPoolWorkspace(pool appsv1.Deployment, pod corev1.Pod) error {
	split, reason := splitAcrossNodes(pool.Spec.Template.Spec, pod.Spec)
	if !split {
		return nil
	}
	i := slices.IndexFunc(pool.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
		return v.Name == sandboxfolder.WorkspaceVolume && v.Ephemeral != nil && v.Ephemeral.VolumeClaimTemplate != nil
	})
	if i < 0 {
		return nil
	}
	modes := pool.Spec.Template.Spec.Volumes[i].Ephemeral.VolumeClaimTemplate.Spec.AccessModes
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
// Both sides must be pinned for the answer to be conclusive: where one is
// unconstrained the scheduler may still place both pods on one node. Pinning
// is read across both forms together, because a nodeSelector on one side and
// required node affinity on the other separates the pods just as surely as two
// different selectors. Identical pinning on both sides is the same pool.
//
// Preferred affinity is advisory and pod anti-affinity separates pods without
// naming a pool, so neither is inspected.
func splitAcrossNodes(agent, sandbox corev1.PodSpec) (bool, string) {
	agentNodes, sandboxNodes := requiredNodeAffinity(agent), requiredNodeAffinity(sandbox)
	agentPinned := len(agent.NodeSelector) > 0 || agentNodes != nil
	sandboxPinned := len(sandbox.NodeSelector) > 0 || sandboxNodes != nil
	if !agentPinned || !sandboxPinned {
		return false, ""
	}
	if maps.Equal(agent.NodeSelector, sandbox.NodeSelector) && reflect.DeepEqual(agentNodes, sandboxNodes) {
		return false, ""
	}
	if !maps.Equal(agent.NodeSelector, sandbox.NodeSelector) {
		return true, fmt.Sprintf("sandbox nodeSelector %v, session-manager nodeSelector %v", sandbox.NodeSelector, agent.NodeSelector)
	}
	return true, "their required node affinities select different nodes"
}

func requiredNodeAffinity(spec corev1.PodSpec) *corev1.NodeSelector {
	if spec.Affinity == nil || spec.Affinity.NodeAffinity == nil {
		return nil
	}
	return spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
}
