package agents

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const scaleTestNS = "kora-tenant-acme"

// poolFixture is a tenant pool Deployment, its ReplicaSet and pool pods as the Deployment controller makes them.
type poolFixture struct {
	dep *appsv1.Deployment
	rs  *appsv1.ReplicaSet
}

func newPoolFixture() poolFixture {
	yes := true
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: poolName, Namespace: scaleTestNS, UID: "dep-uid", Labels: map[string]string{poolLabel: "true"}},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{poolTemplateAnnotation: testTemplate}},
		}},
	}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: poolName + "-abc", Namespace: scaleTestNS, UID: "rs-uid", Labels: map[string]string{poolLabel: "true"},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: poolName, UID: "dep-uid", Controller: &yes}},
	}}
	return poolFixture{dep: dep, rs: rs}
}

// pod is a Ready warm pool pod controlled by the fixture's ReplicaSet, on template, created at the second created.
func (f poolFixture) pod(name, template string, created int) *corev1.Pod {
	yes := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: scaleTestNS, ResourceVersion: "7",
			Labels:            map[string]string{poolLabel: "true", "pod-template-hash": "abc", "runkora.io/tenant": "acme"},
			Annotations:       map[string]string{poolTemplateAnnotation: template},
			CreationTimestamp: metav1.NewTime(time.Unix(int64(created), 0)),
			OwnerReferences:   []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: f.rs.Name, UID: f.rs.UID, Controller: &yes}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: sessionManagerContainerName, Resources: corev1.ResourceRequirements{
			Requests: smResources("25m", "128Mi").Requests, Limits: smResources("1", "1Gi").Limits,
		}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.5", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
}

// bindProvisioner is a synced provisioner over objs that records each pod patch by subresource.
func bindProvisioner(t *testing.T, objs ...runtime.Object) (*K8sProvisioner, *fake.Clientset, map[string][]string) {
	t.Helper()
	client := fake.NewClientset(objs...)
	patches := map[string][]string{}
	client.PrependReactor("patch", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patch := action.(k8stesting.PatchAction)
		patches[action.GetSubresource()] = append(patches[action.GetSubresource()], patch.GetName()+" "+string(patch.GetPatch()))
		return false, nil, nil
	})
	p := cachedProvisioner(t, client)
	p.dynamicClient = ownerRefDynamicClient("acme")
	return p, client, patches
}

// The bind takes the oldest Ready pool pod on the current template in one patch that carries its resource
// version, sets the agent labels, drops the pool label and replaces the ReplicaSet owner with the KoraTenant,
// then grows the pod.
func TestBindPod(t *testing.T) {
	f := newPoolFixture()
	notReadyPod := f.pod("pool-0", testTemplate, 0)
	notReadyPod.Status.Conditions = nil
	p, _, patches := bindProvisioner(t, f.dep, f.rs, notReadyPod, f.pod("pool-old", "t0", 1), f.pod("pool-b", testTemplate, 3), f.pod("pool-a", testTemplate, 2))
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	name, err := p.BindPod(t.Context(), at, scaleTestNS)
	if err != nil || name != "pool-a" {
		t.Fatalf("BindPod = %q, %v; want pool-a", name, err)
	}
	if len(patches[""]) != 1 || len(patches["resize"]) != 1 {
		t.Fatalf("patches = %v, want one bind and one resize", patches)
	}
	var bind struct {
		Metadata struct {
			ResourceVersion string                  `json:"resourceVersion"`
			Labels          map[string]*string      `json:"labels"`
			OwnerReferences []metav1.OwnerReference `json:"ownerReferences"`
		} `json:"metadata"`
	}
	body := patches[""][0][len("pool-a "):]
	if err := json.Unmarshal([]byte(body), &bind); err != nil {
		t.Fatal(err)
	}
	m := bind.Metadata
	if m.ResourceVersion != "7" {
		t.Errorf("resourceVersion = %q, want the pool pod's 7 as the precondition", m.ResourceVersion)
	}
	if v, ok := m.Labels[poolLabel]; !ok || v != nil {
		t.Errorf("pool label = %v, want it removed", v)
	}
	for _, k := range []string{"spiffe.io/agent", "app"} {
		if m.Labels[k] == nil || *m.Labels[k] != "coder" {
			t.Errorf("label %s = %v, want coder", k, m.Labels[k])
		}
	}
	if m.Labels[agentTypeLabel] == nil || *m.Labels[agentTypeLabel] != "at-1" || len(m.Labels) != 5 {
		t.Errorf("labels = %v, want only the five bind labels", m.Labels)
	}
	if v := m.Labels[agentConfigLabel]; v == nil || *v != agentConfigHash(at) {
		t.Errorf("agent config label = %v, want %s", v, agentConfigHash(at))
	}
	if len(m.OwnerReferences) != 1 || m.OwnerReferences[0].Kind != koraTenantKind || m.OwnerReferences[0].Name != "acme" {
		t.Errorf("owners = %+v, want the KoraTenant alone", m.OwnerReferences)
	}
	if got := resizeRequests(t, patches["resize"][0][len("pool-a "):]); got.Memory().String() != "1Gi" {
		t.Errorf("grow requests = %v, want the full 1Gi", got)
	}
	bound, err := p.client.CoreV1().Pods(scaleTestNS).Get(t.Context(), "pool-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, pooled := bound.Labels[poolLabel]; pooled || bound.Labels["app"] != "coder" || bound.Labels["pod-template-hash"] != "abc" {
		t.Errorf("bound pod labels = %v, want the agent labels and pod-template-hash kept", bound.Labels)
	}
}

