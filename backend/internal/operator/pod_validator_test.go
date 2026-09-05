package operator

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func injectedPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			"sidecar.istio.io/status": `{"initContainers":["istio-validation","istio-proxy"],"containers":null}`,
		}},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "istio-validation"}, {Name: "istio-proxy"}},
			Containers:     []corev1.Container{{Name: "sandbox-exec"}, {Name: containerIdentityProxy}, {Name: containerOPA}},
		},
	}
}

func tenantNS(labels map[string]string) *corev1.Namespace {
	labels["orbit.io/role"] = "tenant"
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "orbit-tenant-example", Labels: labels}}
}

func TestValidator_RejectionReasons(t *testing.T) {
	cases := []struct {
		name     string
		revision string
		ns       map[string]string
		mutate   func(*corev1.Pod)
		wantDeny string
	}{
		{"default revision ok", "", map[string]string{"istio-injection": "enabled"}, nil, ""},
		{"named revision ok", "canary", map[string]string{"istio.io/rev": "canary"}, nil, ""},
		{"namespace not labeled", "", map[string]string{}, nil, "not labeled for Istio injection"},
		{"namespace wrong revision", "canary", map[string]string{"istio-injection": "enabled"}, nil, "istio.io/rev=canary"},
		{"ambient namespace", "", map[string]string{"istio-injection": "enabled", "istio.io/dataplane-mode": "ambient"}, nil, "not labeled for Istio injection"},
		{"pod opts out", "", map[string]string{"istio-injection": "enabled"}, func(p *corev1.Pod) {
			p.Annotations["sidecar.istio.io/inject"] = "false"
		}, "opts out"},
		{"pod opts out by label", "", map[string]string{"istio-injection": "enabled"}, func(p *corev1.Pod) {
			p.Labels = map[string]string{"sidecar.istio.io/inject": "false"}
		}, "opts out"},
		{"self-named istio-proxy without injector status", "", map[string]string{"istio-injection": "enabled"}, func(p *corev1.Pod) {
			delete(p.Annotations, "sidecar.istio.io/status")
		}, "no sidecar.istio.io/status"},
		{"interception disabled", "", map[string]string{"istio-injection": "enabled"}, func(p *corev1.Pod) {
			p.Annotations["sidecar.istio.io/interceptionMode"] = "NONE"
		}, "interceptionMode=NONE"},
		{"no istio-proxy", "", map[string]string{"istio-injection": "enabled"}, func(p *corev1.Pod) {
			p.Spec.InitContainers = nil
		}, "no istio-proxy"},
		{"no identity-proxy", "", map[string]string{"istio-injection": "enabled"}, func(p *corev1.Pod) {
			p.Spec.Containers = []corev1.Container{{Name: "sandbox-exec"}, {Name: containerOPA}}
		}, "no identity-proxy"},
		{"no opa", "", map[string]string{"istio-injection": "enabled"}, func(p *corev1.Pod) {
			p.Spec.Containers = []corev1.Container{{Name: "sandbox-exec"}, {Name: containerIdentityProxy}}
		}, "no opa"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := &PodValidator{IstioRevision: tc.revision}
			pod := injectedPod()
			if tc.mutate != nil {
				tc.mutate(pod)
			}
			got := v.rejectionReason(pod, tenantNS(tc.ns))
			if tc.wantDeny == "" && got != "" {
				t.Fatalf("expected allow, got %q", got)
			}
			if tc.wantDeny != "" && !strings.Contains(got, tc.wantDeny) {
				t.Fatalf("expected denial containing %q, got %q", tc.wantDeny, got)
			}
		})
	}
}
