package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

// The cache keeps a pod's identity, labels, deletion, node, conditions and session-manager resources, and no
// object's managedFields.
func TestTrimCached(t *testing.T) {
	now := metav1.Now()
	managed := []metav1.ManagedFieldsEntry{{Manager: "kubelet"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "coder-0", Namespace: scaleTestNS, UID: "u", ResourceVersion: "9", Labels: map[string]string{agentTypeLabel: "a"},
			DeletionTimestamp: &now, Annotations: map[string]string{"sidecar.istio.io/status": "big"}, ManagedFields: managed,
		},
		Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{
			{Name: sessionManagerContainerName, Image: "sm", Resources: smResources("25m", "128Mi")},
			{Name: "other", Image: "other"},
		}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
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
	if got.Name != "coder-0" || got.Namespace != scaleTestNS || got.UID != "u" || got.ResourceVersion != "9" ||
		got.Labels[agentTypeLabel] != "a" || got.DeletionTimestamp == nil || !podReady(got) {
		t.Errorf("trimmed pod lost a field the scaler reads: %+v", got)
	}
	if got.Spec.NodeName != "node-a" || !podWarm(got) || !resizePending(got) {
		t.Errorf("trimmed pod lost the node or session-manager resources the grow reads: %+v", got)
	}
	if got.Annotations != nil || got.ManagedFields != nil || got.Status.Phase != "" ||
		len(got.Spec.Containers) != 1 || got.Spec.Containers[0].Image != "" ||
		len(got.Status.ContainerStatuses) != 1 || got.Status.ContainerStatuses[0].Image != "" {
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
	if _, found, err := p.AgentStatefulSet(t.Context(), "missing", scaleTestNS); found || err != nil {
		t.Errorf("missing agent: found %v, err %v; want not found", found, err)
	}
}

// LivePodOrdinals reads the pods from the API server with one LIST, not from a cache that lags a terminating pod.
func TestLivePodOrdinalsReadsLive(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ready := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: scaleTestNS, Labels: podLabels(at)},
			Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
		}
	}
	ss := liveStatefulSet(at, 2, appsv1.ParallelPodManagement)
	now := metav1.Now()
	draining := ready("coder-0")
	draining.DeletionTimestamp = &now
	client := fake.NewSimpleClientset(ss, draining, ready("coder-1"))
	// The cache holds the StatefulSet and both pods as Ready: it has not seen coder-0 start terminating.
	p := &K8sProvisioner{client: client, workloads: newWorkloadCache(client)}
	for _, obj := range []any{ss, ready("coder-0"), ready("coder-1")} {
		var err error
		if _, isSet := obj.(*appsv1.StatefulSet); isSet {
			err = p.workloads.Apps().V1().StatefulSets().Informer().GetIndexer().Add(obj)
		} else {
			err = p.workloads.Core().V1().Pods().Informer().GetIndexer().Add(obj)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if view, _, _ := p.AgentStatefulSet(t.Context(), at.Slug, scaleTestNS); len(view.Ready) != 2 || view.Terminating != 0 {
		t.Fatalf("cached view: ready %v, terminating %d; want the lagging [0 1] and 0", view.Ready, view.Terminating)
	}
	got, terminating, err := p.LivePodOrdinals(t.Context(), at.Slug, scaleTestNS)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 1 || terminating != 1 {
		t.Errorf("live read: ready %v, terminating %d; want [1] and 1", got, terminating)
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
	p := &K8sProvisioner{client: client, workloads: newWorkloadCache(client)}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if err := p.SyncWorkloadCache(ctx); !errors.Is(err, errWorkloadCacheSync) {
		t.Errorf("err = %v, want errWorkloadCacheSync", err)
	}
}
