package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	sandboxfolder "github.com/clear-route/orbit/internal/agents/sandbox/folder"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func sandboxPod(annoKey, annoVal string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "sandbox-run-1",
			Namespace:   "ns",
			Annotations: map[string]string{annoKey: annoVal},
			Labels:      map[string]string{sandboxfolder.RunIDLabel: "run-1"},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

// withSandboxExec gives the pod a sandbox-exec container with a memory limit and the given state, beside a running sidecar.
func withSandboxExec(pod *corev1.Pod, memory string, state corev1.ContainerState) *corev1.Pod {
	pod.Spec.Containers = []corev1.Container{
		{Name: sandboxContainerName, Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(memory)}}},
		{Name: "istio-proxy"},
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{Name: sandboxContainerName, State: state},
		{Name: "istio-proxy", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
	}
	return pod
}

// withFolderMount mounts the sandbox folder into the pod's sandbox-exec container.
func withFolderMount(pod *corev1.Pod) *corev1.Pod {
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: sandboxfolder.Volume, SubPath: sandboxfolder.Dir, MountPath: "/workspaces/run-1/sandbox"}}
	return pod
}

var (
	execRunning = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	execOOM     = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}
	execErrored = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1}}
)

func TestReplaceIfDeadSandboxPod(t *testing.T) {
	terminating := sandboxPod("marker", "old", corev1.PodRunning)
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"kubernetes"} // required for a DeletionTimestamp to be valid

	tests := []struct {
		name         string
		existing     *corev1.Pod
		desired      *corev1.Pod
		wantReplaced bool
		wantOOM      string
	}{
		{
			name:     "running sandbox-exec is reused",
			existing: withSandboxExec(sandboxPod("marker", "old", corev1.PodRunning), "2Gi", execRunning),
			desired:  withSandboxExec(sandboxPod("marker", "new", corev1.PodPending), "2Gi", corev1.ContainerState{}),
		},
		{
			name:     "pending pod without container statuses is reused",
			existing: sandboxPod("marker", "old", corev1.PodPending),
			desired:  sandboxPod("marker", "new", corev1.PodPending),
		},
		{
			name:         "failed pod is replaced",
			existing:     sandboxPod("marker", "old", corev1.PodFailed),
			desired:      sandboxPod("marker", "new", corev1.PodPending),
			wantReplaced: true,
		},
		{
			name:         "succeeded pod is replaced",
			existing:     sandboxPod("marker", "old", corev1.PodSucceeded),
			desired:      sandboxPod("marker", "new", corev1.PodPending),
			wantReplaced: true,
		},
		{
			name:         "terminating pod is replaced",
			existing:     terminating,
			desired:      sandboxPod("marker", "new", corev1.PodPending),
			wantReplaced: true,
		},
		{
			name:         "exited sandbox-exec with running sidecars is replaced",
			existing:     withSandboxExec(sandboxPod("marker", "old", corev1.PodRunning), "2Gi", execErrored),
			desired:      withSandboxExec(sandboxPod("marker", "new", corev1.PodPending), "2Gi", corev1.ContainerState{}),
			wantReplaced: true,
		},
		{
			name:         "OOMKilled sandbox-exec is replaced and reported at the same size",
			existing:     withSandboxExec(sandboxPod("marker", "old", corev1.PodRunning), "2Gi", execOOM),
			desired:      withSandboxExec(sandboxPod("marker", "new", corev1.PodPending), "2Gi", corev1.ContainerState{}),
			wantReplaced: true,
			wantOOM:      "at its 2Gi limit and was restarted. Files outside /workspaces/run-1 are lost.",
		},
		{
			name:         "OOMKilled sandbox-exec with a sandbox folder reports the folder lost",
			existing:     withFolderMount(withSandboxExec(sandboxPod("marker", "old", corev1.PodRunning), "2Gi", execOOM)),
			desired:      withSandboxExec(sandboxPod("marker", "new", corev1.PodPending), "2Gi", corev1.ContainerState{}),
			wantReplaced: true,
			wantOOM:      "The sandbox folder /workspaces/run-1/sandbox was lost with it. Other files under /workspaces/run-1 are intact.",
		},
		{
			name:         "OOMKilled sandbox-exec replaced by a larger one is not reported",
			existing:     withSandboxExec(sandboxPod("marker", "old", corev1.PodRunning), "2Gi", execOOM),
			desired:      withSandboxExec(sandboxPod("marker", "new", corev1.PodPending), "4Gi", corev1.ContainerState{}),
			wantReplaced: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing)
			p := &K8sProvisioner{client: client, logger: logging.NewLoggerWithOutput(io.Discard)}

			replaced, err := p.replaceIfDeadSandboxPod(context.Background(), tt.desired, "ns")
			if tt.wantOOM != "" {
				if !errors.Is(err, ErrSandboxOutOfMemory) || !strings.Contains(err.Error(), tt.wantOOM) {
					t.Fatalf("err = %v, want ErrSandboxOutOfMemory containing %q", err, tt.wantOOM)
				}
			} else if err != nil {
				t.Fatalf("replaceIfDeadSandboxPod: %v", err)
			}
			if replaced != tt.wantReplaced {
				t.Fatalf("replaced = %v, want %v", replaced, tt.wantReplaced)
			}
			got, err := client.CoreV1().Pods("ns").Get(context.Background(), "sandbox-run-1", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			want := "old"
			if tt.wantReplaced {
				want = "new"
			}
			if got.Annotations["marker"] != want {
				t.Fatalf("marker = %q, want %q", got.Annotations["marker"], want)
			}
		})
	}
}

func TestWriteEnvironmentChoiceError_OutOfMemoryReachesAgent(t *testing.T) {
	c := &Controller{logger: logging.NewLoggerWithOutput(io.Discard)}
	w := httptest.NewRecorder()
	err := fmt.Errorf("provision sandbox pod: %w at its 2Gi limit", ErrSandboxOutOfMemory)

	if !c.writeEnvironmentChoiceError(context.Background(), w, err) {
		t.Fatal("expected ErrSandboxOutOfMemory to be answered")
	}
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "ran out of memory at its 2Gi limit") {
		t.Fatalf("got %d %s, want 409 carrying the out-of-memory message", w.Code, w.Body.String())
	}
}
