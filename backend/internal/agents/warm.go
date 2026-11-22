package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// An idle agent's session-manager runs at warm requests with its full limits, and dispatch grows it back
// before a run (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 3).

// growTimeout bounds the wait for a grow. A grow still pending after it releases the run. Tests shorten it.
var growTimeout = 5 * time.Second

// growPollInterval is how often a grow re-reads the workload cache. The reads are local.
const growPollInterval = 50 * time.Millisecond

// wakeReserveSelector selects the chart's wake-reserve pods. Deleting the one on a node frees room for a grow.
const wakeReserveSelector = "app=kora-agent-wake-reserve"

// errGrowTimeout means a warm pod's grow did not apply within growTimeout.
var errGrowTimeout = errors.New("agent pod did not grow within the bound")

// sessionManagerResources returns the session-manager container's spec resources and, when the kubelet
// reports them, its applied resources.
func sessionManagerResources(pod *corev1.Pod) (spec corev1.ResourceRequirements, applied *corev1.ResourceRequirements) {
	for _, c := range pod.Spec.Containers {
		if c.Name == sessionManagerContainerName {
			spec = c.Resources
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == sessionManagerContainerName {
			applied = cs.Resources
		}
	}
	return spec, applied
}

// podWarm reports whether the pod's session-manager memory request is below its memory limit.
func podWarm(pod *corev1.Pod) bool {
	spec, _ := sessionManagerResources(pod)
	limit, ok := spec.Limits[corev1.ResourceMemory]
	return ok && spec.Requests.Memory().Cmp(limit) < 0
}

// resizePending reports whether the session-manager's applied requests differ from its spec.
func resizePending(pod *corev1.Pod) bool {
	spec, applied := sessionManagerResources(pod)
	if applied == nil {
		return false
	}
	return spec.Requests.Memory().Cmp(*applied.Requests.Memory()) != 0 || spec.Requests.Cpu().Cmp(*applied.Requests.Cpu()) != 0
}

// resizeDeferred reports whether the kubelet holds the pod's resize until its node has room.
// An Infeasible resize does not fit the node at all, and freeing the reserve does not help it.
func resizeDeferred(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodResizePending {
			return c.Status == corev1.ConditionTrue && c.Reason == corev1.PodReasonDeferred
		}
	}
	return false
}

// cachedAgentPod returns the agent's pod at ordinal from the workload cache.
func (p *K8sProvisioner) cachedAgentPod(slug, namespace string, ordinal int) (*corev1.Pod, bool, error) {
	pod, err := p.workloads.Core().V1().Pods().Lister().Pods(namespace).Get(slug + "-" + strconv.Itoa(ordinal))
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cached pod %s/%s-%d: %w", namespace, slug, ordinal, err)
	}
	return pod, true, nil
}

// resize sets the session-manager's requests through the pod's resize subresource. The limits are unchanged.
func (p *K8sProvisioner) resize(ctx context.Context, namespace, name string, cpu, memory resource.Quantity) error {
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"containers": []any{map[string]any{
		"name":      sessionManagerContainerName,
		"resources": map[string]any{"requests": corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: memory}},
	}}}})
	if err != nil {
		return err
	}
	if _, err := p.client.CoreV1().Pods(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "resize"); err != nil {
		return fmt.Errorf("resize pod %s/%s: %w", namespace, name, err)
	}
	return nil
}

// ShrinkPod sets a hot pod's session-manager requests to the warm size.
func (p *K8sProvisioner) ShrinkPod(ctx context.Context, slug, namespace string, ordinal int) error {
	warm := p.cfg.Workloads.ForSessionManager().Warm
	return p.resize(ctx, namespace, slug+"-"+strconv.Itoa(ordinal), *warm.CPU, *warm.Memory)
}

// GrowPod sets a warm pod's session-manager requests to the full size: its memory limit and the configured CPU.
func (p *K8sProvisioner) GrowPod(ctx context.Context, slug, namespace string, ordinal int) error {
	pod, found, err := p.cachedAgentPod(slug, namespace, ordinal)
	if err != nil || !found {
		return err
	}
	return p.grow(ctx, pod)
}

func (p *K8sProvisioner) grow(ctx context.Context, pod *corev1.Pod) error {
	spec, _ := sessionManagerResources(pod)
	return p.resize(ctx, pod.Namespace, pod.Name, *p.cfg.Workloads.ForSessionManager().CPU, *spec.Limits.Memory())
}

// WakePod grows a warm pod and waits until the kubelet applies the full requests. A Deferred grow deletes the
// wake-reserve pod on the pod's node once. A pod the cache does not hold, or a hot pod, needs no grow.
// Before the cache syncs, after a kora-agents start, it reads the pod live and grows it with no wait.
func (p *K8sProvisioner) WakePod(ctx context.Context, slug, namespace string, ordinal int) error {
	if !p.workloads.Core().V1().Pods().Informer().HasSynced() {
		pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, slug+"-"+strconv.Itoa(ordinal), metav1.GetOptions{})
		if apierrors.IsNotFound(err) || (err == nil && !podWarm(pod)) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read pod %s/%s-%d: %w", namespace, slug, ordinal, err)
		}
		return p.grow(ctx, pod)
	}
	pod, found, err := p.cachedAgentPod(slug, namespace, ordinal)
	if err != nil || !found || (!podWarm(pod) && !resizePending(pod)) {
		return err
	}
	if podWarm(pod) {
		if err = p.grow(ctx, pod); err != nil {
			return err
		}
	}
	released := false
	err = wait.PollUntilContextTimeout(ctx, growPollInterval, growTimeout, true, func(ctx context.Context) (bool, error) {
		cached, found, readErr := p.cachedAgentPod(slug, namespace, ordinal)
		if readErr != nil || !found {
			return false, readErr
		}
		if !podWarm(cached) && !resizePending(cached) {
			return true, nil
		}
		if !released && resizeDeferred(cached) {
			released = true
			p.releaseWakeReserve(ctx, cached.Spec.NodeName)
		}
		return false, nil
	})
	if wait.Interrupted(err) {
		return fmt.Errorf("%w: %s/%s-%d", errGrowTimeout, namespace, slug, ordinal)
	}
	return err
}

// releaseWakeReserve deletes the wake-reserve pods on node. A failure is logged. The grow keeps waiting.
func (p *K8sProvisioner) releaseWakeReserve(ctx context.Context, node string) {
	pods := p.client.CoreV1().Pods(p.cfg.SystemNamespace)
	list, err := pods.List(ctx, metav1.ListOptions{LabelSelector: wakeReserveSelector, FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node).String()})
	if err != nil {
		p.logger.Warn(ctx, "list wake reserve failed", logging.Field{Key: "node", Value: node}, logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, pod := range list.Items {
		if err := pods.Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			p.logger.Warn(ctx, "delete wake reserve failed", logging.Field{Key: "pod", Value: pod.Name}, logging.Field{Key: "error", Value: err.Error()})
			continue
		}
		p.logger.Info(ctx, "released wake reserve for a deferred grow", logging.Field{Key: "node", Value: node})
	}
}
