package agents

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// agentTypeLabel is on every bound session-manager pod and on no pool or sandbox pod. The bind sets it.
// A manifest patch cannot remove it (manifestpatch/protected.go).
const agentTypeLabel = "runkora.io/agent-type"

// errWorkloadCacheSync means the agent workload cache stopped before its first full list.
var errWorkloadCacheSync = errors.New("agent workload cache did not sync")

// newWorkloadCache watches the pods that carry selector in every namespace for the scaler and placement
// (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 2). kora-agents runs one cache of bound
// pods by agentTypeLabel and one of the pool Deployments and pool pods by poolLabel
// (docs/adr/2026-11-22-agent-cold-tier.md).
func newWorkloadCache(client kubernetes.Interface, selector string) informers.SharedInformerFactory {
	return informers.NewSharedInformerFactoryWithOptions(client, 0,
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.LabelSelector = selector }),
		informers.WithTransform(trimCached))
}

// trimCached drops what the scaler, placement, the bind and the grow never read: all of a Deployment but
// its identity, replicas and pool template annotation, and all of a pod but its
// identity, labels, pool template annotation, owners, creation, deletion, node, IP, phase, conditions and
// session-manager resources. It keeps the cache small at many agents.
func trimCached(obj any) (any, error) {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: o.Name, Namespace: o.Namespace, UID: o.UID, ResourceVersion: o.ResourceVersion, Labels: o.Labels},
			Spec: appsv1.DeploymentSpec{Replicas: o.Spec.Replicas, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{poolTemplateAnnotation: o.Spec.Template.Annotations[poolTemplateAnnotation]},
			}}},
		}, nil
	case *corev1.Pod:
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: o.Name, Namespace: o.Namespace, UID: o.UID, ResourceVersion: o.ResourceVersion,
				Labels: o.Labels, OwnerReferences: o.OwnerReferences,
				Annotations:       map[string]string{poolTemplateAnnotation: o.Annotations[poolTemplateAnnotation]},
				CreationTimestamp: o.CreationTimestamp, DeletionTimestamp: o.DeletionTimestamp,
			},
			Spec:   corev1.PodSpec{NodeName: o.Spec.NodeName},
			Status: corev1.PodStatus{PodIP: o.Status.PodIP, Phase: o.Status.Phase, Conditions: o.Status.Conditions},
		}
		for _, c := range o.Spec.Containers {
			if c.Name == sessionManagerContainerName {
				pod.Spec.Containers = []corev1.Container{{Name: c.Name, Resources: c.Resources}}
			}
		}
		for _, cs := range o.Status.ContainerStatuses {
			if cs.Name == sessionManagerContainerName {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: cs.Name, Resources: cs.Resources}}
			}
		}
		return pod, nil
	}
	return obj, nil
}

// workloadCacheSyncLog is how often SyncWorkloadCache logs that the cache has not synced yet.
// A list or watch the API server refuses, such as for missing RBAC, keeps the cache from syncing.
const workloadCacheSyncLog = time.Minute

// SyncWorkloadCache starts the bound pod and pool caches and waits for their first full lists until ctx ends.
func (p *K8sProvisioner) SyncWorkloadCache(ctx context.Context) error {
	p.workloads.Core().V1().Pods().Informer()
	p.pool.Core().V1().Pods().Informer()
	p.pool.Apps().V1().Deployments().Informer()
	p.workloads.Start(ctx.Done())
	p.pool.Start(ctx.Done())
	for {
		waitCtx, cancel := context.WithTimeout(ctx, workloadCacheSyncLog)
		synced := slices.Collect(maps.Values(p.workloads.WaitForCacheSync(waitCtx.Done())))
		synced = slices.AppendSeq(synced, maps.Values(p.pool.WaitForCacheSync(waitCtx.Done())))
		cancel()
		if !slices.Contains(synced, false) {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", errWorkloadCacheSync, ctx.Err())
		}
		p.logger.Error(ctx, "scaler waiting for its agent workload cache to sync",
			logging.Field{Key: "waited", Value: workloadCacheSyncLog.String()})
	}
}
