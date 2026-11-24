package agents

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// A terminating pool pod gets the drain grace period.
func TestSessionManagerDrain(t *testing.T) {
	p := podSecurityProvisioner()
	p.cfg.DrainGracePeriodSecs = 3600
	pool, err := p.buildTenantPool("acme", "kora-tenant-acme")
	if err != nil {
		t.Fatal(err)
	}
	if grace := pool.Spec.Template.Spec.TerminationGracePeriodSeconds; grace == nil || *grace != 3600 {
		t.Errorf("grace = %v, want 3600", grace)
	}
}

// The session-manager readiness probe runs at once and every second.
func TestSessionManagerReadinessProbe(t *testing.T) {
	p := podSecurityProvisioner()
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ss := p.buildSessionManagerStatefulSet(at, "kora-tenant-acme", podLabels(at))
	for _, c := range ss.Spec.Template.Spec.Containers {
		if c.Name != sessionManagerContainerName {
			continue
		}
		rp := c.ReadinessProbe
		if rp == nil || rp.InitialDelaySeconds != 0 || rp.PeriodSeconds != 1 || rp.TimeoutSeconds != 2 || rp.FailureThreshold != 10 || rp.HTTPGet == nil || rp.HTTPGet.Path != "/health" {
			t.Errorf("readiness probe = %+v, want GET /health at once and every second, 2s timeout, 10 failures", rp)
		}
		return
	}
	t.Fatal("no session-manager container")
}

// The headless Service keeps a draining pod in DNS, including on an existing Service.
func TestHeadlessService_PublishesNotReadyAddresses(t *testing.T) {
	const ns, name = "kora-tenant-acme", "coder"
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
	const ns = "kora-tenant-acme"
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
