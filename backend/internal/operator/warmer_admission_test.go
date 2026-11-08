package operator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	authnv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const warmerTestNS = "kora-tenant-example"

func warmerTestDaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandbox-image-kora-base", Namespace: warmerTestNS, UID: types.UID("ds-uid"),
			Labels: map[string]string{
				"app.kubernetes.io/component":  warmerComponent,
				"app.kubernetes.io/managed-by": "kora-operator",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: si.GroupVersion.String(), Kind: "SandboxImage", Name: si.BaseName,
				UID: types.UID("img-uid"), Controller: new(true),
			}},
		},
	}
}

func warmerTestPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandbox-image-kora-base-abcde", Namespace: warmerTestNS,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "DaemonSet", Name: "sandbox-image-kora-base",
				UID: types.UID("ds-uid"), Controller: new(true),
			}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "warm", Image: "kora-base@sha256:abc"}}},
	}
}

func warmerTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	ns := tenantNS(map[string]string{"istio-injection": "enabled", "runkora.io/tenant": "example"})
	return fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(append(objs, ns)...).Build()
}

func podCreateRequest(t *testing.T, pod *corev1.Pod, user string) admission.Request {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Namespace: warmerTestNS,
		Object:    runtime.RawExtension{Raw: raw},
		UserInfo:  authnv1.UserInfo{Username: user},
	}}
}

// A warmer is recognized by its creator and its controlling DaemonSet, never by
// what the pod declares. kora-agents can create pods with any labels and owner
// references in a tenant namespace, so every case that drops one part of the
// chain must read as an ordinary pod.
func TestIsSandboxWarmer(t *testing.T) {
	cases := []struct {
		name      string
		user      string
		pod       func(*corev1.Pod)
		ds        func(*appsv1.DaemonSet)
		noDS      bool
		wantMatch bool
	}{
		{name: "created by the DaemonSet controller from a warmer DaemonSet", user: daemonSetControllerUser, wantMatch: true},
		{name: "created by kora-agents with a forged owner reference", user: "system:serviceaccount:kora-system:kora-agents"},
		{name: "no controlling owner", user: daemonSetControllerUser, pod: func(p *corev1.Pod) { p.OwnerReferences = nil }},
		{name: "owner is not a DaemonSet", user: daemonSetControllerUser, pod: func(p *corev1.Pod) { p.OwnerReferences[0].Kind = "ReplicaSet" }},
		{name: "owner UID does not match the DaemonSet", user: daemonSetControllerUser, pod: func(p *corev1.Pod) { p.OwnerReferences[0].UID = "other" }},
		{name: "DaemonSet does not exist", user: daemonSetControllerUser, noDS: true},
		{name: "DaemonSet is not a warmer", user: daemonSetControllerUser, ds: func(d *appsv1.DaemonSet) { d.Labels["app.kubernetes.io/component"] = "other" }},
		{name: "DaemonSet is not managed by the operator", user: daemonSetControllerUser, ds: func(d *appsv1.DaemonSet) { d.Labels["app.kubernetes.io/managed-by"] = "argocd" }},
		{name: "DaemonSet is not controlled by a SandboxImage", user: daemonSetControllerUser, ds: func(d *appsv1.DaemonSet) { d.OwnerReferences = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod, ds := warmerTestPod(), warmerTestDaemonSet()
			if tc.pod != nil {
				tc.pod(pod)
			}
			if tc.ds != nil {
				tc.ds(ds)
			}
			var objs []client.Object
			if !tc.noDS {
				objs = append(objs, ds)
			}
			got, err := isSandboxWarmer(context.Background(), warmerTestClient(t, objs...), podCreateRequest(t, pod, tc.user), pod)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantMatch {
				t.Fatalf("isSandboxWarmer = %v, want %v", got, tc.wantMatch)
			}
		})
	}
}

// A verified warmer gets the install's pull secrets and an Istio opt-out, and none of kora's sidecars.
func TestMutatorHandle_Warmer(t *testing.T) {
	m := testMutator()
	m.Client = warmerTestClient(t, warmerTestDaemonSet())
	m.Decoder = admission.NewDecoder(NewScheme())

	resp := m.Handle(context.Background(), podCreateRequest(t, warmerTestPod(), daemonSetControllerUser))
	if !resp.Allowed {
		t.Fatalf("denied: %v", resp.Result)
	}
	patch, err := json.Marshal(resp.Patches)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"sidecar.istio.io/inject":"false"`, `regcred`} {
		if !strings.Contains(string(patch), want) {
			t.Errorf("patch is missing %s: %s", want, patch)
		}
	}
	for _, unwanted := range []string{containerIdentityProxy, containerOPA, podLabelOPAPolicy} {
		if strings.Contains(string(patch), unwanted) {
			t.Errorf("patch adds %s to a warmer: %s", unwanted, patch)
		}
	}
}

// The same pod sent by any other user is injected like any tenant pod.
func TestMutatorHandle_ForgedWarmerGetsSidecars(t *testing.T) {
	m := testMutator()
	m.Client = warmerTestClient(t, warmerTestDaemonSet())
	m.Decoder = admission.NewDecoder(NewScheme())

	resp := m.Handle(context.Background(), podCreateRequest(t, warmerTestPod(), "system:serviceaccount:kora-system:kora-agents"))
	if !resp.Allowed {
		t.Fatalf("denied: %v", resp.Result)
	}
	patch, err := json.Marshal(resp.Patches)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patch), containerIdentityProxy) || !strings.Contains(string(patch), containerOPA) {
		t.Fatalf("forged warmer was not injected: %s", patch)
	}
}

// The validator admits a verified warmer with no sidecars, and denies the same pod from any other user.
func TestValidatorHandle_Warmer(t *testing.T) {
	v := &PodValidator{Client: warmerTestClient(t, warmerTestDaemonSet()), Decoder: admission.NewDecoder(NewScheme())}

	if resp := v.Handle(context.Background(), podCreateRequest(t, warmerTestPod(), daemonSetControllerUser)); !resp.Allowed {
		t.Fatalf("warmer denied: %v", resp.Result)
	}
	resp := v.Handle(context.Background(), podCreateRequest(t, warmerTestPod(), "system:serviceaccount:kora-system:kora-agents"))
	if resp.Allowed {
		t.Fatal("pod without sidecars from kora-agents was admitted")
	}
}
