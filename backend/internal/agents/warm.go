package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// An idle agent's session-manager runs at warm requests with its full limits, and dispatch grows it back
// before a run (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 3).

// defaultGrowTimeout bounds the wait for a grow. A grow still pending after it releases the run.
const defaultGrowTimeout = 5 * time.Second

// shrinkSettle bounds the scaler's wait for its own cache to show a shrink before it releases the agent lock.
const shrinkSettle = time.Second

// growPollInterval is how often a grow re-reads the workload cache. The reads are local.
const growPollInterval = 50 * time.Millisecond

// wakeReserveSelector selects the chart's wake-reserve pods. Deleting the one on a node frees room for a grow.
const wakeReserveSelector = "app=kora-agent-wake-reserve"

// errGrowTimeout means a warm pod's grow did not apply within the grow timeout.
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

// templateRequests returns the session-manager requests of the StatefulSet's pod template.
func templateRequests(ss *appsv1.StatefulSet) corev1.ResourceList {
	for _, c := range ss.Spec.Template.Spec.Containers {
		if c.Name == sessionManagerContainerName {
			return c.Resources.Requests
		}
	}
	return nil
}

// fullRequests is the pod's full session-manager CPU and memory request: the template's, capped by the
// pod's own limits. The cap keeps a pod a roll has not reached within its older limits.
func fullRequests(pod *corev1.Pod, template corev1.ResourceList) corev1.ResourceList {
	spec, _ := sessionManagerResources(pod)
	full := corev1.ResourceList{}
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		q, ok := template[name]
		if !ok {
			continue
		}
		if limit, ok := spec.Limits[name]; ok && limit.Cmp(q) < 0 {
			q = limit
		}
		full[name] = q
	}
	return full
}

// podWarm reports whether the pod's session-manager memory request is below its full request.
func podWarm(pod *corev1.Pod, template corev1.ResourceList) bool {
	spec, _ := sessionManagerResources(pod)
	full, ok := fullRequests(pod, template)[corev1.ResourceMemory]
	return ok && spec.Requests.Memory().Cmp(full) < 0
}

// resizeRefused reports whether the API server refused a resize for good: no resize subresource, no
// permission, or a request the pod cannot take. A retry on the next pass would fail the same way.
// A not-found that names the pod means the pod is gone, and its replacement may resize.
func resizeRefused(err error) bool {
	var status apierrors.APIStatus
	if apierrors.IsNotFound(err) && errors.As(err, &status) {
		details := status.Status().Details
		return details == nil || details.Name == ""
	}
	return apierrors.IsForbidden(err) || apierrors.IsMethodNotSupported(err) || apierrors.IsInvalid(err)
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

// cachedAgentPod returns the agent's pod at ordinal and its StatefulSet's session-manager template requests
// from the workload cache.
func (p *K8sProvisioner) cachedAgentPod(slug, namespace string, ordinal int) (*corev1.Pod, corev1.ResourceList, bool, error) {
	ss, err := p.workloads.Apps().V1().StatefulSets().Lister().StatefulSets(namespace).Get(slug)
	if apierrors.IsNotFound(err) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("read cached statefulset %s/%s: %w", namespace, slug, err)
	}
	pod, err := p.workloads.Core().V1().Pods().Lister().Pods(namespace).Get(slug + "-" + strconv.Itoa(ordinal))
	if apierrors.IsNotFound(err) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("read cached pod %s/%s-%d: %w", namespace, slug, ordinal, err)
	}
	return pod, templateRequests(ss), true, nil
}

// resize sets the session-manager's requests through the pod's resize subresource. The limits are unchanged.
func (p *K8sProvisioner) resize(ctx context.Context, namespace, name string, requests corev1.ResourceList) error {
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"containers": []any{map[string]any{
		"name":      sessionManagerContainerName,
		"resources": map[string]any{"requests": requests},
	}}}})
	if err != nil {
		return err
	}
	if _, err := p.client.CoreV1().Pods(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "resize"); err != nil {
		return fmt.Errorf("resize pod %s/%s: %w", namespace, name, err)
	}
	return nil
}

