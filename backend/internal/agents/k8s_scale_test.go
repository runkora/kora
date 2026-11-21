package agents

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const scaleTestNS = "kora-tenant-acme"

// liveStatefulSet is an agent StatefulSet as an earlier release left it.
func liveStatefulSet(at AgentType, replicas int32, policy appsv1.PodManagementPolicyType) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS, UID: types.UID("old-uid"), Labels: map[string]string{"app": at.Slug}},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            &replicas,
			PodManagementPolicy: policy,
			Selector:            &metav1.LabelSelector{MatchLabels: podLabels(at)},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: sessionManagerContainerName, Image: "live"}},
			}},
		},
	}
}

func TestBuildStatefulSetUsesParallel(t *testing.T) {
	p := podSecurityProvisioner()
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ss := p.buildSessionManagerStatefulSet(at, scaleTestNS, podLabels(at))
	if ss.Spec.PodManagementPolicy != appsv1.ParallelPodManagement {
		t.Errorf("podManagementPolicy = %q, want Parallel", ss.Spec.PodManagementPolicy)
	}
}

// Apply creates a missing StatefulSet at max(minReplicas, 1) and leaves a live one's replicas alone.
func TestEnsureStatefulSetReplicas(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		min  int32
		live *appsv1.StatefulSet
		want int32
	}{
		{"create at the floor", 2, nil, 2},
		{"create with a zero floor", 0, nil, 1},
		{"re-apply keeps the live replicas", 2, liveStatefulSet(at, 5, appsv1.ParallelPodManagement), 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := podSecurityProvisioner()
			p.cfg.MinReplicas = tc.min
			var objs []runtime.Object
			if tc.live != nil {
				objs = append(objs, tc.live)
			}
			client := fake.NewSimpleClientset(objs...)
			p.client = client
			if err := p.ensureStatefulSet(ctx, at, scaleTestNS, agentTypeLabels(at), metav1.OwnerReference{}, true); err != nil {
				t.Fatal(err)
			}
			got, err := client.AppsV1().StatefulSets(scaleTestNS).Get(ctx, at.Slug, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if *got.Spec.Replicas != tc.want {
				t.Errorf("replicas = %d, want %d", *got.Spec.Replicas, tc.want)
			}
			for _, a := range client.Actions() {
				if a.GetVerb() == "delete" {
					t.Errorf("apply deleted %s; a live StatefulSet is updated in place", a.GetResource().Resource)
				}
			}
		})
	}
}

func TestSwitchToParallel(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ctx := context.Background()

	t.Run("an OrderedReady StatefulSet is orphan-deleted and recreated", func(t *testing.T) {
		client := fake.NewSimpleClientset(liveStatefulSet(at, 4, appsv1.OrderedReadyPodManagement))
		p := &K8sProvisioner{client: client}
		if err := p.SwitchToParallel(ctx, at.Slug, scaleTestNS); err != nil {
			t.Fatal(err)
		}
		var deleted bool
		for _, a := range client.Actions() {
			if d, ok := a.(k8stesting.DeleteAction); ok {
				deleted = true
				opts := d.GetDeleteOptions()
				if opts.PropagationPolicy == nil || *opts.PropagationPolicy != metav1.DeletePropagationOrphan {
					t.Errorf("delete propagation = %v, want Orphan so the pods keep running", opts.PropagationPolicy)
				}
				if opts.Preconditions == nil || *opts.Preconditions.UID != "old-uid" {
					t.Errorf("delete preconditions = %v, want the live UID", opts.Preconditions)
				}
			}
		}
		if !deleted {
			t.Fatal("no delete")
		}
		got, err := client.AppsV1().StatefulSets(scaleTestNS).Get(ctx, at.Slug, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Spec.PodManagementPolicy != appsv1.ParallelPodManagement || *got.Spec.Replicas != 4 ||
			!maps.Equal(got.Spec.Selector.MatchLabels, podLabels(at)) || got.Labels["app"] != at.Slug {
			t.Errorf("recreated = policy %q, replicas %d, selector %v, labels %v; want Parallel, 4 and the live selector and labels",
				got.Spec.PodManagementPolicy, *got.Spec.Replicas, got.Spec.Selector.MatchLabels, got.Labels)
		}
	})

	t.Run("a Parallel StatefulSet is left alone", func(t *testing.T) {
		client := fake.NewSimpleClientset(liveStatefulSet(at, 3, appsv1.ParallelPodManagement))
		p := &K8sProvisioner{client: client}
		if err := p.SwitchToParallel(ctx, at.Slug, scaleTestNS); err != nil {
			t.Fatal(err)
		}
		for _, a := range client.Actions() {
			if a.GetVerb() != "get" {
				t.Errorf("unexpected %s", a.GetVerb())
			}
		}
	})
}

// scaleReactor serves the StatefulSet scale subresource from replicas and records updates.
func scaleReactor(client *fake.Clientset, replicas *int32) {
	client.PrependReactor("get", "statefulsets", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: *replicas}}, nil
	})
	client.PrependReactor("update", "statefulsets", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		scale := a.(k8stesting.UpdateAction).GetObject().(*autoscalingv1.Scale)
		*replicas = scale.Spec.Replicas
		return true, scale, nil
	})
}

