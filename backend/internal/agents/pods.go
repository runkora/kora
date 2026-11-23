package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// Every agent pod is a pool pod bound to the agent (docs/adr/2026-11-22-agent-cold-tier.md, O3). The bind
// takes a Ready pool pod out of the pool and gives it the agent identity. A bound pod serves its agent until it
// is deleted.

// Bind errors: the tenant pool has no Ready pod on its current template, or a pool pod's controller is not a
// live ReplicaSet of the tenant's pool Deployment or the pod is not on that Deployment's template.
var (
	errNoPoolPod      = errors.New("no ready pool pod to bind")
	errPoolProvenance = errors.New("pod is not controlled by the tenant pool")
)

// errNoPodIP is returned for an agent pod that has no IP.
var errNoPodIP = errors.New("agent pod has no IP")

// agentPod is what placement and the scaler read of one bound pod.
type agentPod struct {
	Name string
	// IP is set when the pod is Ready.
	IP string
	// Ready is a Ready pod with an IP. A terminating pod is never listed.
	Ready bool
	// Warm and Resizing report a session-manager below its full requests or with a pending resize.
	Warm, Resizing bool
	// Template is the pool template the pod was created from, and Config the agent config hash it was bound with.
	Template, Config string
	// DeferredSince is when the kubelet started to hold the pod's grow as Deferred, zero when it holds none.
	DeferredSince time.Time
}

// agentPods is an agent's bound pods.
type agentPods struct {
	// Live holds the pods that are not terminating and have not exited, oldest first.
	Live []agentPod
	// Finished names the pods that exited without a delete, such as a pod the kubelet evicted.
	Finished []string
	// Template is the pool's current template, empty when the cache holds no pool Deployment.
	Template string
	// MemoryLimitBytes is the configured session-manager memory limit, the scaler's fallback for memory.max.
	MemoryLimitBytes int64
}

// readyOf returns the Ready pods in pods, in order.
func readyOf(pods []agentPod) []agentPod {
	return slices.DeleteFunc(slices.Clone(pods), func(pod agentPod) bool { return !pod.Ready })
}

// rolling reports a live pod on another template than the pool's current one.
func (a agentPods) rolling(config string) bool {
	return len(a.old(config)) > 0
}

// isOld reports a pod bound with another agent config, or created from another pool template than the
// pool's current one. With no pool Deployment in the cache, the template is not compared.
func (a agentPods) isOld(pod agentPod, config string) bool {
	return pod.Config != config || (a.Template != "" && pod.Template != a.Template)
}

// current returns the live pods on the current pool template and agent config, oldest first.
func (a agentPods) current(config string) []agentPod {
	return slices.DeleteFunc(slices.Clone(a.Live), func(pod agentPod) bool { return a.isOld(pod, config) })
}

// old returns the live pods on another pool template or agent config, oldest first.
func (a agentPods) old(config string) []agentPod {
	return slices.DeleteFunc(slices.Clone(a.Live), func(pod agentPod) bool { return !a.isOld(pod, config) })
}

// agentConfigLabel carries agentConfigHash on a bound pod.
const agentConfigLabel = "runkora.io/agent-config"

// agentConfigHash is a short hash of the agent's framework and model. A pod bound with another hash is rolled,
// so a framework or model change replaces the agent's pods as the earlier restart annotation did.
func agentConfigHash(at AgentType) string {
	model := ""
	if at.ModelID != nil {
		model = *at.ModelID
	}
	sum := sha256.Sum256([]byte(string(at.Framework) + "\x00" + model))
	return hex.EncodeToString(sum[:5])
}

// readable returns the Ready pods with no pending resize. A resizing pod has no memory reading and counts as full.
func readable(pods []agentPod) []agentPod {
	return slices.DeleteFunc(slices.Clone(pods), func(pod agentPod) bool { return !pod.Ready || pod.Resizing })
}

