package agents

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// agentTypeLabel is on every agent StatefulSet and session-manager pod, and on no sandbox pod.
// A manifest patch cannot remove it (manifestpatch/protected.go).
const agentTypeLabel = "runkora.io/agent-type"

// errWorkloadCacheSync means the agent workload cache stopped before its first full list.
var errWorkloadCacheSync = errors.New("agent workload cache did not sync")

// newWorkloadCache watches the agent StatefulSets and session-manager pods of every namespace for
// the scaler (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 2).
func newWorkloadCache(client kubernetes.Interface) informers.SharedInformerFactory {
	return informers.NewSharedInformerFactoryWithOptions(client, 0,
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.LabelSelector = agentTypeLabel }),
		informers.WithTransform(trimCached))
}

// trimCached drops what the scaler and the grow never read: managedFields from every object, and all of a
// pod but its identity, labels, deletion, node, IP, conditions and session-manager resources. It keeps the
// cache small at many agents. The scaler reads a pod's memory at its IP (docs/adr/2026-11-22-agent-cold-tier.md).
func trimCached(obj any) (any, error) {
	switch o := obj.(type) {
	case *corev1.Pod:
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: o.Name, Namespace: o.Namespace, UID: o.UID, ResourceVersion: o.ResourceVersion,
				Labels: o.Labels, DeletionTimestamp: o.DeletionTimestamp,
			},
			Spec:   corev1.PodSpec{NodeName: o.Spec.NodeName},
			Status: corev1.PodStatus{PodIP: o.Status.PodIP, Conditions: o.Status.Conditions},
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
	case metav1.Object:
		o.SetManagedFields(nil)
	}
	return obj, nil
}

// workloadCacheSyncLog is how often SyncWorkloadCache logs that the cache has not synced yet.
// A list or watch the API server refuses, such as for missing RBAC, keeps the cache from syncing.
const workloadCacheSyncLog = time.Minute

// SyncWorkloadCache starts the agent workload cache and waits for its first full list until ctx ends.
func (p *K8sProvisioner) SyncWorkloadCache(ctx context.Context) error {
	p.workloads.Apps().V1().StatefulSets().Informer()
	p.workloads.Core().V1().Pods().Informer()
	p.workloads.Start(ctx.Done())
	for {
		waitCtx, cancel := context.WithTimeout(ctx, workloadCacheSyncLog)
		synced := p.workloads.WaitForCacheSync(waitCtx.Done())
		cancel()
		if !slices.Contains(slices.Collect(maps.Values(synced)), false) {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", errWorkloadCacheSync, ctx.Err())
		}
		p.logger.Error(ctx, "scaler waiting for its agent workload cache to sync",
			logging.Field{Key: "waited", Value: workloadCacheSyncLog.String()})
	}
}