// ShrinkPod sets a hot pod's session-manager requests to the warm size, never above its full requests, and
// waits up to shrinkSettle for the cache to show it. A dispatch on this replica then sees the pod warm.
func (p *K8sProvisioner) ShrinkPod(ctx context.Context, slug, namespace string, ordinal int) error {
	pod, template, found, err := p.cachedAgentPod(slug, namespace, ordinal)
	if err != nil || !found {
		return err
	}
	warm := p.cfg.Workloads.ForSessionManager().Warm
	full := fullRequests(pod, template)
	requests := corev1.ResourceList{corev1.ResourceCPU: *warm.CPU, corev1.ResourceMemory: *warm.Memory}
	for name, q := range full {
		if q.Cmp(requests[name]) < 0 {
			requests[name] = q
		}
	}
	if fullMemory, ok := full[corev1.ResourceMemory]; !ok || requests.Memory().Cmp(fullMemory) >= 0 {
		return nil // The full memory request is already at or below the warm size.
	}
	if err := p.resize(ctx, namespace, pod.Name, requests); err != nil {
		return err
	}
	_ = wait.PollUntilContextTimeout(ctx, growPollInterval, shrinkSettle, true, func(context.Context) (bool, error) {
		cached, template, found, readErr := p.cachedAgentPod(slug, namespace, ordinal)
		return readErr != nil || !found || podWarm(cached, template), nil
	})
	return nil
}

// GrowPod sets a warm pod's session-manager requests to its full requests with no wait. A grow the kubelet
// holds as Deferred frees the wake reserve on the pod's node.
func (p *K8sProvisioner) GrowPod(ctx context.Context, slug, namespace string, ordinal int) error {
	pod, template, found, err := p.cachedAgentPod(slug, namespace, ordinal)
	if err != nil || !found {
		return err
	}
	if resizeDeferred(pod) {
		p.releaseWakeReserve(ctx, pod.Spec.NodeName)
	}
	if !podWarm(pod, template) {
		return nil
	}
	return p.resize(ctx, pod.Namespace, pod.Name, fullRequests(pod, template))
}

// WakePod grows a warm pod and waits until the kubelet applies the full requests. live reports whether
// placement's pod list showed the pod warm. It covers a cache that has not seen the shrink yet. A Deferred
// grow deletes the wake-reserve pod on the pod's node once. Before the cache syncs, after a kora-agents
// start, it reads the pod live and grows it with no wait.
func (p *K8sProvisioner) WakePod(ctx context.Context, slug, namespace string, ordinal int, live bool) error {
	if !p.workloads.Core().V1().Pods().Informer().HasSynced() || !p.workloads.Apps().V1().StatefulSets().Informer().HasSynced() {
		return p.wakeUncached(ctx, slug, namespace, ordinal)
	}
	pod, template, found, err := p.cachedAgentPod(slug, namespace, ordinal)
	if err != nil || !found {
		return err
	}
	switch {
	case podWarm(pod, template):
		err = p.resize(ctx, pod.Namespace, pod.Name, fullRequests(pod, template))
	case live:
		err = p.wakeUncached(ctx, slug, namespace, ordinal)
	case !resizePending(pod):
		return nil
	}
	if err != nil {
		return err
	}
	return p.waitGrown(ctx, slug, namespace, ordinal)
}

// wakeUncached grows a warm pod read from the API server, with no wait.
func (p *K8sProvisioner) wakeUncached(ctx context.Context, slug, namespace string, ordinal int) error {
	ss, err := p.client.AppsV1().StatefulSets(namespace).Get(ctx, slug, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read statefulset %s/%s: %w", namespace, slug, err)
	}
	pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, slug+"-"+strconv.Itoa(ordinal), metav1.GetOptions{})
	if apierrors.IsNotFound(err) || (err == nil && !podWarm(pod, templateRequests(ss))) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pod %s/%s-%d: %w", namespace, slug, ordinal, err)
	}
	return p.resize(ctx, pod.Namespace, pod.Name, fullRequests(pod, templateRequests(ss)))
}

// waitGrown waits on the cache up to the grow timeout for the pod's applied requests to match its full spec.
func (p *K8sProvisioner) waitGrown(ctx context.Context, slug, namespace string, ordinal int) error {
	timeout := p.growTimeout
	if timeout <= 0 {
		timeout = defaultGrowTimeout
	}
	released := false
	err := wait.PollUntilContextTimeout(ctx, growPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		cached, template, found, readErr := p.cachedAgentPod(slug, namespace, ordinal)
		if readErr != nil || !found {
			return false, readErr
		}
		if !podWarm(cached, template) && !resizePending(cached) {
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
	if node == "" {
		return
	}
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