// sortAgentPods sorts pods into an agentPods, oldest first by creation and then by name.
func (p *K8sProvisioner) sortAgentPods(pods []corev1.Pod, namespace string) agentPods {
	slices.SortFunc(pods, func(a, b corev1.Pod) int {
		if c := a.CreationTimestamp.Compare(b.CreationTimestamp.Time); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	full := p.fullRequests()
	out := agentPods{Template: p.poolTemplate(namespace), MemoryLimitBytes: full.Memory().Value()}
	for i := range pods {
		pod := &pods[i]
		switch {
		case pod.DeletionTimestamp != nil:
		case pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded:
			out.Finished = append(out.Finished, pod.Name)
		default:
			// A Ready pod with no IP yet cannot be called.
			ready := podReady(pod) && pod.Status.PodIP != ""
			view := agentPod{
				Name: pod.Name, Ready: ready, Warm: podWarm(pod, full), Resizing: resizePending(pod),
				Template: pod.Annotations[poolTemplateAnnotation], Config: pod.Labels[agentConfigLabel],
				DeferredSince: deferredSince(pod),
			}
			if ready {
				view.IP = pod.Status.PodIP
			}
			out.Live = append(out.Live, view)
		}
	}
	return out
}

// deferredSince is when the kubelet started to hold the pod's resize as Deferred, zero when it holds none.
func deferredSince(pod *corev1.Pod) time.Time {
	if !resizeDeferred(pod) {
		return time.Time{}
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodResizePending {
			return c.LastTransitionTime.Time
		}
	}
	return time.Time{}
}

// agentPodSelector selects the agent's bound pods: the bind sets app to the slug and the agent type label to
// the agent's ID. A pod bound to a deleted agent with the same slug does not match, and neither does a pod
// of the agent StatefulSet an earlier release ran, which retireStatefulSet drains.
func agentPodSelector(at AgentType) labels.Selector {
	notStatefulSet, _ := labels.NewRequirement(statefulSetPodLabel, selection.DoesNotExist, nil)
	return labels.SelectorFromSet(labels.Set{"app": at.Slug, agentTypeLabel: at.ID}).Add(*notStatefulSet)
}

// AgentPods returns the agent's bound pods from the workload cache, or from the API server before the cache
// has synced. A pod a roll has not reached still takes runs (docs/adr/2026-10-12-run-scheduling.md).
func (p *K8sProvisioner) AgentPods(ctx context.Context, at AgentType, namespace string) (agentPods, error) {
	informer := p.workloads.Core().V1().Pods()
	if !informer.Informer().HasSynced() {
		return p.LiveAgentPods(ctx, at, namespace)
	}
	cached, err := informer.Lister().Pods(namespace).List(agentPodSelector(at))
	if err != nil {
		return agentPods{}, fmt.Errorf("read cached pods of %s/%s: %w", namespace, at.Slug, err)
	}
	pods := make([]corev1.Pod, len(cached))
	for i, pod := range cached {
		pods[i] = *pod
	}
	return p.sortAgentPods(pods, namespace), nil
}

// LiveAgentPods lists the agent's bound pods from the API server. The scaler reads them before it removes a
// pod, as the cache can miss a pod that started terminating.
func (p *K8sProvisioner) LiveAgentPods(ctx context.Context, at AgentType, namespace string) (agentPods, error) {
	pods, err := p.listAgentPods(ctx, at, namespace)
	if err != nil {
		return agentPods{}, err
	}
	return p.sortAgentPods(pods, namespace), nil
}

// listAgentPods lists the agent's bound pods from the API server.
func (p *K8sProvisioner) listAgentPods(ctx context.Context, at AgentType, namespace string) ([]corev1.Pod, error) {
	list, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: agentPodSelector(at).String()})
	if err != nil {
		return nil, fmt.Errorf("list pods of %s/%s: %w", namespace, at.Slug, err)
	}
	return list.Items, nil
}

// poolTemplate is the current template of the tenant's pool Deployment in the cache, empty when it holds none.
func (p *K8sProvisioner) poolTemplate(namespace string) string {
	dep, err := p.pool.Apps().V1().Deployments().Lister().Deployments(namespace).Get(poolName)
	if err != nil {
		return ""
	}
	return dep.Spec.Template.Annotations[poolTemplateAnnotation]
}

// PodIP returns the IP of the bound pod name, Ready or draining, from the workload cache, or from the API
// server when the cache does not hold it. Session-manager calls go to the pod IP with the agent Service as
// Host header (docs/adr/2026-11-22-agent-cold-tier.md, A1).
func (p *K8sProvisioner) PodIP(ctx context.Context, namespace, name string) (string, error) {
	pod, found, err := p.cachedAgentPod(namespace, name)
	if err != nil || !found {
		if pod, err = p.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
			return "", fmt.Errorf("get pod %s/%s: %w", namespace, name, err)
		}
	}
	if pod.Status.PodIP == "" {
		return "", fmt.Errorf("%w: %s/%s", errNoPodIP, namespace, name)
	}
	return pod.Status.PodIP, nil
}