// A pod another replica bound first, which fails the resource version precondition, is skipped for the next one.
func TestBindPodSkipsAConflict(t *testing.T) {
	f := newPoolFixture()
	p, client, _ := bindProvisioner(t, f.dep, f.rs, f.pod("pool-a", testTemplate, 1), f.pod("pool-b", testTemplate, 2))
	client.PrependReactor("patch", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "" && action.(k8stesting.PatchAction).GetName() == "pool-a" {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, "pool-a", errors.New("changed"))
		}
		return false, nil, nil
	})
	name, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS)
	if err != nil || name != "pool-b" {
		t.Errorf("BindPod = %q, %v; want pool-b after the conflict on pool-a", name, err)
	}
}

// A patch the API server refuses as invalid or forbidden is logged, and the next pool pod is bound.
func TestBindPodSkipsARefusedPatch(t *testing.T) {
	for name, refusal := range map[string]error{
		"forbidden": apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "pool-a", errors.New("kora-agent-bind")),
		"invalid":   apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "pool-a", nil),
	} {
		t.Run(name, func(t *testing.T) {
			f := newPoolFixture()
			p, client, _ := bindProvisioner(t, f.dep, f.rs, f.pod("pool-a", testTemplate, 1), f.pod("pool-b", testTemplate, 2))
			client.PrependReactor("patch", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() == "" && action.(k8stesting.PatchAction).GetName() == "pool-a" {
					return true, nil, refusal
				}
				return false, nil, nil
			})
			name, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS)
			if err != nil || name != "pool-b" {
				t.Errorf("BindPod = %q, %v; want pool-b after the refusal of pool-a", name, err)
			}
		})
	}
}

// A provenance check the API server cannot answer skips the pod.
func TestBindPodSkipsAFailedProvenanceRead(t *testing.T) {
	f := newPoolFixture()
	p, client, patches := bindProvisioner(t, f.dep, f.rs, f.pod("pool-a", testTemplate, 1))
	client.PrependReactor("get", "replicasets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api down")
	})
	if _, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS); !errors.Is(err, errNoPoolPod) || len(patches) != 0 {
		t.Errorf("BindPod = %v with patches %v, want errNoPoolPod and no patch", err, patches)
	}
}

