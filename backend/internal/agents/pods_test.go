package agents

import (
	"context"
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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
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

// A patch an admission policy refuses as forbidden or invalid fails the bind with that error, records it on
// the span and counts it. No other pool pod is tried.
func TestBindPodReturnsARefusedPatch(t *testing.T) {
	for name, refusal := range map[string]error{
		"forbidden": apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "pool-a", errors.New("kora-agent-bind")),
		"invalid":   apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "pool-a", nil),
	} {
		t.Run(name, func(t *testing.T) {
			f := newPoolFixture()
			p, client, _ := bindProvisioner(t, f.dep, f.rs, f.pod("pool-a", testTemplate, 1), f.pod("pool-b", testTemplate, 2))
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			m, err := NewMetrics(provider.Meter("test"))
			if err != nil {
				t.Fatal(err)
			}
			p.SetMetrics(m)
			patched := 0
			client.PrependReactor("patch", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() != "" {
					return false, nil, nil
				}
				patched++
				return true, nil, refusal
			})
			name, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS)
			if name != "" || !errors.Is(err, refusal) || patched != 1 {
				t.Errorf("BindPod = %q, %v after %d patches; want the refusal after one patch", name, err, patched)
			}
			counted, ok := collectRunMetrics(t, reader)["kora.agents.bind.refused"].Data.(metricdata.Sum[int64])
			if !ok || len(counted.DataPoints) != 1 || counted.DataPoints[0].Value != 1 {
				t.Errorf("bind.refused = %#v, want one point of 1", counted)
			}
		})
	}
}

// The bind span records a failure with a static description: no pool pod to bind, a provenance read
// that failed and a grow the API server refused.
func TestBindPodSpanStatus(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	for name, tc := range map[string]struct {
		setup func(f poolFixture, client *fake.Clientset) []runtime.Object
		want  error
	}{
		"no pool pod": {want: errNoPoolPod},
		"provenance read": {want: errSpanCheckPoolProvenance, setup: func(f poolFixture, client *fake.Clientset) []runtime.Object {
			client.PrependReactor("get", "replicasets", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("api down")
			})
			return nil
		}},
		"grow": {want: errSpanGrowBoundPod, setup: func(f poolFixture, client *fake.Clientset) []runtime.Object {
			client.PrependReactor("patch", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() == "resize" {
					return true, nil, errors.New("resize unavailable")
				}
				return false, nil, nil
			})
			return nil
		}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := spanRecorder()
			f := newPoolFixture()
			objs := []runtime.Object{f.dep, f.rs}
			if name != "no pool pod" {
				objs = append(objs, f.pod("pool-a", testTemplate, 1))
			}
			p, client, _ := bindProvisioner(t, objs...)
			if tc.setup != nil {
				tc.setup(f, client)
			}
			if _, err := p.BindPod(t.Context(), at, scaleTestNS); err == nil {
				t.Fatal("BindPod succeeded")
			}
			span := endedSpan(t, rec, "agents.K8sProvisioner.BindPod")
			if span.Status().Description != tc.want.Error() {
				t.Errorf("span status = %q, want %q", span.Status().Description, tc.want.Error())
			}
		})
	}
}

// agentConfigHash is the first 10 hex digits of the SHA-256 of the framework, a NUL byte and the model.
func TestAgentConfigHash(t *testing.T) {
	model := "anthropic/claude-haiku-4-5"
	if got := agentConfigHash(AgentType{Framework: "claude-code", ModelID: &model}); got != "9c653a787f" {
		t.Errorf("hash = %s, want 9c653a787f", got)
	}
	if got := agentConfigHash(AgentType{}); got != "6e340b9cff" {
		t.Errorf("hash of no framework or model = %s, want 6e340b9cff", got)
	}
}

// A provenance check the API server cannot answer fails the bind.
func TestBindPodReturnsAFailedProvenanceRead(t *testing.T) {
	f := newPoolFixture()
	p, client, patches := bindProvisioner(t, f.dep, f.rs, f.pod("pool-a", testTemplate, 1))
	client.PrependReactor("get", "replicasets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api down")
	})
	if _, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS); err == nil || errors.Is(err, errNoPoolPod) || len(patches) != 0 {
		t.Errorf("BindPod = %v with patches %v, want the read error and no patch", err, patches)
	}
}