func TestScaleStatefulSet(t *testing.T) {
	ctx := context.Background()
	replicas := int32(2)
	client := fake.NewSimpleClientset()
	scaleReactor(client, &replicas)
	p := &K8sProvisioner{client: client}

	if err := p.ScaleStatefulSet(ctx, "coder", scaleTestNS, 2, 5); err != nil || replicas != 5 {
		t.Fatalf("scale 2 to 5: err %v, replicas %d", err, replicas)
	}
	if err := p.ScaleStatefulSet(ctx, "coder", scaleTestNS, 2, 3); !errors.Is(err, errReplicasMoved) || replicas != 5 {
		t.Errorf("stale scale: err %v, replicas %d; want errReplicasMoved and no change", err, replicas)
	}
	// A concurrent change to the scale is a moved replica count, not a failure.
	client.PrependReactor("update", "statefulsets", func(a k8stesting.Action) (bool, runtime.Object, error) {
		return a.GetSubresource() == "scale", nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "statefulsets"}, "coder", errors.New("changed"))
	})
	if err := p.ScaleStatefulSet(ctx, "coder", scaleTestNS, 5, 6); !errors.Is(err, errReplicasMoved) {
		t.Errorf("conflicting scale: err %v, want errReplicasMoved", err)
	}
}

// The scaler's view counts the draining pods below the replicas and leaves them out of Ready.
func TestAgentStatefulSetCountsTerminatingPods(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	now := metav1.Now()
	pod := func(name string, terminating bool) *corev1.Pod {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: scaleTestNS, Labels: podLabels(at)},
			Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
		}
		if terminating {
			p.DeletionTimestamp = &now
		}
		return p
	}
	p := &K8sProvisioner{client: fake.NewSimpleClientset(liveStatefulSet(at, 2, appsv1.ParallelPodManagement),
		pod("coder-0", true), pod("coder-1", false), pod("coder-2", true))}
	view, found, err := p.AgentStatefulSet(context.Background(), at.Slug, scaleTestNS)
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if !slices.Equal(view.Ready, []int{1}) || view.Terminating != 1 {
		t.Errorf("ready %v, terminating %d; want [1] and 1: coder-2 is above the replicas", view.Ready, view.Terminating)
	}
}

// defaultTemplate stands in for API defaulting of a pod template.
func defaultTemplate(t *corev1.PodTemplateSpec) {
	for i := range t.Spec.Containers {
		t.Spec.Containers[i].TerminationMessagePath = corev1.TerminationMessagePathDefault
	}
}

// dryRunReactor answers a server-side dry run with the defaulted object without storing it, as the API
// server does, and counts the stored updates.
func dryRunReactor(client *fake.Clientset, writes *int) {
	client.PrependReactor("update", "statefulsets", func(a k8stesting.Action) (bool, runtime.Object, error) {
		u, ok := a.(k8stesting.UpdateActionImpl)
		if !ok || a.GetSubresource() != "" {
			return false, nil, nil
		}
		if len(u.GetUpdateOptions().DryRun) > 0 {
			dry := u.GetObject().DeepCopyObject().(*appsv1.StatefulSet)
			defaultTemplate(&dry.Spec.Template)
			return true, dry, nil
		}
		*writes++
		return false, nil, nil
	})
}

