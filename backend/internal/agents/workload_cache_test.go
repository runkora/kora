package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// cacheProvisioner is a provisioner over client with unstarted bound pod and pool caches.
func cacheProvisioner(client *fake.Clientset) *K8sProvisioner {
	return &K8sProvisioner{client: client, cfg: podSecurityProvisioner().cfg, logger: logging.NewLoggerWithOutput(io.Discard),
		workloads: newWorkloadCache(client, agentTypeLabel), pool: newWorkloadCache(client, poolLabel)}
}

// cachedProvisioner is a provisioner over client whose caches have synced.
func cachedProvisioner(t *testing.T, client *fake.Clientset) *K8sProvisioner {
	t.Helper()
	p := cacheProvisioner(client)
	if err := p.SyncWorkloadCache(t.Context()); err != nil {
		t.Fatal(err)
	}
	return p
}

// waitCached waits for the workload cache to show a write the test made.
func waitCached(t *testing.T, cached func() bool) {
	t.Helper()
	err := wait.PollUntilContextTimeout(t.Context(), 10*time.Millisecond, 5*time.Second, true,
		func(context.Context) (bool, error) { return cached(), nil })
	if err != nil {
		t.Fatalf("workload cache did not catch up: %v", err)
	}
}

// boundPod is a Ready pod bound to at, created at the second created.
func boundPod(at AgentType, name string, created int) *corev1.Pod {
	labels := maps.Clone(podLabels(at))
	labels[agentConfigLabel] = agentConfigHash(at)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: scaleTestNS, Labels: labels,
			Annotations:       map[string]string{poolTemplateAnnotation: testTemplate},
			CreationTimestamp: metav1.NewTime(time.Unix(int64(created), 0)),
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
}

// The cache keeps what the scaler, placement and the bind read of a pod and the pool's template, and nothing else.
func TestTrimCached(t *testing.T) {
	now := metav1.Now()
	managed := []metav1.ManagedFieldsEntry{{Manager: "kubelet"}}
	owners := []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "rs", UID: "rs-uid"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pool-a", Namespace: scaleTestNS, UID: "u", ResourceVersion: "9", Labels: map[string]string{agentTypeLabel: "a"},
			DeletionTimestamp: &now, CreationTimestamp: now, OwnerReferences: owners, ManagedFields: managed,
			Annotations: map[string]string{"sidecar.istio.io/status": "big", poolTemplateAnnotation: testTemplate},
		},
		Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{
			{Name: sessionManagerContainerName, Image: "sm", Resources: smResources("25m", "128Mi")},
			{Name: "other", Image: "other"},
		}},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed, PodIP: "10.0.0.1", HostIP: "10.1.0.1", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: sessionManagerContainerName, Image: "sm", Resources: new(smResources("250m", "1Gi"))},
				{Name: "other", Image: "other"},
			},
		},
	}
	out, err := trimCached(pod)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(*corev1.Pod)
	if got.Name != "pool-a" || got.Namespace != scaleTestNS || got.UID != "u" || got.ResourceVersion != "9" ||
		got.Labels[agentTypeLabel] != "a" || got.DeletionTimestamp == nil || got.CreationTimestamp != now || len(got.OwnerReferences) != 1 ||
		got.Annotations[poolTemplateAnnotation] != testTemplate || !podReady(got) || got.Status.PodIP != "10.0.0.1" || got.Status.Phase != corev1.PodFailed {
		t.Errorf("trimmed pod lost a field the scaler or the bind reads: %+v", got)
	}
	if got.Spec.NodeName != "node-a" || !podWarm(got, smResources("250m", "1Gi").Requests) || !resizePending(got) {
		t.Errorf("trimmed pod lost the node or session-manager resources the grow reads: %+v", got)
	}
	if len(got.Annotations) != 1 || got.ManagedFields != nil || got.Status.HostIP != "" ||
		len(got.Spec.Containers) != 1 || got.Spec.Containers[0].Image != "" ||
		len(got.Status.ContainerStatuses) != 1 || got.Status.ContainerStatuses[0].Image != "" {
		t.Errorf("trimmed pod kept fields the scaler never reads: %+v", got)
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: poolName, Namespace: scaleTestNS, ManagedFields: managed},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{poolTemplateAnnotation: testTemplate, "other": "x"}},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: sessionManagerContainerName}}},
		}},
	}
	out, err = trimCached(dep)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(*appsv1.Deployment); got.Name != poolName || got.ManagedFields != nil || len(got.Spec.Template.Spec.Containers) != 0 ||
		len(got.Spec.Template.Annotations) != 1 || got.Spec.Template.Annotations[poolTemplateAnnotation] != testTemplate {
		t.Errorf("trimmed Deployment = %+v, want the name and pool template only", got)
	}
}