// With no bound pod coming up, a pool pod coming up keeps a run queued past the start budget.
func TestLivePodCountsPoolPods(t *testing.T) {
	f := newPoolFixture()
	stuck := f.pod("pool-s", testTemplate, 1)
	stuck.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: sessionManagerContainerName, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}}
	for name, tc := range map[string]struct {
		objs []runtime.Object
		want bool
	}{
		"no pool pod":      {nil, false},
		"a stuck pool pod": {[]runtime.Object{stuck}, false},
		"a pool pod up":    {[]runtime.Object{f.pod("pool-a", testTemplate, 2)}, true},
	} {
		t.Run(name, func(t *testing.T) {
			p := podSecurityProvisioner()
			p.client = fake.NewSimpleClientset(tc.objs...)
			if got, err := p.LivePod(t.Context(), "coder", scaleTestNS); err != nil || got != tc.want {
				t.Errorf("LivePod = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// The agent ServiceAccount an earlier release created goes once no StatefulSet pod of the agent is left.
func TestRetireServiceAccount(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	ssPod := boundPod(at, "coder-0", 1)
	ssPod.Labels[statefulSetPodLabel] = "coder-0"
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS}}
	p, client, _ := bindProvisioner(t, ssPod, sa)
	if err := p.RetireServiceAccount(t.Context(), at, scaleTestNS); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().ServiceAccounts(scaleTestNS).Get(t.Context(), at.Slug, metav1.GetOptions{}); err != nil {
		t.Fatalf("ServiceAccount deleted while a StatefulSet pod is left: %v", err)
	}
	if err := client.CoreV1().Pods(scaleTestNS).Delete(t.Context(), "coder-0", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	waitCached(t, func() bool {
		_, found, _ := p.cachedAgentPod(scaleTestNS, "coder-0")
		return !found
	})
	if err := p.RetireServiceAccount(t.Context(), at, scaleTestNS); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().ServiceAccounts(scaleTestNS).Get(t.Context(), at.Slug, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("ServiceAccount after the StatefulSet pods are gone: %v, want not found", err)
	}
}

// A pool pod whose controller is not a live ReplicaSet of the tenant's pool Deployment is never bound.
func TestBindPodChecksPoolProvenance(t *testing.T) {
	for name, mutate := range map[string]func(f *poolFixture, pod *corev1.Pod){
		"no controller":            func(_ *poolFixture, pod *corev1.Pod) { pod.OwnerReferences = nil },
		"ReplicaSet UID differs":   func(f *poolFixture, _ *corev1.Pod) { f.rs.UID = "other-rs" },
		"ReplicaSet gone":          func(f *poolFixture, _ *corev1.Pod) { f.rs.Name = "other" },
		"another Deployment's set": func(f *poolFixture, _ *corev1.Pod) { f.rs.OwnerReferences[0].UID = "other-dep" },
		"Deployment gone":          func(f *poolFixture, _ *corev1.Pod) { f.dep.Name = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newPoolFixture()
			pod := f.pod("pool-a", testTemplate, 1)
			mutate(&f, pod)
			p, _, patches := bindProvisioner(t, f.dep, f.rs, pod)
			if _, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS); !errors.Is(err, errNoPoolPod) {
				t.Errorf("err = %v, want errNoPoolPod", err)
			}
			if len(patches) != 0 {
				t.Errorf("patches = %v, want none", patches)
			}
		})
	}
}

// AgentPods lists the live pods oldest first, leaves out a terminating pod, names the pods that exited, and
// carries the pool's current template.
func TestAgentPods(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	now := metav1.Now()
	terminating := boundPod(at, "pool-t", 0)
	terminating.DeletionTimestamp = &now
	evicted := boundPod(at, "pool-e", 1)
	evicted.Status.Phase = corev1.PodFailed
	starting := boundPod(at, "pool-s", 4)
	starting.Status.Conditions, starting.Status.PodIP = nil, ""
	old := boundPod(at, "pool-o", 2)
	old.Annotations[poolTemplateAnnotation] = "t0"
	model := "anthropic/claude-sonnet-5"
	changed := AgentType{ID: at.ID, Slug: at.Slug, TenantID: at.TenantID, ModelID: &model}
	p, _, _ := bindProvisioner(t, newPoolFixture().dep, terminating, evicted, starting, boundPod(at, "pool-n", 3), old)
	pods, err := p.AgentPods(t.Context(), at.Slug, scaleTestNS)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, pod := range pods.Live {
		names = append(names, pod.Name)
	}
	if !slices.Equal(names, []string{"pool-o", "pool-n", "pool-s"}) || !slices.Equal(pods.Finished, []string{"pool-e"}) {
		t.Errorf("live %v and finished %v, want [pool-o pool-n pool-s] and [pool-e]", names, pods.Finished)
	}
	if pods.Template != testTemplate || pods.Live[2].Ready || pods.Live[1].IP != "10.0.0.1" {
		t.Errorf("pods = %+v, want the current template, pool-s not Ready and pool-n's IP", pods)
	}
	config := agentConfigHash(at)
	if old := pods.old(config); len(old) != 1 || old[0].Name != "pool-o" || !pods.rolling(config) {
		t.Errorf("old pods = %+v, want pool-o on the old template", old)
	}
	if len(pods.old(agentConfigHash(changed))) != 3 {
		t.Errorf("old pods after a model change = %+v, want all three", pods.old(agentConfigHash(changed)))
	}
	if ready := pods.ready(); len(ready) != 2 {
		t.Errorf("ready = %+v, want pool-o and pool-n", ready)
	}
}

// PodIP reads the cache and falls back to the API server for a pod the cache does not hold yet.
// PodLive reads the API server.
func TestPodIPAndPodLive(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	p, client, _ := bindProvisioner(t, boundPod(at, "pool-a", 1))
	if ip, err := p.PodIP(t.Context(), scaleTestNS, "pool-a"); err != nil || ip != "10.0.0.1" {
		t.Errorf("PodIP = %q, %v; want the cached 10.0.0.1", ip, err)
	}
	late := boundPod(at, "pool-b", 2)
	late.Labels = nil
	late.Status.PodIP = "10.0.0.2"
	if _, err := client.CoreV1().Pods(scaleTestNS).Create(t.Context(), late, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if ip, err := p.PodIP(t.Context(), scaleTestNS, "pool-b"); err != nil || ip != "10.0.0.2" {
		t.Errorf("PodIP = %q, %v; want the live 10.0.0.2", ip, err)
	}
	if _, err := p.PodIP(t.Context(), scaleTestNS, "gone"); err == nil {
		t.Error("PodIP of a missing pod succeeded")
	}
	if live, err := p.PodLive(t.Context(), scaleTestNS, "pool-a"); err != nil || !live {
		t.Errorf("PodLive(pool-a) = %v, %v; want true", live, err)
	}
	if live, err := p.PodLive(t.Context(), scaleTestNS, "gone"); err != nil || live {
		t.Errorf("PodLive(gone) = %v, %v; want false", live, err)
	}
}

// An apply deletes the workload an earlier release ran for the agent, and leaves the pool alone.
func TestProvisionRetiresTheStatefulSet(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	meta := metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS, Labels: agentTypeLabels(at)}
	claim := meta
	claim.Name = "workspaces-coder-0"
	legacy := []runtime.Object{
		&appsv1.StatefulSet{ObjectMeta: meta}, &policyv1.PodDisruptionBudget{ObjectMeta: meta},
		&corev1.ServiceAccount{ObjectMeta: meta}, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: meta},
		&corev1.PersistentVolumeClaim{ObjectMeta: claim},
	}
	for _, update := range []bool{false, true} {
		p, client, _ := ownerRefFixture(t, podSecurityProvisioner().cfg, "acme")
		for _, obj := range legacy {
			if err := client.Tracker().Add(obj.DeepCopyObject()); err != nil {
				t.Fatal(err)
			}
		}
		if err := p.provision(t.Context(), at, scaleTestNS, update); err != nil {
			t.Fatal(err)
		}
		_, ssErr := client.AppsV1().StatefulSets(scaleTestNS).Get(t.Context(), at.Slug, metav1.GetOptions{})
		_, pdbErr := client.PolicyV1().PodDisruptionBudgets(scaleTestNS).Get(t.Context(), at.Slug, metav1.GetOptions{})
		_, saErr := client.CoreV1().ServiceAccounts(scaleTestNS).Get(t.Context(), at.Slug, metav1.GetOptions{})
		_, hpaErr := client.AutoscalingV2().HorizontalPodAutoscalers(scaleTestNS).Get(t.Context(), at.Slug, metav1.GetOptions{})
		_, pvcErr := client.CoreV1().PersistentVolumeClaims(scaleTestNS).Get(t.Context(), claim.Name, metav1.GetOptions{})
		if saErr != nil {
			t.Errorf("update %v: the apply deleted the ServiceAccount the StatefulSet pods still use: %v", update, saErr)
		}
		for name, err := range map[string]error{"statefulset": ssErr, "pdb": pdbErr, "hpa": hpaErr, "claim": pvcErr} {
			if gone := apierrors.IsNotFound(err); gone != update {
				t.Errorf("update %v: %s gone = %v, want %v", update, name, gone, update)
			}
		}
		if _, err := client.CoreV1().ServiceAccounts(scaleTestNS).Get(t.Context(), poolName, metav1.GetOptions{}); err != nil {
			t.Errorf("update %v: pool serviceaccount: %v", update, err)
		}
	}
}