// With no live bound pod, a pool pod coming up keeps a run queued past the start budget.
func TestLivePodCountsPoolPods(t *testing.T) {
	f := newPoolFixture()
	stuck := f.pod("pool-s", testTemplate, 1)
	stuck.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: sessionManagerContainerName, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}}
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	stuckBound := boundPod(at, "pool-b", 1)
	stuckBound.Status.ContainerStatuses = stuck.Status.ContainerStatuses
	stuckOther := boundPod(AgentType{ID: "at-old", Slug: "coder", TenantID: "acme"}, "pool-c", 1)
	stuckOther.Status.ContainerStatuses = stuck.Status.ContainerStatuses
	statefulSetPod := boundPod(at, "coder-0", 1)
	statefulSetPod.Labels[statefulSetPodLabel] = "coder-0"
	for name, tc := range map[string]struct {
		objs []runtime.Object
		want bool
	}{
		"no pool pod":      {nil, false},
		"a stuck pool pod": {[]runtime.Object{stuck}, false},
		"a pool pod up":    {[]runtime.Object{f.pod("pool-a", testTemplate, 2)}, true},
		// The stuck bound pod keeps placement from binding the pool pod.
		"a stuck bound pod beside a pool pod up": {[]runtime.Object{stuckBound, f.pod("pool-a", testTemplate, 2)}, false},
		// A pod bound to a deleted agent with the same slug, or a StatefulSet pod, is not the agent's.
		"another agent record's stuck pod": {[]runtime.Object{stuckOther, f.pod("pool-a", testTemplate, 2)}, true},
		"a StatefulSet pod up":             {[]runtime.Object{statefulSetPod}, false},
	} {
		t.Run(name, func(t *testing.T) {
			p := podSecurityProvisioner()
			p.client = fake.NewSimpleClientset(tc.objs...)
			if got, err := p.LivePod(t.Context(), at, scaleTestNS); err != nil || got != tc.want {
				t.Errorf("LivePod = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// Deleting an agent deletes its ServiceAccount at once and clears its retired entry.
func TestDeprovisionDeletesTheServiceAccount(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	p, typed, _ := ownerRefFixture(t, podSecurityProvisioner().cfg, "acme")
	if err := typed.Tracker().Add(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS, Labels: agentTypeLabels(at)}}); err != nil {
		t.Fatal(err)
	}
	p.retiredAccounts.Store(scaleTestNS+"/"+at.Slug, struct{}{})
	if err := p.DeprovisionAgentType(t.Context(), at, scaleTestNS); err != nil {
		t.Fatal(err)
	}
	if _, err := typed.CoreV1().ServiceAccounts(scaleTestNS).Get(t.Context(), at.Slug, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("ServiceAccount after deprovision: %v, want not found", err)
	}
	if _, kept := p.retiredAccounts.Load(scaleTestNS + "/" + at.Slug); kept {
		t.Error("the retired entry survived deprovision")
	}
}

// The agent ServiceAccount an earlier release created goes once no StatefulSet pod of the agent is left.
func TestRetireServiceAccount(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	ssPod := boundPod(at, "coder-0", 1)
	ssPod.Labels[statefulSetPodLabel] = "coder-0"
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS, UID: "sa-uid", Labels: agentTypeLabels(at)}}
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

// A ServiceAccount named after the slug that the agent's earlier release did not create, such as default or one
// of another agent record with the same slug, is never deleted.
func TestRetireServiceAccountKeepsAnotherAccount(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "default", TenantID: "acme"}
	for name, labels := range map[string]map[string]string{
		"no agent label":     nil,
		"another agent's ID": agentTypeLabels(AgentType{ID: "at-old", Slug: at.Slug, TenantID: "acme"}),
	} {
		t.Run(name, func(t *testing.T) {
			sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS, Labels: labels}}
			p, client, _ := bindProvisioner(t, sa)
			if err := p.RetireServiceAccount(t.Context(), at, scaleTestNS); err != nil {
				t.Fatal(err)
			}
			if err := p.DeprovisionAgentType(t.Context(), at, scaleTestNS); err != nil {
				t.Fatal(err)
			}
			if _, err := client.CoreV1().ServiceAccounts(scaleTestNS).Get(t.Context(), at.Slug, metav1.GetOptions{}); err != nil {
				t.Errorf("ServiceAccount: %v, want it kept", err)
			}
		})
	}
}

