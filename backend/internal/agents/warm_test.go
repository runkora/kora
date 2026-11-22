package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// smResources is a session-manager's resources with the given requests and the 2 CPU and 1Gi limits.
func smResources(cpu, memory string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(memory)},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("1Gi")},
	}
}

// residencyStatefulSet is the coder StatefulSet with a session-manager template of the given resources.
func residencyStatefulSet(replicas int32, template corev1.ResourceRequirements) *appsv1.StatefulSet {
	ss := liveStatefulSet(AgentType{Slug: "coder", TenantID: "acme"}, replicas, appsv1.ParallelPodManagement)
	ss.Spec.Template.Spec.Containers[0].Resources = template
	return ss
}

// residencyPod is an agent pod whose session-manager has the spec requests and the applied requests.
func residencyPod(name string, spec, applied corev1.ResourceRequirements) *corev1.Pod {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: scaleTestNS, Labels: podLabels(at)},
		Spec:       corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: sessionManagerContainerName, Resources: spec}}},
		Status: corev1.PodStatus{
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: sessionManagerContainerName, Resources: &applied}},
		},
	}
}

func TestResidencyOfPod(t *testing.T) {
	full, warm := smResources("250m", "1Gi"), smResources("25m", "128Mi")
	template := full.Requests
	deferred := residencyPod("coder-0", full, warm)
	deferred.Status.Conditions = append(deferred.Status.Conditions, corev1.PodCondition{Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: "Deferred"})
	infeasible := residencyPod("coder-0", full, warm)
	infeasible.Status.Conditions = append(infeasible.Status.Conditions, corev1.PodCondition{Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: corev1.PodReasonInfeasible})
	inProgress := residencyPod("coder-0", full, warm)
	inProgress.Status.Conditions = append(inProgress.Status.Conditions, corev1.PodCondition{Type: corev1.PodResizeInProgress, Status: corev1.ConditionTrue})
	noStatus := residencyPod("coder-0", warm, warm)
	noStatus.Status.ContainerStatuses = nil
	for _, tc := range []struct {
		name                       string
		pod                        *corev1.Pod
		warm, resizing, isDeferred bool
	}{
		{"hot", residencyPod("coder-0", full, full), false, false, false},
		{"warm", residencyPod("coder-0", warm, warm), true, false, false},
		{"shrinking", residencyPod("coder-0", warm, full), true, true, false},
		{"growing", residencyPod("coder-0", full, warm), false, true, false},
		{"grow deferred", deferred, false, true, true},
		{"grow infeasible", infeasible, false, true, false},
		{"grow in progress", inProgress, false, true, false},
		{"no applied resources yet", noStatus, true, false, false},
		// A manifest patch may set a request below the limit. The pod is hot at its template's requests.
		{"patched request below the limit", residencyPod("coder-0", smResources("250m", "512Mi"), smResources("250m", "512Mi")), false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpl := template
			if tc.name == "patched request below the limit" {
				tmpl = smResources("250m", "512Mi").Requests
			}
			if got := podWarm(tc.pod, tmpl); got != tc.warm {
				t.Errorf("podWarm = %v, want %v", got, tc.warm)
			}
			if got := resizePending(tc.pod); got != tc.resizing {
				t.Errorf("resizePending = %v, want %v", got, tc.resizing)
			}
			if got := resizeDeferred(tc.pod); got != tc.isDeferred {
				t.Errorf("resizeDeferred = %v, want %v", got, tc.isDeferred)
			}
		})
	}
}

// warmProvisioner is a provisioner over objs with a synced workload cache and a recorded resize patch.
func warmProvisioner(t *testing.T, objs ...runtime.Object) (*K8sProvisioner, *fake.Clientset, *[]string) {
	t.Helper()
	client := fake.NewClientset(objs...)
	var patches []string
	client.PrependReactor("patch", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "resize" {
			patches = append(patches, string(action.(k8stesting.PatchAction).GetPatch()))
		}
		return false, nil, nil
	})
	p := cachedProvisioner(t, client)
	p.growTimeout = 300 * time.Millisecond
	p.logger = logging.NewLoggerWithOutput(io.Discard)
	p.cfg.SystemNamespace = "kora-system"
	return p, client, &patches
}