// PodLive reads the pod name from the API server and reports whether it exists and is not terminating.
// Placement reads it under the agent lock, after which the scaler cannot remove the pod.
func (p *K8sProvisioner) PodLive(ctx context.Context, namespace, name string) (bool, error) {
	pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get pod %s/%s: %w", namespace, name, err)
	}
	return pod.DeletionTimestamp == nil, nil
}

// LivePod reports whether the agent has a bound pod that is not terminating or finished, Ready or not.
// A pod still coming up, such as one waiting for a node, keeps a run queued past the start budget.
// A pod with any container, init container or sidecar included, that keeps failing to start is not coming up.
func (p *K8sProvisioner) LivePod(ctx context.Context, at AgentType, namespace string) (bool, error) {
	pods, err := p.listAgentPods(ctx, at, namespace)
	if err != nil {
		return false, err
	}
	if slices.ContainsFunc(pods, comingUp) {
		return true, nil
	}
	// Placement binds a pool pod only for an agent with no bound pod that is not terminating or finished, so a
	// stuck bound pod alone is not live.
	if slices.ContainsFunc(pods, func(pod corev1.Pod) bool { return pod.DeletionTimestamp == nil && !podFinished(pod) }) {
		return false, nil
	}
	// With no live bound pod, a pool pod coming up in the tenant is one a bind can take
	// (docs/adr/2026-11-22-agent-cold-tier.md, D5).
	pool, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: poolLabel})
	if err != nil {
		return false, fmt.Errorf("list pool pods of %s: %w", namespace, err)
	}
	return slices.ContainsFunc(pool.Items, comingUp), nil
}

// comingUp reports a pod that is not terminating or finished and has no container stuck failing to start.
func comingUp(pod corev1.Pod) bool {
	return pod.DeletionTimestamp == nil && !podFinished(pod) && !podStuck(&pod)
}

// podFinished reports a pod that exited.
func podFinished(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

// DeletePod deletes the pod name with its own grace period, which drains its runs for up to
// drainGracePeriodSecs.
func (p *K8sProvisioner) DeletePod(ctx context.Context, namespace, name string) error {
	if err := ignoreNotFound(p.client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})); err != nil {
		return fmt.Errorf("delete pod %s/%s: %w", namespace, name, err)
	}
	return nil
}

// BindPod binds a Ready pool pod on the pool's current template to the agent and grows it to full size
// (docs/adr/2026-11-22-agent-cold-tier.md, D5). It returns the pod's name. The patch carries the pool pod's
// resource version, so two replicas cannot bind one pod. A pod another replica bound first is skipped.
func (p *K8sProvisioner) BindPod(ctx context.Context, at AgentType, namespace string) (string, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.K8sProvisioner.BindPod",
		trace.WithAttributes(observability.AgentSlugKey.String(at.Slug), semconv.K8SNamespaceNameKey.String(namespace)))
	defer span.End()
	cached, err := p.pool.Core().V1().Pods().Lister().Pods(namespace).List(labels.Everything())
	if err != nil {
		return "", fmt.Errorf("read cached pool pods of %s: %w", namespace, err)
	}
	slices.SortFunc(cached, func(a, b *corev1.Pod) int { return a.CreationTimestamp.Compare(b.CreationTimestamp.Time) })
	template := p.poolTemplate(namespace)
	// A bind with no owner reference leaves the pod unowned, so a failed tenant read fails the bind.
	owner, err := p.lookupTenantOwnerRef(ctx, at.TenantID)
	if err != nil {
		observability.RecordFailure(span, err, errSpanBindPoolPod)
		return "", err
	}
	for _, pod := range cached {
		if !bindCandidate(pod, template) {
			continue
		}
		err = p.checkPoolProvenance(ctx, pod)
		if errors.Is(err, errPoolProvenance) {
			p.logger.Warn(ctx, "skipping a pool pod the tenant pool does not control or that is not on its template",
				logging.Field{Key: "pod", Value: pod.Name}, logging.Field{Key: "namespace", Value: namespace})
			continue
		}
		if err != nil {
			observability.RecordFailure(span, err, errSpanCheckPoolProvenance)
			return "", err
		}
		err = p.bind(ctx, pod, at, owner)
		switch {
		case apierrors.IsConflict(err) || apierrors.IsNotFound(err):
			// Another replica bound the pod first, or the pod is gone.
			continue
		case apierrors.IsInvalid(err) || apierrors.IsForbidden(err):
			// An admission policy refused the bind.
			observability.RecordFailure(span, err, errSpanBindRefused)
			p.metrics.recordBindRefused(ctx)
			return "", err
		case err != nil:
			observability.RecordFailure(span, err, errSpanBindPoolPod)
			return "", err
		}
		if err = p.grow(ctx, pod); err != nil {
			observability.RecordFailure(span, err, errSpanGrowBoundPod)
			return pod.Name, err
		}
		return pod.Name, nil
	}
	err = fmt.Errorf("%w in %s", errNoPoolPod, namespace)
	observability.RecordFailure(span, err, errNoPoolPod)
	return "", err
}

