package agents

import (
	"fmt"

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
		out = append(out, &pod)
	}

	return out, nil
}