// The ServiceAccount delete carries the UID that the label check read as its precondition.
func TestRetireServiceAccountPreconditionsTheUID(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS, UID: "sa-uid", Labels: agentTypeLabels(at)}}
	p, client, _ := bindProvisioner(t, sa)
	var uid *types.UID
	client.PrependReactor("delete", "serviceaccounts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if pre := action.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions; pre != nil {
			uid = pre.UID
		}
		return false, nil, nil
	})
	if err := p.RetireServiceAccount(t.Context(), at, scaleTestNS); err != nil {
		t.Fatal(err)
	}
	if uid == nil || *uid != "sa-uid" {
		t.Errorf("delete precondition UID = %v, want sa-uid", uid)
	}
}

// With no pool Deployment in the cache, the bind compares the pod's template with the live Deployment's.
func TestBindPodChecksTheLiveTemplate(t *testing.T) {
	for name, tc := range map[string]struct {
		template string
		want     string
	}{
		"same template":  {testTemplate, "pool-a"},
		"stale template": {"t-new", ""},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPoolFixture()
			p, client, _ := bindProvisioner(t, f.rs, f.pod("pool-a", testTemplate, 1))
			live := f.dep.DeepCopy()
			live.Spec.Template.Annotations[poolTemplateAnnotation] = tc.template
			client.PrependReactor("get", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, live, nil
			})
			name, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS)
			if name != tc.want || (tc.want == "" && !errors.Is(err, errNoPoolPod)) {
				t.Errorf("BindPod = %q, %v; want %q", name, err, tc.want)
			}
		})
	}
}

// The provenance check reads the pool Deployment's UID from the cache and makes no Deployment GET.
func TestBindPodReadsTheCachedDeployment(t *testing.T) {
	f := newPoolFixture()
	p, client, _ := bindProvisioner(t, f.dep, f.rs, f.pod("pool-a", testTemplate, 1))
	if _, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS); err != nil {
		t.Fatal(err)
	}
	for _, a := range client.Actions() {
		if a.GetVerb() == "get" && a.GetResource().Resource == "deployments" {
			t.Errorf("BindPod made a live Deployment GET: %v", a)
		}
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

// AgentPods lists the live pods oldest first, leaves out a terminating pod, a pod bound to another agent record
// with the same slug and a StatefulSet pod, names the pods that exited, and carries the pool's current template.
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
	leaked := boundPod(AgentType{ID: "at-old", Slug: at.Slug, TenantID: at.TenantID}, "pool-l", 0)
	statefulSetPod := boundPod(at, "coder-0", 0)
	statefulSetPod.Labels[statefulSetPodLabel] = "coder-0"
	p, _, _ := bindProvisioner(t, newPoolFixture().dep, terminating, evicted, starting, boundPod(at, "pool-n", 3), old, leaked, statefulSetPod)
	pods, err := p.AgentPods(t.Context(), at, scaleTestNS)
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
	if ready := readyOf(pods.Live); len(ready) != 2 {
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

// A bind whose KoraTenant read fails makes no patch, so no bound pod is left unowned.
func TestBindPodRefusesAFailedTenantRead(t *testing.T) {
	f := newPoolFixture()
	p, _, patches := bindProvisioner(t, f.dep, f.rs, f.pod("pool-a", testTemplate, 1))
	dyn := ownerRefDynamicClient("acme")
	dyn.PrependReactor("get", "koratenants", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api down")
	})
	p.dynamicClient = dyn
	if name, err := p.BindPod(t.Context(), AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}, scaleTestNS); err == nil || name != "" || len(patches) != 0 {
		t.Errorf("BindPod = %q, %v with patches %v; want the read error and no patch", name, err, patches)
	}
}

// failingBound answers every bound check with err.
type failingBound struct{ err error }

func (b failingBound) Bound(context.Context, sessionmanager.Pod) (bool, error) { return false, b.err }

// A bound check that keeps failing names its last error in the timeout.
func TestWaitBoundKeepsTheLastError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	refused := errors.New("status 403")
	err := waitBound(ctx, failingBound{refused}, sessionmanager.PodAt("10.0.0.1", "coder", scaleTestNS))
	if !errors.Is(err, errNotBound) || !errors.Is(err, refused) {
		t.Errorf("waitBound = %v, want errNotBound wrapping the last check error", err)
	}
}
