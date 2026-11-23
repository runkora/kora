package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// residencyPod is an agent pod whose session-manager has the spec requests and the applied requests.
func residencyPod(name string, spec, applied corev1.ResourceRequirements) *corev1.Pod {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: scaleTestNS, Labels: podLabels(at)},
		Spec:       corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: sessionManagerContainerName, Resources: spec}}},
		Status: corev1.PodStatus{
			PodIP:             "10.0.0.1",
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

// withSessionManager sets the configured session-manager CPU and memory.
func withSessionManager(p *K8sProvisioner, cpu, memory string) {
	c, m := resource.MustParse(cpu), resource.MustParse(memory)
	p.cfg.Workloads.SessionManager.CPU, p.cfg.Workloads.SessionManager.Memory = &c, &m
}

// A shrink sets the warm requests and a grow the configured requests, a raised CPU included. Neither sets limits.
func TestShrinkAndGrowPod(t *testing.T) {
	patched := smResources("400m", "1Gi")
	p, _, patches := warmProvisioner(t, residencyPod("coder-0", patched, patched))
	withSessionManager(p, "400m", "1Gi")
	ctx := context.Background()
	if err := p.ShrinkPod(ctx, scaleTestNS, "coder-0"); err != nil {
		t.Fatal(err)
	}
	if err := p.GrowPod(ctx, scaleTestNS, "coder-0"); err != nil {
		t.Fatal(err)
	}
	if len(*patches) != 2 {
		t.Fatalf("resize patches = %d, want 2", len(*patches))
	}
	if got := resizeRequests(t, (*patches)[0]); got.Cpu().String() != "25m" || got.Memory().String() != "128Mi" {
		t.Errorf("shrink requests = %v, want 25m and 128Mi", got)
	}
	if got := resizeRequests(t, (*patches)[1]); got.Cpu().String() != "400m" || got.Memory().String() != "1Gi" {
		t.Errorf("grow requests = %v, want the configured 400m and 1Gi", got)
	}
}

// A size at or below the warm memory is never shrunk, and a grow of a pod a roll has not reached stays
// within its own limits.
func TestShrinkAndGrowBounds(t *testing.T) {
	small := smResources("20m", "100Mi")
	p, _, patches := warmProvisioner(t, residencyPod("coder-0", small, small))
	withSessionManager(p, "20m", "100Mi")
	if err := p.ShrinkPod(context.Background(), scaleTestNS, "coder-0"); err != nil || len(*patches) != 0 {
		t.Errorf("shrink of a small size: %v and %d patches, want none", err, len(*patches))
	}
	old := residencyPod("coder-0", smResources("25m", "128Mi"), smResources("25m", "128Mi"))
	p, _, patches = warmProvisioner(t, old)
	withSessionManager(p, "250m", "2Gi")
	if err := p.GrowPod(context.Background(), scaleTestNS, "coder-0"); err != nil {
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
	p, _, patches := warmProvisioner(t, residencyPod("coder-0", full, full))
	for _, name := range []string{"coder-0", "coder-1"} {
		if err := p.WakePod(context.Background(), scaleTestNS, name, false); err != nil {
			t.Fatalf("%s: %v", name, err)
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
		pod, found, _ := p.cachedAgentPod(scaleTestNS, name)
		return found && !podWarm(pod, p.fullRequests())
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
	p, client, patches := warmProvisioner(t, residencyPod("coder-0", smResources("25m", "128Mi"), smResources("25m", "128Mi")))
	go applyResize(t, p, client, "coder-0")
	if err := p.WakePod(context.Background(), scaleTestNS, "coder-0", false); err != nil {
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
	p, client, patches := warmProvisioner(t, deferred, reserve("reserve-a", "node-a"), reserve("reserve-b", "node-b"))
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
	err := p.WakePod(context.Background(), scaleTestNS, "coder-0", false)
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

// AgentPods lists a pod with a pending resize and the warm pods.
func TestAgentPodsResidency(t *testing.T) {
	full, warm := smResources("250m", "1Gi"), smResources("25m", "128Mi")
	p, _, _ := warmProvisioner(t, residencyPod("coder-0", full, full), residencyPod("coder-1", warm, warm), residencyPod("coder-2", full, warm))
	pods, err := p.AgentPods(context.Background(), "coder", scaleTestNS)
	if err != nil {
		t.Fatal(err)
	}
	var warmPods, resizing []string
	for _, pod := range pods.Live {
		if pod.Warm {
			warmPods = append(warmPods, pod.Name)
		}
		if pod.Resizing {
			resizing = append(resizing, pod.Name)
		}
	}
	if len(pods.ready()) != 3 || !slices.Equal(warmPods, []string{"coder-1"}) || !slices.Equal(resizing, []string{"coder-2"}) {
		t.Errorf("pods = %+v; want 3 Ready, coder-1 warm and coder-2 resizing", pods.Live)
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
			client := fake.NewClientset(tc.pod)
			var patches int
			client.PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
				patches++
				return false, nil, nil
			})
			p := cacheProvisioner(client)
			p.logger = logging.NewLoggerWithOutput(io.Discard)
			if err := p.WakePod(context.Background(), scaleTestNS, "coder-0", false); err != nil {
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
	p, client, patches := warmProvisioner(t, residencyPod("coder-0", full, full))
	// The live pod is warm. The cache still shows it hot because the reactor answers only the live GET.
	client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, residencyPod("coder-0", smResources("25m", "128Mi"), smResources("25m", "128Mi")), nil
	})
	if err := p.WakePod(context.Background(), scaleTestNS, "coder-0", true); err != nil {
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
	p, client, patches := warmProvisioner(t, deferred, reserve)
	if err := p.GrowPod(context.Background(), scaleTestNS, "coder-0"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("kora-system").Get(context.Background(), "reserve-a", metav1.GetOptions{}); err == nil {
		t.Error("the reserve on the pod's node still exists")
	}
	if len(*patches) != 0 {
		t.Errorf("resize patches = %d, want none for a grow already requested", len(*patches))
	}
}

// A missing resize subresource, no permission or an invalid request is a refusal for good. A pod that is gone
// is not: its replacement may resize.
func TestResizeRefused(t *testing.T) {
	pods := schema.GroupResource{Resource: "pods"}
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"no resize subresource": {apierrors.NewGenericServerResponse(404, "patch", pods, "", "", 0, false), true},
		"pod gone":              {apierrors.NewNotFound(pods, "coder-0"), false},
		"forbidden":             {apierrors.NewForbidden(pods, "coder-0", errors.New("no pods/resize")), true},
		"invalid":               {apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "coder-0", nil), true},
		"conflict":              {apierrors.NewConflict(pods, "coder-0", errors.New("changed")), false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := resizeRefused(fmt.Errorf("resize pod: %w", tc.err)); got != tc.want {
				t.Errorf("resizeRefused = %v, want %v", got, tc.want)
			}
		})
	}
}
