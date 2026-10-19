package agents

import (
	"context"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"

	sandboxfolder "github.com/runkora/kora/backend/internal/agents/sandbox/folder"
)

func sandboxMountProvisioner(enabled bool) *K8sProvisioner {
	p := podSecurityProvisioner()
	p.cfg.SandboxMount = enabled
	return p
}

// With the node agent enabled, the sandbox mounts its local disk's working folder
// inside the workspace. Its workspace claim names its session-manager pod.
func TestSandboxPod_SandboxMount(t *testing.T) {
	pod := buildTestSandboxPod(sandboxMountProvisioner(true), Run{ID: "run-1", PodOrdinal: 2}, "coder", "orbit-tenant-acme", testSandboxSpec(), "token")

	if pod.Name != "sandbox-run-1" || pod.Labels[sandboxfolder.RunIDLabel] != "run-1" || pod.Labels[sandboxfolder.RoleLabel] != "sandbox" {
		t.Errorf("pod %q labels %v, want sandbox-run-1 with the run and role labels", pod.Name, pod.Labels)
	}
	if !hasFolderMount(pod) {
		t.Error("hasFolderMount = false for a sandbox with the folder mount")
	}
	if !slices.ContainsFunc(pod.Spec.Containers[0].VolumeMounts, func(m corev1.VolumeMount) bool {
		return m.Name == localDiskVolume && m.MountPath == "/workspaces/run-1/sandbox" && m.SubPath == "sandbox"
	}) {
		t.Errorf("mounts = %+v, want the local disk's sandbox folder at /workspaces/run-1/sandbox", pod.Spec.Containers[0].VolumeMounts)
	}
	// The node agent finds the session-manager pod through this claim.
	if !slices.ContainsFunc(pod.Spec.Volumes, func(v corev1.Volume) bool {
		return v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == "workspaces-coder-2"
	}) {
		t.Errorf("volumes = %+v, want the claim workspaces-coder-2", pod.Spec.Volumes)
	}
}

// Without the node agent no folder sits in the workspace.
func TestSandboxPod_NoSandboxMount(t *testing.T) {
	pod := buildTestSandboxPod(sandboxMountProvisioner(false), Run{ID: "run-1"}, "coder", "orbit-tenant-acme", testSandboxSpec(), "token")
	if hasFolderMount(pod) {
		t.Error("hasFolderMount = true without the node agent")
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

// A reused live sandbox reports its UID as the folder only when the feature is on
// and the pod mounts its working folder. A pod from before the feature has none.
func TestProvisionSandbox_FolderOfLivePod(t *testing.T) {
	run, spec := Run{ID: "run-1"}, testSandboxSpec()
	for name, tc := range map[string]struct{ builtWith, provisionWith bool }{
		"feature on":              {builtWith: true, provisionWith: true},
		"pod from before feature": {builtWith: false, provisionWith: true},
		"feature off":             {builtWith: true, provisionWith: false},
	} {
		t.Run(name, func(t *testing.T) {
			live := buildTestSandboxPod(sandboxMountProvisioner(tc.builtWith), run, "coder", "ns", spec, "live-token")
			live.UID = "pod-uid"
			live.Status = corev1.PodStatus{
				Phase:      corev1.PodRunning,
				PodIP:      "10.0.0.7",
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			}
			p := sandboxMountProvisioner(tc.provisionWith)
			p.client = fake.NewSimpleClientset(live)

			sb, err := p.ProvisionSandbox(context.Background(), run, "coder", "ns", spec)
			want := ""
			if tc.builtWith && tc.provisionWith {
				want = "pod-uid"
			}
			if err != nil || sb.Folder != want {
				t.Errorf("ProvisionSandbox = %+v, %v, want folder %q", sb, err, want)
			}
		})
	}
}