// bindCandidate reports a Ready pool pod that is not terminating and is on template, or on any template when
// template is empty.
func bindCandidate(pod *corev1.Pod, template string) bool {
	return pod.DeletionTimestamp == nil && podReady(pod) && (template == "" || pod.Annotations[poolTemplateAnnotation] == template)
}

// SizePool sets the tenant pool's replicas to agents.pool.size plus the cold wakes that its Ready pool pods
// do not cover (docs/adr/2026-11-22-agent-cold-tier.md, D5). waits counts the agents in the namespace with a
// pending run and no bound pod. A pool already at that size is left alone.
func (p *K8sProvisioner) SizePool(ctx context.Context, namespace string, waits int32) error {
	dep, err := p.pool.Apps().V1().Deployments().Lister().Deployments(namespace).Get(poolName)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read cached pool deployment of %s: %w", namespace, err)
	}
	cached, err := p.pool.Core().V1().Pods().Lister().Pods(namespace).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("read cached pool pods of %s: %w", namespace, err)
	}
	var ready int32
	for _, pod := range cached {
		if pod.DeletionTimestamp == nil && podReady(pod) {
			ready++
		}
	}
	want := p.cfg.PoolSize + max(waits-ready, 0)
	if dep.Spec.Replicas != nil && *dep.Spec.Replicas == want {
		return nil
	}
	deployments := p.client.AppsV1().Deployments(namespace)
	if err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, getErr := deployments.Get(ctx, poolName, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.Spec.Replicas = &want
		_, getErr = deployments.Update(ctx, existing, metav1.UpdateOptions{})
		return getErr
	}); err != nil {
		return fmt.Errorf("size pool deployment of %s: %w", namespace, err)
	}
	p.logger.Info(ctx, "sized tenant pool", logging.Field{Key: "namespace", Value: namespace},
		logging.Field{Key: "replicas", Value: want}, logging.Field{Key: "cold_waits", Value: waits})
	return nil
}

// checkPoolProvenance checks that the pod's controller is a live ReplicaSet with the same UID, that the
// ReplicaSet's controller is the tenant's pool Deployment, and that the pod is on that Deployment's template
// (docs/adr/2026-11-22-agent-cold-tier.md, D3). The Deployment comes from the pool cache, or from the API
// server when the cache holds none.
func (p *K8sProvisioner) checkPoolProvenance(ctx context.Context, pod *corev1.Pod) error {
	ref := metav1.GetControllerOf(pod)
	if ref == nil || ref.APIVersion != "apps/v1" || ref.Kind != "ReplicaSet" {
		return errPoolProvenance
	}
	rs, err := p.client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return errPoolProvenance
	}
	if err != nil {
		return fmt.Errorf("get pool replicaset %s/%s: %w", pod.Namespace, ref.Name, err)
	}
	dep, err := p.poolDeployment(ctx, pod.Namespace)
	if apierrors.IsNotFound(err) {
		return errPoolProvenance
	}
	if err != nil {
		return fmt.Errorf("get pool deployment %s/%s: %w", pod.Namespace, poolName, err)
	}
	owner := metav1.GetControllerOf(rs)
	if rs.UID != ref.UID || owner == nil || owner.APIVersion != "apps/v1" || owner.Kind != "Deployment" ||
		owner.Name != poolName || owner.UID != dep.UID ||
		pod.Annotations[poolTemplateAnnotation] != dep.Spec.Template.Annotations[poolTemplateAnnotation] {
		return errPoolProvenance
	}
	return nil
}

// poolDeployment reads the tenant's pool Deployment from the pool cache, or from the API server when the cache
// holds none.
func (p *K8sProvisioner) poolDeployment(ctx context.Context, namespace string) (*appsv1.Deployment, error) {
	dep, err := p.pool.Apps().V1().Deployments().Lister().Deployments(namespace).Get(poolName)
	if apierrors.IsNotFound(err) {
		return p.client.AppsV1().Deployments(namespace).Get(ctx, poolName, metav1.GetOptions{})
	}
	return dep, err
}

