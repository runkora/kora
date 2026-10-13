package agents

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Each agent starts with the configured pods, the HPA keeps that floor, and a
// terminating pod gets the drain grace period.
func TestSessionManagerScaleAndDrain(t *testing.T) {
	p := podSecurityProvisioner()
	p.cfg.MinReplicas = 2
	p.cfg.DrainGracePeriodSecs = 3600
	at := AgentType{Slug: "coder", TenantID: "acme"}

	ss := p.buildSessionManagerStatefulSet(at, "orbit-tenant-acme", podLabels(at))
	if *ss.Spec.Replicas != 2 || ss.Spec.Template.Spec.TerminationGracePeriodSeconds == nil || *ss.Spec.Template.Spec.TerminationGracePeriodSeconds != 3600 {
		t.Errorf("replicas = %d, grace = %v, want 2 and 3600", *ss.Spec.Replicas, ss.Spec.Template.Spec.TerminationGracePeriodSeconds)
	}
	if hpa := p.buildHPA(at.Slug, "orbit-tenant-acme", nil); *hpa.Spec.MinReplicas != 2 {
		t.Errorf("HPA minimum = %d, want 2", *hpa.Spec.MinReplicas)
	}
}

// The headless Service keeps a draining pod in DNS, including on an existing Service.
func TestHeadlessService_PublishesNotReadyAddresses(t *testing.T) {
	const ns, name = "orbit-tenant-acme", "coder"
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       corev1.ServiceSpec{ClusterIP: "None"},
	})

	if err := p.ensureHeadlessService(context.Background(), name, ns, map[string]string{"app": name}, metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureHeadlessService: %v", err)
	}
	got, err := p.client.CoreV1().Services(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Spec.PublishNotReadyAddresses {
		t.Error("an existing headless Service did not get publishNotReadyAddresses")
	}
}

// Deleting an agent deletes its pods instead of leaving them to drain.
func TestDeprovision_DeletesAgentPods(t *testing.T) {
	const ns = "orbit-tenant-acme"
	at := AgentType{Slug: "coder", TenantID: "acme"}
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "coder-0", Namespace: ns, Labels: podLabels(at)}})

	if err := p.deleteAgentPods(context.Background(), at, ns); err != nil {
		t.Fatalf("deleteAgentPods: %v", err)
	}
	if pods, _ := p.client.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{}); len(pods.Items) != 0 {
		t.Errorf("agent pods left = %d, want 0", len(pods.Items))
	}
	for _, a := range p.client.(*fake.Clientset).Actions() {
		del, ok := a.(k8stesting.DeleteAction)
		if !ok {
			continue
		}
		if g := del.GetDeleteOptions().GracePeriodSeconds; g == nil || *g != deletedAgentPodGrace {
			t.Errorf("pod deleted with grace %v, want %d", g, deletedAgentPodGrace)
		}
	}
}
