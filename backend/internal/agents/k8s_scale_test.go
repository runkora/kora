package agents

import (
	"context"
	"errors"
	"maps"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