// An apply that changes the pod template adds one pod on the new revision and holds the old pods,
// in one write. At maxReplicas, or with an unchanged template, there is no hold.
func TestEnsureStatefulSetSurge(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ctx := context.Background()
	for _, tc := range []struct {
		name          string
		replicas      int32
		partition     int32
		unchanged     bool
		wantReplicas  int32
		wantPartition int32
	}{
		{"changed template below maxReplicas", 2, 0, false, 3, 2},
		{"changed template at maxReplicas", 4, 0, false, 4, 0},
		{"a hold at maxReplicas starts the roll", 4, 3, false, 4, 0},
		{"unchanged template", 2, 0, true, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := podSecurityProvisioner()
			p.cfg.MaxReplicas = 4
			live := liveStatefulSet(at, tc.replicas, appsv1.ParallelPodManagement)
			live.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{
				Type:          appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{Partition: &tc.partition},
			}
			if tc.unchanged {
				rendered, err := p.applyStatefulSetPatches(p.buildSessionManagerStatefulSet(at, scaleTestNS, agentTypeLabels(at)))
				if err != nil {
					t.Fatal(err)
				}
				live.Spec.Template = rendered.Spec.Template
				defaultTemplate(&live.Spec.Template)
			}
			client := fake.NewSimpleClientset(live)
			writes := 0
			dryRunReactor(client, &writes)
			p.client = client
			if err := p.ensureStatefulSet(ctx, at, scaleTestNS, agentTypeLabels(at), metav1.OwnerReference{}, true); err != nil {
				t.Fatal(err)
			}
			got, err := client.AppsV1().StatefulSets(scaleTestNS).Get(ctx, at.Slug, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if *got.Spec.Replicas != tc.wantReplicas || specPartition(got) != int(tc.wantPartition) || writes != 1 {
				t.Errorf("replicas %d, partition %d, writes %d; want %d, %d and 1 write",
					*got.Spec.Replicas, specPartition(got), writes, tc.wantReplicas, tc.wantPartition)
			}
		})
	}
}

// A hold the apply left is released by a scaler that never saw the apply, as after a restart, once the
// surge pod is Ready, and not before.
func TestRollHoldSurvivesRestart(t *testing.T) {
	at := AgentType{ID: "a", Slug: "coder", TenantID: "acme", MaxConcurrentRuns: 30}
	ctx := context.Background()
	p := podSecurityProvisioner()
	p.cfg.MaxReplicas = 10
	client := fake.NewSimpleClientset(liveStatefulSet(at, 1, appsv1.ParallelPodManagement))
	writes := 0
	dryRunReactor(client, &writes)
	p.client = client
	if err := p.ensureStatefulSet(ctx, at, scaleTestNS, agentTypeLabels(at), metav1.OwnerReference{}, true); err != nil {
		t.Fatal(err)
	}
	pod := func(name string, ready corev1.ConditionStatus) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: scaleTestNS, Labels: podLabels(at)},
			Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}}},
		}
	}
	if _, err := client.CoreV1().Pods(scaleTestNS).Create(ctx, pod("coder-0", corev1.ConditionTrue), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	surge, err := client.CoreV1().Pods(scaleTestNS).Create(ctx, pod("coder-1", corev1.ConditionFalse), metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	partition := func() int {
		ss, err := client.AppsV1().StatefulSets(scaleTestNS).Get(ctx, at.Slug, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return specPartition(ss)
	}
	f := &scalerFake{t: t, memory: fullPod}
	q := resource.MustParse("180Mi")
	scaler := NewScaler(f, p, f, ScalerConfig{
		TenantNamespaceFn: func(string) string { return scaleTestNS }, MinReplicas: 1, MaxReplicas: 10,
		PlacementWatermark: 0.70, StartingRunAllowances: NewStartingRunAllowances(q, q, q),
	}, p.logger)

	if err := scaler.scale(ctx, at); err != nil {
		t.Fatal(err)
	}
	if got := partition(); got != 1 {
		t.Fatalf("partition %d with the surge pod not Ready, want the hold of 1", got)
	}
	surge.Status.Conditions[0].Status = corev1.ConditionTrue
	if _, err := client.CoreV1().Pods(scaleTestNS).UpdateStatus(ctx, surge, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := scaler.scale(ctx, at); err != nil {
		t.Fatal(err)
	}
	if got := partition(); got != 0 {
		t.Errorf("partition %d with the surge pod Ready, want 0", got)
	}
}
