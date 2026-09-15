package agents

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/clear-route/orbit/internal/agents/manifestpatch"
	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
)

// RenderResources builds every Kubernetes resource orbit-agents would
// provision for the given agent type, applies each configured manifest
// patch, and returns the resulting typed objects in the order the runtime
// would create them. When renderSandbox is true, a sample sandbox pod is
// appended.
//
// Uses the same private buildX helpers the runtime ensureX functions call, so
// preview output matches the manifests actually written to the cluster (Ports,
// selectors, security contexts, env, volumes, VolumeClaimTemplates …). No k8s
// client, no database.
//
// Owner references are intentionally OMITTED: the runtime looks up the
// OrbitTenant CR's and the StatefulSet's UID from the live cluster
// (tenantOwnerRef/statefulSetOwnerRef in k8s.go) before calling applyOwnerRef,
// and RenderResources has no client to do that lookup with. A rendered object
// therefore differs from the one actually created on
// metadata.ownerReferences only; every field a patch could reach is faithful.
func RenderResources(cfg K8sProvisionerConfig, at AgentType, namespace string, renderSandbox bool) ([]runtime.Object, error) {
	p := &K8sProvisioner{cfg: cfg}
	labels := agentTypeLabels(at)
	var out []runtime.Object

	sa := p.buildServiceAccount(at.Slug, namespace, labels)
	sa.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"}
	sa, err := manifestpatch.ApplyToResource(sa, cfg.Patches, "ServiceAccount", sa.Name, sa.Labels, sa.Annotations)
	if err != nil {
		return nil, fmt.Errorf("render ServiceAccount: %w", err)
	}
	out = append(out, &sa)

	ss := p.buildSessionManagerStatefulSet(at, namespace, labels)
	ss.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"}
	ss, err = manifestpatch.ApplyToResource(ss, cfg.Patches, "StatefulSet", ss.Name, ss.Labels, ss.Annotations)
	if err != nil {
		return nil, fmt.Errorf("render StatefulSet: %w", err)
	}
	if len(ss.Spec.VolumeClaimTemplates) > 0 {
		pvc := ss.Spec.VolumeClaimTemplates[0]
		ss.Spec.VolumeClaimTemplates[0], err = manifestpatch.ApplyToResource(pvc, cfg.Patches, "PersistentVolumeClaim", pvc.Name, pvc.Labels, pvc.Annotations)
		if err != nil {
			return nil, fmt.Errorf("render PersistentVolumeClaim: %w", err)
		}
	}
	out = append(out, &ss)

	svc := p.buildHeadlessService(at.Slug, namespace, labels)
	svc.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}
	svc, err = manifestpatch.ApplyToResource(svc, cfg.Patches, "Service", svc.Name, svc.Labels, svc.Annotations)
	if err != nil {
		return nil, fmt.Errorf("render Service: %w", err)
	}
	out = append(out, &svc)

	hpa := p.buildHPA(at.Slug, namespace, labels)
	hpa.TypeMeta = metav1.TypeMeta{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler"}
	hpa, err = manifestpatch.ApplyToResource(hpa, cfg.Patches, "HorizontalPodAutoscaler", hpa.Name, hpa.Labels, hpa.Annotations)
	if err != nil {
		return nil, fmt.Errorf("render HorizontalPodAutoscaler: %w", err)
	}
	out = append(out, &hpa)

	pdb := p.buildPDB(at.Slug, namespace, labels)
	pdb.TypeMeta = metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"}
	pdb, err = manifestpatch.ApplyToResource(pdb, cfg.Patches, "PodDisruptionBudget", pdb.Name, pdb.Labels, pdb.Annotations)
	if err != nil {
		return nil, fmt.Errorf("render PodDisruptionBudget: %w", err)
	}
	out = append(out, &pdb)

	if renderSandbox {
		pod := p.buildSandboxPod(Run{ID: "sample-run"}, at.Slug, namespace, []kubeconfig.Cluster{})
		pod.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}
		podVal, err := manifestpatch.ApplyToResource(*pod, cfg.Patches, "Pod", pod.Name, pod.Labels, pod.Annotations)
		if err != nil {
			return nil, fmt.Errorf("render sandbox Pod: %w", err)
		}
		out = append(out, &podVal)
	}

	return out, nil
}
