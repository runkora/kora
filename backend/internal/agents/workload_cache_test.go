package agents

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/flowcontrol"
)

// cachedProvisioner is a provisioner over client whose workload cache has synced.
func cachedProvisioner(t *testing.T, client *fake.Clientset) *K8sProvisioner {
	t.Helper()
	p := &K8sProvisioner{client: client, workloads: newWorkloadCache(client)}
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

// The cache keeps a pod's identity, labels, deletion and conditions, and no object's managedFields.
func TestTrimCached(t *testing.T) {
	now := metav1.Now()
	managed := []metav1.ManagedFieldsEntry{{Manager: "kubelet"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "coder-0", Namespace: scaleTestNS, UID: "u", ResourceVersion: "9", Labels: map[string]string{agentTypeLabel: "a"},
			DeletionTimestamp: &now, Annotations: map[string]string{"sidecar.istio.io/status": "big"}, ManagedFields: managed,
		},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: sessionManagerContainerName}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
	out, err := trimCached(pod)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(*corev1.Pod)
	if got.Name != "coder-0" || got.Namespace != scaleTestNS || got.UID != "u" || got.ResourceVersion != "9" ||
		got.Labels[agentTypeLabel] != "a" || got.DeletionTimestamp == nil || !podReady(got) {
		t.Errorf("trimmed pod lost a field the scaler reads: %+v", got)
	}
	if got.Annotations != nil || got.ManagedFields != nil || len(got.Spec.Containers) != 0 || got.Status.Phase != "" {
		t.Errorf("trimmed pod kept fields the scaler never reads: %+v", got)
	}
	ss := liveStatefulSet(AgentType{Slug: "coder"}, 1, appsv1.ParallelPodManagement)
	ss.ManagedFields = managed
	out, err = trimCached(ss)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(*appsv1.StatefulSet); got.ManagedFields != nil || len(got.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("trimmed StatefulSet: managedFields %v, containers %d; want none and the template kept", got.ManagedFields, len(got.Spec.Template.Spec.Containers))
	}
}

// agentWorkloads is a client holding n agents with one Ready pod each, and one sandbox pod the cache must skip.
func agentWorkloads(n int) *fake.Clientset {
	objs := []runtime.Object{&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sandbox", Namespace: scaleTestNS}}}
	for i := range n {
		at := AgentType{ID: fmt.Sprintf("id-%d", i), Slug: fmt.Sprintf("agent-%d", i), TenantID: "acme"}
		objs = append(objs, liveStatefulSet(at, 1, appsv1.ParallelPodManagement), &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: at.Slug + "-0", Namespace: scaleTestNS, Labels: podLabels(at)},
			Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
		})
	}
	return fake.NewSimpleClientset(objs...)
}

// After the cache syncs, reading every agent's view makes no API call.
func TestAgentStatefulSetMakesNoAPIRead(t *testing.T) {
	const agents = 50
	client := agentWorkloads(agents)
	p := cachedProvisioner(t, client)
	synced := len(client.Actions())
	for i := range agents {
		view, found, err := p.AgentStatefulSet(t.Context(), fmt.Sprintf("agent-%d", i), scaleTestNS)
		if err != nil || !found || view.Replicas != 1 || len(view.Ready) != 1 {
			t.Fatalf("agent-%d: view %+v, found %v, err %v", i, view, found, err)
		}
	}
	if extra := client.Actions()[synced:]; len(extra) != 0 {
		t.Errorf("a pass over %d agents made %d API calls after the sync, want 0: %v", agents, len(extra), extra)
	}
	if _, err := p.workloads.Core().V1().Pods().Lister().Pods(scaleTestNS).Get("sandbox"); err == nil {
		t.Error("the cache holds a sandbox pod")
	}
	if _, found, err := p.AgentStatefulSet(t.Context(), "missing", scaleTestNS); found || err != nil {
		t.Errorf("missing agent: found %v, err %v; want not found", found, err)
	}
}

// BenchmarkScalerPassReads times the Kubernetes reads of one scaler pass. Each API call waits on the
// kora-agents limiter (50 QPS, burst 100) and 9.5 ms of latency measured on production.
// The cache takes the API out of the pass, which before this cache made 2 calls per agent.
func BenchmarkScalerPassReads(b *testing.B) {
	for _, agents := range []int{48, 480, 960} {
		b.Run(fmt.Sprintf("agents=%d", agents), func(b *testing.B) {
			client := agentWorkloads(agents)
			p := &K8sProvisioner{client: client, workloads: newWorkloadCache(client)}
			if err := p.SyncWorkloadCache(b.Context()); err != nil {
				b.Fatal(err)
			}
			limiter := flowcontrol.NewTokenBucketRateLimiter(50, 100)
			var calls atomic.Int64
			client.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
				limiter.Accept()
				time.Sleep(9500 * time.Microsecond)
				calls.Add(1)
				return false, nil, nil
			})
			for b.Loop() {
				for i := range agents {
					if _, _, err := p.AgentStatefulSet(b.Context(), fmt.Sprintf("agent-%d", i), scaleTestNS); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.ReportMetric(float64(calls.Load())/float64(b.N), "api-calls/pass")
		})
	}
}

// A cache the API server never lists for returns when ctx ends, with the sync error.
func TestSyncWorkloadCacheStopsWithContext(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	p := &K8sProvisioner{client: client, workloads: newWorkloadCache(client)}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if err := p.SyncWorkloadCache(ctx); !errors.Is(err, errWorkloadCacheSync) {
		t.Errorf("err = %v, want errWorkloadCacheSync", err)
	}
}
