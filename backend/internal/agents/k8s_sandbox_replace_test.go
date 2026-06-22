package agents

import (
	"context"
	"io"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func sandboxPod(annoKey, annoVal string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "sandbox-run-1",
			Namespace:   "ns",
			Annotations: map[string]string{annoKey: annoVal},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

func TestReplaceIfDeadSandboxPod_LivePodReused(t *testing.T) {
	existing := sandboxPod("marker", "old", corev1.PodRunning)
	client := fake.NewSimpleClientset(existing)
	p := &K8sProvisioner{client: client, logger: logging.NewLoggerWithOutput(io.Discard)}

	desired := sandboxPod("marker", "new", corev1.PodRunning)
	replaced, err := p.replaceIfDeadSandboxPod(context.Background(), desired, "ns")
	if err != nil {
		t.Fatalf("replaceIfDeadSandboxPod: %v", err)
	}
	if replaced {
		t.Fatal("expected a live pod to be reused, not replaced")
	}
	got, err := client.CoreV1().Pods("ns").Get(context.Background(), "sandbox-run-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Annotations["marker"] != "old" {
		t.Fatalf("expected live pod left untouched (marker=old), got marker=%q", got.Annotations["marker"])
	}
}

func TestReplaceIfDeadSandboxPod_DeadPodRecreated(t *testing.T) {
	for _, phase := range []corev1.PodPhase{corev1.PodFailed, corev1.PodSucceeded} {
		t.Run(string(phase), func(t *testing.T) {
			existing := sandboxPod("marker", "old", phase)
			client := fake.NewSimpleClientset(existing)
			p := &K8sProvisioner{client: client, logger: logging.NewLoggerWithOutput(io.Discard)}

			desired := sandboxPod("marker", "new", corev1.PodPending)
			replaced, err := p.replaceIfDeadSandboxPod(context.Background(), desired, "ns")
			if err != nil {
				t.Fatalf("replaceIfDeadSandboxPod: %v", err)
			}
			if !replaced {
				t.Fatalf("expected a %s pod to be replaced", phase)
			}
			got, err := client.CoreV1().Pods("ns").Get(context.Background(), "sandbox-run-1", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Annotations["marker"] != "new" {
				t.Fatalf("expected dead pod recreated from desired spec (marker=new), got marker=%q", got.Annotations["marker"])
			}
		})
	}
}

func TestReplaceIfDeadSandboxPod_TerminatingPodRecreated(t *testing.T) {
	existing := sandboxPod("marker", "old", corev1.PodRunning)
	now := metav1.Now()
	existing.DeletionTimestamp = &now
	existing.Finalizers = []string{"kubernetes"} // required for a DeletionTimestamp to be valid
	client := fake.NewSimpleClientset(existing)
	p := &K8sProvisioner{client: client, logger: logging.NewLoggerWithOutput(io.Discard)}

	desired := sandboxPod("marker", "new", corev1.PodPending)
	replaced, err := p.replaceIfDeadSandboxPod(context.Background(), desired, "ns")
	if err != nil {
		t.Fatalf("replaceIfDeadSandboxPod: %v", err)
	}
	if !replaced {
		t.Fatal("expected a terminating pod to be replaced")
	}
}