// bind patches the pool pod in one write that the bind admission policy allows: it removes the pool label,
// sets the agent identity, the agent Service selector, the agent type and the agent config hash, and replaces the ReplicaSet owner
// reference with the KoraTenant's. The pod's resource version is the patch's precondition.
func (p *K8sProvisioner) bind(ctx context.Context, pod *corev1.Pod, at AgentType, owner metav1.OwnerReference) error {
	owners := []metav1.OwnerReference{}
	if owner.Kind != "" {
		owners = append(owners, owner)
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{
		"resourceVersion": pod.ResourceVersion,
		"labels": map[string]any{
			poolLabel: nil, "spiffe.io/agent": at.Slug, "app": at.Slug, agentTypeLabel: at.ID, agentConfigLabel: agentConfigHash(at),
		},
		"ownerReferences": owners,
	}})
	if err != nil {
		return err
	}
	if _, err = p.client.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("bind pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	p.logger.Info(ctx, "bound pool pod to agent",
		logging.Field{Key: "pod", Value: pod.Name}, logging.Field{Key: "agent_type_id", Value: at.ID})
	return nil
}

// retireStatefulSet deletes the workload an earlier release ran for the agent: its StatefulSet, whose pods
// drain, its PodDisruptionBudget and HPA, and its StatefulSet workspace claims
// (docs/adr/2026-11-22-agent-cold-tier.md). RetireServiceAccount deletes its ServiceAccount once the pods are gone.
func (p *K8sProvisioner) retireStatefulSet(ctx context.Context, at AgentType, namespace string) error {
	background := metav1.DeletePropagationBackground
	return errors.Join(
		ignoreNotFound(p.client.AppsV1().StatefulSets(namespace).Delete(ctx, at.Slug, metav1.DeleteOptions{PropagationPolicy: &background})),
		ignoreNotFound(p.client.PolicyV1().PodDisruptionBudgets(namespace).Delete(ctx, at.Slug, metav1.DeleteOptions{})),
		ignoreNotFound(p.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, at.Slug, metav1.DeleteOptions{})),
		p.deleteWorkspacePVCs(ctx, at, namespace),
	)
}

// statefulSetPodLabel is the label the StatefulSet controller sets on each of its pods.
const statefulSetPodLabel = "statefulset.kubernetes.io/pod-name"

// RetireServiceAccount deletes the ServiceAccount an earlier release created for the agent once the bound pod
// cache holds no StatefulSet pod of the agent. The scaler calls it on each pass, and a done delete is not repeated.
func (p *K8sProvisioner) RetireServiceAccount(ctx context.Context, at AgentType, namespace string) error {
	key := namespace + "/" + at.Slug
	if _, done := p.retiredAccounts.Load(key); done {
		return nil
	}
	sel := labels.SelectorFromSet(labels.Set{"app": at.Slug, agentTypeLabel: at.ID})
	pods, err := p.workloads.Core().V1().Pods().Lister().Pods(namespace).List(sel)
	if err != nil {
		return fmt.Errorf("read cached pods of %s/%s: %w", namespace, at.Slug, err)
	}
	if slices.ContainsFunc(pods, func(pod *corev1.Pod) bool { return pod.Labels[statefulSetPodLabel] != "" }) {
		return nil
	}
	if err := p.deleteAgentServiceAccount(ctx, at, namespace); err != nil {
		return err
	}
	p.retiredAccounts.Store(key, struct{}{})
	return nil
}

// deleteAgentServiceAccount deletes the ServiceAccount named after the agent's slug only when it carries the
// agent's type label, which the earlier release set on it. The delete is preconditioned on the UID it read.
func (p *K8sProvisioner) deleteAgentServiceAccount(ctx context.Context, at AgentType, namespace string) error {
	sa, err := p.client.CoreV1().ServiceAccounts(namespace).Get(ctx, at.Slug, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get serviceaccount %s/%s: %w", namespace, at.Slug, err)
	}
	if sa.Labels[agentTypeLabel] != at.ID {
		return nil
	}
	opts := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &sa.UID}}
	if err := ignoreNotFound(p.client.CoreV1().ServiceAccounts(namespace).Delete(ctx, at.Slug, opts)); err != nil {
		return fmt.Errorf("delete serviceaccount %s/%s: %w", namespace, at.Slug, err)
	}
	return nil
}