// resizeRequests decodes the session-manager requests a resize patch sets.
func resizeRequests(t *testing.T, patch string) corev1.ResourceList {
	t.Helper()
	var body struct {
		Spec struct {
			Containers []corev1.Container `json:"containers"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(patch), &body); err != nil || len(body.Spec.Containers) != 1 || body.Spec.Containers[0].Name != sessionManagerContainerName {
		t.Fatalf("resize patch %s: %v", patch, err)
	}
	if body.Spec.Containers[0].Resources.Limits != nil {
		t.Errorf("resize patch %s sets limits, want requests only", patch)
	}
	return body.Spec.Containers[0].Resources.Requests
}

// A shrink sets the warm requests and a grow the template's requests, a patched CPU included. Neither sets limits.
func TestShrinkAndGrowPod(t *testing.T) {
	patched := smResources("400m", "1Gi")
	p, _, patches := warmProvisioner(t, residencyStatefulSet(1, patched), residencyPod("coder-0", patched, patched))
	ctx := context.Background()
	if err := p.ShrinkPod(ctx, "coder", scaleTestNS, 0); err != nil {
		t.Fatal(err)
	}
	if err := p.GrowPod(ctx, "coder", scaleTestNS, 0); err != nil {
		t.Fatal(err)
	}
	if len(*patches) != 2 {
		t.Fatalf("resize patches = %d, want 2", len(*patches))
	}
	if got := resizeRequests(t, (*patches)[0]); got.Cpu().String() != "25m" || got.Memory().String() != "128Mi" {
		t.Errorf("shrink requests = %v, want 25m and 128Mi", got)
	}
	if got := resizeRequests(t, (*patches)[1]); got.Cpu().String() != "400m" || got.Memory().String() != "1Gi" {
		t.Errorf("grow requests = %v, want the template's 400m and 1Gi", got)
	}
}

// A template at or below the warm memory is never shrunk, and a grow of a pod a roll has not reached stays
// within its own limits.
func TestShrinkAndGrowBounds(t *testing.T) {
	small := smResources("20m", "100Mi")
	p, _, patches := warmProvisioner(t, residencyStatefulSet(1, small), residencyPod("coder-0", small, small))
	if err := p.ShrinkPod(context.Background(), "coder", scaleTestNS, 0); err != nil || len(*patches) != 0 {
		t.Errorf("shrink of a small template: %v and %d patches, want none", err, len(*patches))
	}
	bigger := corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("2Gi")}}
	old := residencyPod("coder-0", smResources("25m", "128Mi"), smResources("25m", "128Mi"))
	p, _, patches = warmProvisioner(t, residencyStatefulSet(1, bigger), old)
	if err := p.GrowPod(context.Background(), "coder", scaleTestNS, 0); err != nil {
		t.Fatal(err)
	}
	if len(*patches) != 1 {
		t.Fatalf("resize patches = %d, want 1", len(*patches))
	}
	if got := resizeRequests(t, (*patches)[0]); got.Memory().String() != "1Gi" {
		t.Errorf("grow memory = %v, want the pod's 1Gi limit", got.Memory())
	}
}

// A hot pod and a pod the cache does not hold take no grow.
func TestWakePodSkipsHotPod(t *testing.T) {
	full := smResources("250m", "1Gi")
	p, _, patches := warmProvisioner(t, residencyStatefulSet(2, full), residencyPod("coder-0", full, full))
	for _, ord := range []int{0, 1} {
		if err := p.WakePod(context.Background(), "coder", scaleTestNS, ord, false); err != nil {
			t.Fatalf("ordinal %d: %v", ord, err)
		}
	}
	if len(*patches) != 0 {
		t.Errorf("resize patches = %v, want none", *patches)
	}
}

// applyResize stands in for the kubelet: it sets the pod's applied requests to its spec once the cache shows the grow.
func applyResize(t *testing.T, p *K8sProvisioner, client *fake.Clientset, name string) {
	t.Helper()
	waitCached(t, func() bool {
		pod, template, found, _ := p.cachedAgentPod("coder", scaleTestNS, 0)
		return found && !podWarm(pod, template)
	})
	pod, err := client.CoreV1().Pods(scaleTestNS).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Error(err)
		return
	}
	pod.Status.ContainerStatuses[0].Resources = new(pod.Spec.Containers[0].Resources)
	if _, err := client.CoreV1().Pods(scaleTestNS).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Error(err)
	}
}

// A warm pod grows and WakePod returns once the applied requests are full.
func TestWakePodGrowsWarmPod(t *testing.T) {
	p, client, patches := warmProvisioner(t, residencyStatefulSet(1, smResources("250m", "1Gi")),
		residencyPod("coder-0", smResources("25m", "128Mi"), smResources("25m", "128Mi")))
	go applyResize(t, p, client, "coder-0")
	if err := p.WakePod(context.Background(), "coder", scaleTestNS, 0, false); err != nil {
		t.Fatal(err)
	}
	if len(*patches) != 1 {
		t.Errorf("resize patches = %d, want 1", len(*patches))
	}
}

// A Deferred grow deletes the wake reserve on its node once and only there, and times out if room never frees.
func TestWakePodDeferredReleasesReserveAndTimesOut(t *testing.T) {
	reserve := func(name, node string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kora-system", Labels: map[string]string{"app": "kora-agent-wake-reserve"}},
			Spec:       corev1.PodSpec{NodeName: node},
		}
	}
	deferred := residencyPod("coder-0", smResources("250m", "1Gi"), smResources("25m", "128Mi"))
	deferred.Status.Conditions = append(deferred.Status.Conditions, corev1.PodCondition{Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: "Deferred"})
	p, client, patches := warmProvisioner(t, residencyStatefulSet(1, smResources("250m", "1Gi")), deferred,
		reserve("reserve-a", "node-a"), reserve("reserve-b", "node-b"))
	var deletes int
	client.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		deletes++
		return false, nil, nil
	})
	// The fake ignores field selectors, so this keeps the List to the node the real API server would return.
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if got := action.(k8stesting.ListAction).GetListRestrictions().Fields.String(); got != "spec.nodeName=node-a" {
			t.Errorf("reserve list field selector = %q, want spec.nodeName=node-a", got)
		}
		return true, &corev1.PodList{Items: []corev1.Pod{*reserve("reserve-a", "node-a")}}, nil
	})
	err := p.WakePod(context.Background(), "coder", scaleTestNS, 0, false)
	if !errors.Is(err, errGrowTimeout) {
		t.Fatalf("WakePod = %v, want errGrowTimeout", err)
	}
	if len(*patches) != 0 {
		t.Errorf("resize patches = %d, want none for a grow already requested", len(*patches))
	}
	if deletes != 1 {
		t.Errorf("reserve deletes = %d, want 1", deletes)
	}
	if _, err := client.CoreV1().Pods("kora-system").Get(context.Background(), "reserve-a", metav1.GetOptions{}); err == nil {
		t.Error("reserve on the pod's node still exists")
	}
	if _, err := client.CoreV1().Pods("kora-system").Get(context.Background(), "reserve-b", metav1.GetOptions{}); err != nil {
		t.Errorf("reserve on another node: %v", err)
	}
}

// ReadyOrdinals, from the live client, and the scaler's view, from the cache, list a pod with a pending resize
// and the warm pods.
func TestOrdinalsResidency(t *testing.T) {
	full, warm := smResources("250m", "1Gi"), smResources("25m", "128Mi")
	p, _, _ := warmProvisioner(t, residencyStatefulSet(3, full),
		residencyPod("coder-0", full, full), residencyPod("coder-1", warm, warm), residencyPod("coder-2", full, warm))
	o, err := p.ReadyOrdinals(context.Background(), "coder", scaleTestNS)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(o.ready, []int{0, 1, 2}) || !slices.Equal(o.warm, []int{1}) || !slices.Equal(o.resizing, []int{2}) {
		t.Errorf("ReadyOrdinals = %+v; want ready [0 1 2], warm [1] and resizing [2]", o)
	}
	view, found, err := p.AgentStatefulSet(context.Background(), "coder", scaleTestNS)
	if err != nil || !found {
		t.Fatalf("AgentStatefulSet: %v, %v", found, err)
	}
	if !slices.Equal(view.Warm, []int{1}) || !slices.Equal(view.Resizing, []int{2}) {
		t.Errorf("view warm %v and resizing %v, want [1] and [2]", view.Warm, view.Resizing)
	}
}

// Before the cache syncs, a warm pod is read live and grown with no wait, and a hot pod is left alone.
func TestWakePodBeforeCacheSync(t *testing.T) {
	for name, tc := range map[string]struct {
		pod   *corev1.Pod
		grows bool
	}{
		"warm": {residencyPod("coder-0", smResources("25m", "128Mi"), smResources("25m", "128Mi")), true},
		"hot":  {residencyPod("coder-0", smResources("250m", "1Gi"), smResources("250m", "1Gi")), false},
	} {
		t.Run(name, func(t *testing.T) {
			client := fake.NewClientset(residencyStatefulSet(1, smResources("250m", "1Gi")), tc.pod)
			var patches int
			client.PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
				patches++
				return false, nil, nil
			})
			p := &K8sProvisioner{client: client, workloads: newWorkloadCache(client), logger: logging.NewLoggerWithOutput(io.Discard)}
			if err := p.WakePod(context.Background(), "coder", scaleTestNS, 0, false); err != nil {
				t.Fatal(err)
			}
			if (patches == 1) != tc.grows {
				t.Errorf("patches = %d, want a grow %v", patches, tc.grows)
			}
		})
	}
}

// A pod with no node yet has no reserve to release, and no pod list is made.
func TestReleaseWakeReserveWithNoNode(t *testing.T) {
	client := fake.NewClientset()
	p := &K8sProvisioner{client: client, logger: logging.NewLoggerWithOutput(io.Discard)}
	p.releaseWakeReserve(context.Background(), "")
	if n := len(client.Actions()); n != 0 {
		t.Errorf("API actions = %d, want none", n)
	}
}

// A pod placement listed as warm grows even when the cache has not seen the shrink yet.
func TestWakePodLiveWarmOverStaleCache(t *testing.T) {
	full := smResources("250m", "1Gi")
	p, client, patches := warmProvisioner(t, residencyStatefulSet(1, full), residencyPod("coder-0", full, full))
	// The live pod is warm. The cache still shows it hot because the reactor answers only the live GET.
	client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, residencyPod("coder-0", smResources("25m", "128Mi"), smResources("25m", "128Mi")), nil
	})
	if err := p.WakePod(context.Background(), "coder", scaleTestNS, 0, true); err != nil {
		t.Fatal(err)
	}
	if len(*patches) != 1 {
		t.Fatalf("resize patches = %d, want 1", len(*patches))
	}
	if got := resizeRequests(t, (*patches)[0]); got.Memory().String() != "1Gi" {
		t.Errorf("grow memory = %v, want 1Gi", got.Memory())
	}
}

// The scaler's grow of a pod whose grow is held as Deferred frees the wake reserve on its node.
func TestGrowPodDeferredReleasesReserve(t *testing.T) {
	deferred := residencyPod("coder-0", smResources("250m", "1Gi"), smResources("25m", "128Mi"))
	deferred.Status.Conditions = append(deferred.Status.Conditions, corev1.PodCondition{Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: corev1.PodReasonDeferred})
	reserve := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "reserve-a", Namespace: "kora-system", Labels: map[string]string{"app": "kora-agent-wake-reserve"}},
		Spec:       corev1.PodSpec{NodeName: "node-a"},
	}
	p, client, patches := warmProvisioner(t, residencyStatefulSet(1, smResources("250m", "1Gi")), deferred, reserve)
	if err := p.GrowPod(context.Background(), "coder", scaleTestNS, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("kora-system").Get(context.Background(), "reserve-a", metav1.GetOptions{}); err == nil {
		t.Error("the reserve on the pod's node still exists")
	}
	if len(*patches) != 0 {
		t.Errorf("resize patches = %d, want none for a grow already requested", len(*patches))
	}
}
