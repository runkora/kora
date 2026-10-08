package agents

import (
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	sandboxmount "github.com/clear-route/orbit/internal/agents/sandbox/mount"
)

func sandboxMountProvisioner(enabled bool) *K8sProvisioner {
	p := podSecurityProvisioner()
	p.cfg.SandboxMount = enabled
	return p
}

// With the node agent enabled, the sandbox names its session-manager pod and mounts its
// local disk's working folder inside the workspace.
func TestSandboxPod_SandboxMount(t *testing.T) {
	pod := buildTestSandboxPod(sandboxMountProvisioner(true), Run{ID: "run-1", PodOrdinal: 2}, "coder", "orbit-tenant-acme", testSandboxSpec(), "token")

	if got := pod.Annotations[sandboxmount.WorkspacePodAnnotation]; got != "coder-2" {
		t.Errorf("workspace pod annotation = %q, want coder-2", got)
	}
	if pod.Name != sandboxmount.SandboxPodName("run-1") {
		t.Errorf("pod name = %q", pod.Name)
	}
	if !slices.ContainsFunc(pod.Spec.Containers[0].VolumeMounts, func(m corev1.VolumeMount) bool {
		return m.Name == localDiskVolume && m.MountPath == "/workspaces/run-1/sandbox" && m.SubPath == "sandbox"
	}) {
		t.Errorf("mounts = %+v, want the local disk's sandbox folder at /workspaces/run-1/sandbox", pod.Spec.Containers[0].VolumeMounts)
	}
	// The node agent accepts only a sandbox that mounts its session-manager's workspace claim.
	if !slices.ContainsFunc(pod.Spec.Volumes, func(v corev1.Volume) bool {
		return v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == "workspaces-coder-2"
	}) {
		t.Errorf("volumes = %+v, want the claim workspaces-coder-2", pod.Spec.Volumes)
	}
}

// Without the node agent no session-manager is named, and no folder sits in the workspace.
func TestSandboxPod_NoSandboxMount(t *testing.T) {
	pod := buildTestSandboxPod(sandboxMountProvisioner(false), Run{ID: "run-1"}, "coder", "orbit-tenant-acme", testSandboxSpec(), "token")
	if _, ok := pod.Annotations[sandboxmount.WorkspacePodAnnotation]; ok {
		t.Errorf("annotations %v, want no workspace pod", pod.Annotations)
	}
	for _, m := range pod.Spec.Containers[0].VolumeMounts {
		if strings.HasSuffix(m.MountPath, "/sandbox") {
			t.Errorf("unexpected sandbox folder mount %+v", m)
		}
	}
}

// The session-manager receives the node agent's mounts on its workspace volume only when the agent is enabled.
func TestSessionManager_WorkspacePropagation(t *testing.T) {
	at := AgentType{ID: "at-1", Slug: "coder", TenantID: "acme"}
	for _, enabled := range []bool{true, false} {
		ss := sandboxMountProvisioner(enabled).buildSessionManagerStatefulSet(at, "ns", podLabels(at))
		var ws *corev1.VolumeMount
		for i, m := range ss.Spec.Template.Spec.Containers[0].VolumeMounts {
			if m.Name == "workspaces" {
				ws = &ss.Spec.Template.Spec.Containers[0].VolumeMounts[i]
			}
		}
		if ws == nil || ws.SubPath != "" {
			t.Fatalf("enabled=%v: workspace mount %+v, want the whole volume", enabled, ws)
		}
		got := ws.MountPropagation != nil && *ws.MountPropagation == corev1.MountPropagationHostToContainer
		if got != enabled {
			t.Errorf("enabled=%v: propagation %v", enabled, ws.MountPropagation)
		}
	}
}
