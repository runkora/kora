package agents

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/clear-route/orbit/internal/agents/manifestpatch"
	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
)

// RenderResources builds every Kubernetes resource orbit-agents would
// provision for the given agent type, applies each configured manifest
// patch, and returns the resulting typed objects in the order the runtime
// would create them. When renderSandbox is true, a sample sandbox pod is
// appended.
//
// This runs entirely in-memory. No k8s client, no database. Used by the
// orbit-agents `-render` CLI mode for pre-deploy previews and by tests that
// pin the rendered manifests against golden files.
func RenderResources(cfg K8sProvisionerConfig, at AgentType, namespace string, renderSandbox bool) ([]runtime.Object, error) {
	p := &K8sProvisioner{cfg: cfg}
	labels := agentTypeLabels(at)
	var out []runtime.Object

	sa := corev1.ServiceAccount{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: namespace, Labels: labels},
	}
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

	svc := corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: sessionManagerPort, Protocol: corev1.ProtocolTCP},
			},
		},
	}
	svc, err = manifestpatch.ApplyToResource(svc, cfg.Patches, "Service", svc.Name, svc.Labels, svc.Annotations)
	if err != nil {
		return nil, fmt.Errorf("render Service: %w", err)
	}
	out = append(out, &svc)

	cpuTarget := int32(70)
	memTarget := int32(80)
	minReplicas := int32(1)
	hpa := autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta:   metav1.TypeMeta{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler"},
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: namespace, Labels: labels},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "StatefulSet", Name: at.Slug},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
			Metrics: []autoscalingv2.MetricSpec{
				{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{
					Name:   corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &cpuTarget},
				}},
				{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{
					Name:   corev1.ResourceMemory,
					Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &memTarget},
				}},
			},
		},
	}
	hpa, err = manifestpatch.ApplyToResource(hpa, cfg.Patches, "HorizontalPodAutoscaler", hpa.Name, hpa.Labels, hpa.Annotations)
	if err != nil {
		return nil, fmt.Errorf("render HorizontalPodAutoscaler: %w", err)
	}
	out = append(out, &hpa)

	minAvailable := intstr.FromInt32(1)
	pdb := policyv1.PodDisruptionBudget{
		TypeMeta:   metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: namespace, Labels: labels},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector:     &metav1.LabelSelector{MatchLabels: labels},
		},
	}
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