// agentWorkloads is a client holding n agents with one bound pod each, and one sandbox pod the cache must skip.
func agentWorkloads(n int) *fake.Clientset {
	objs := []runtime.Object{&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sandbox", Namespace: scaleTestNS}}}
	for i := range n {
		at := AgentType{ID: fmt.Sprintf("id-%d", i), Slug: fmt.Sprintf("agent-%d", i), TenantID: "acme"}
		objs = append(objs, boundPod(at, fmt.Sprintf("pool-%d", i), i))
	}
	return fake.NewSimpleClientset(objs...)
}

// After the cache syncs, reading every agent's pods makes no API call.
func TestAgentPodsMakesNoAPIRead(t *testing.T) {
	const agents = 50
	client := agentWorkloads(agents)
	p := cachedProvisioner(t, client)
	synced := len(client.Actions())
	for i := range agents {
		pods, err := p.AgentPods(t.Context(), fmt.Sprintf("agent-%d", i), scaleTestNS)
		if err != nil || len(pods.Live) != 1 || !pods.Live[0].Ready || pods.Live[0].Name != fmt.Sprintf("pool-%d", i) {
			t.Fatalf("agent-%d: pods %+v, err %v", i, pods, err)
		}
	}
	// The informers open their watches after the list that syncs them, so a watch may land after synced.
	var reads []k8stesting.Action
	for _, a := range client.Actions()[synced:] {
		if a.GetVerb() != "watch" {
			reads = append(reads, a)
		}
	}
	if len(reads) != 0 {
		t.Errorf("a pass over %d agents made %d API reads after the sync, want 0: %v", agents, len(reads), reads)
	}
	if _, err := p.workloads.Core().V1().Pods().Lister().Pods(scaleTestNS).Get("sandbox"); err == nil {
		t.Error("the cache holds a sandbox pod")
	}
	if pods, err := p.AgentPods(t.Context(), "missing", scaleTestNS); len(pods.Live) != 0 || err != nil {
		t.Errorf("missing agent: pods %+v, err %v; want none", pods, err)
	}
}

// Before the cache syncs, AgentPods reads the API server.
func TestAgentPodsBeforeTheCacheSyncs(t *testing.T) {
	at := AgentType{ID: "a", Slug: "coder", TenantID: "acme"}
	client := fake.NewSimpleClientset(boundPod(at, "pool-a", 1))
	pods, err := cacheProvisioner(client).AgentPods(t.Context(), at.Slug, scaleTestNS)
	if err != nil || len(pods.Live) != 1 {
		t.Fatalf("pods %+v, err %v; want pool-a", pods, err)
	}
	if actions := client.Actions(); len(actions) != 1 || actions[0].GetVerb() != "list" {
		t.Errorf("API calls = %v, want one pod list", actions)
	}
}

// LiveAgentPods reads the pods from the API server with one LIST, not from a cache that lags a terminating pod.
func TestLiveAgentPodsReadsLive(t *testing.T) {
	at := AgentType{ID: "a", Slug: "coder", TenantID: "acme"}
	now := metav1.Now()
	draining := boundPod(at, "pool-a", 1)
	draining.DeletionTimestamp = &now
	client := fake.NewSimpleClientset(draining, boundPod(at, "pool-b", 2))
	p := cacheProvisioner(client)
	// The cache holds both pods as Ready: it has not seen pool-a start terminating.
	for _, pod := range []*corev1.Pod{boundPod(at, "pool-a", 1), boundPod(at, "pool-b", 2)} {
		if err := p.workloads.Core().V1().Pods().Informer().GetIndexer().Add(pod); err != nil {
			t.Fatal(err)
		}
	}
	got, err := p.LiveAgentPods(t.Context(), at.Slug, scaleTestNS)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Live) != 1 || got.Live[0].Name != "pool-b" {
		t.Errorf("live read = %+v, want pool-b alone", got.Live)
	}
	if actions := client.Actions(); len(actions) != 1 || actions[0].GetVerb() != "list" {
		t.Errorf("API calls = %v, want one pod list", actions)
	}
}

// A cache the API server never lists for returns when ctx ends, with the sync error.
func TestSyncWorkloadCacheStopsWithContext(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	p := cacheProvisioner(client)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if err := p.SyncWorkloadCache(ctx); !errors.Is(err, errWorkloadCacheSync) {
		t.Errorf("err = %v, want errWorkloadCacheSync", err)
	}
}
