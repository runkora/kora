//go:build linux

package mount

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/clear-route/orbit/internal/agents/sandbox/folder"
)

func sandboxPod(node, hostIP string) *corev1.Pod {
	uid, gid := int64(100), int64(100)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandbox-run-1", Namespace: "tenant", UID: types.UID("0d6f0f3e-1111-4a8b-9c2d-000000000001"),
			Labels: map[string]string{folder.RunIDLabel: "run-1"},
		},
		Spec: corev1.PodSpec{
			NodeName:        node,
			SecurityContext: &corev1.PodSecurityContext{RunAsUser: &uid, RunAsGroup: &gid},
			Containers: []corev1.Container{{Name: "sandbox-exec", VolumeMounts: []corev1.VolumeMount{
				{Name: folder.Volume, MountPath: "/workspaces/run-1/sandbox", SubPath: folder.Dir},
			}}},
			Volumes: []corev1.Volume{{Name: folder.WorkspaceVolume, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "workspaces-agent-0"},
			}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, HostIP: hostIP},
	}
}

func workspacePod(node, hostIP string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-0", Namespace: "tenant", UID: types.UID("0d6f0f3e-1111-4a8b-9c2d-0000000000aa")},
		Spec:       corev1.PodSpec{NodeName: node},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, HostIP: hostIP},
	}
}

// The sandbox's node exports the folder to the session-manager's node only, and the session-manager's node mounts it.
func TestPlan_SplitsRolesByNode(t *testing.T) {
	pairs := []pair{{sandbox: sandboxPod("node-a", "10.0.0.1"), workspace: workspacePod("node-b", "10.0.0.2")}}

	exports, targets := plan("node-a", "/var/lib/kubelet", pairs)
	want := export{name: "0d6f0f3e-1111-4a8b-9c2d-000000000001", path: "/var/lib/kubelet/pods/0d6f0f3e-1111-4a8b-9c2d-000000000001/volumes/kubernetes.io~empty-dir/local-disk/sandbox", client: "10.0.0.2", uid: 100, gid: 100}
	if len(exports) != 1 || exports[0] != want {
		t.Errorf("node-a exports = %+v, want %+v", exports, want)
	}
	if len(targets) != 0 {
		t.Errorf("node-a targets = %+v, want none", targets)
	}

	exports, targets = plan("node-b", "/var/lib/kubelet", pairs)
	if len(exports) != 0 {
		t.Errorf("node-b exports = %+v, want none", exports)
	}
	if len(targets) != 1 || targets[0].source != "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001" || targets[0].workspaceUID != "0d6f0f3e-1111-4a8b-9c2d-0000000000aa" || targets[0].runID != "run-1" {
		t.Errorf("node-b targets = %+v", targets)
	}
}

// A co-located sandbox is exported to its own node and mounted there.
func TestPlan_CoLocated(t *testing.T) {
	pairs := []pair{{sandbox: sandboxPod("node-a", "10.0.0.1"), workspace: workspacePod("node-a", "10.0.0.1")}}
	exports, targets := plan("node-a", "/k", pairs)
	if len(exports) != 1 || exports[0].client != "10.0.0.1" || len(targets) != 1 {
		t.Errorf("exports = %+v, targets = %+v", exports, targets)
	}
}

// Nothing is exported or mounted for a pair that is not live, not this run's, or not this session-manager's,
// or for a pod UID or node address that could add entries or options to the exports table. A sandbox without
// a non-root user and group is not exported, since the export maps every client user to them.
func TestPlan_SkipsUnusablePairs(t *testing.T) {
	now := metav1.Now()
	root := int64(0)
	cases := map[string]func(sb, ws *corev1.Pod){
		"sandbox deleting":     func(sb, _ *corev1.Pod) { sb.DeletionTimestamp = &now },
		"workspace deleting":   func(_, ws *corev1.Pod) { ws.DeletionTimestamp = &now },
		"sandbox pending":      func(sb, _ *corev1.Pod) { sb.Status.Phase = corev1.PodPending },
		"sandbox failed":       func(sb, _ *corev1.Pod) { sb.Status.Phase = corev1.PodFailed },
		"workspace no host IP": func(_, ws *corev1.Pod) { ws.Status.HostIP = "" },
		"run ID escapes":       func(sb, _ *corev1.Pod) { sb.Labels[folder.RunIDLabel] = "../other" },
		"run ID dot-dot":       func(sb, _ *corev1.Pod) { sb.Labels[folder.RunIDLabel] = ".." },
		"no run ID":            func(sb, _ *corev1.Pod) { delete(sb.Labels, folder.RunIDLabel) },
		// A pod labelled with another run's ID does not carry that run's sandbox name.
		"claims another run": func(sb, _ *corev1.Pod) { sb.Labels[folder.RunIDLabel] = "run-2" },
		"another workspace":  func(_, ws *corev1.Pod) { ws.Name = "agent-1" },
		"no workspace claim": func(sb, _ *corev1.Pod) { sb.Spec.Volumes = nil },
		"no folder mount":    func(sb, _ *corev1.Pod) { sb.Spec.Containers = nil },
		"another namespace":  func(_, ws *corev1.Pod) { ws.Namespace = "other-tenant" },
		"UID with a newline": func(sb, _ *corev1.Pod) { sb.UID = "0d6f0f3e-1111-4a8b-9c2d-000000000001\n/ *(rw,no_root_squash)" },
		"UID path":           func(sb, _ *corev1.Pod) { sb.UID = "../../etc" },
		"client with an ACL": func(_, ws *corev1.Pod) { ws.Status.HostIP = "10.0.0.2 *(rw)" },
		"server not an IP":   func(sb, _ *corev1.Pod) { sb.Status.HostIP = "evil.example.com" },
		"workspace UID path": func(_, ws *corev1.Pod) { ws.UID = "../*" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			sb, ws := sandboxPod("node-a", "10.0.0.1"), workspacePod("node-a", "10.0.0.1")
			mutate(sb, ws)
			exports, targets := plan("node-a", "/k", []pair{{sandbox: sb, workspace: ws}})
			if len(exports) != 0 || len(targets) != 0 {
				t.Errorf("exports = %+v, targets = %+v, want none", exports, targets)
			}
		})
	}
	for name, mutate := range map[string]func(sb *corev1.Pod){
		"no run-as":    func(sb *corev1.Pod) { sb.Spec.SecurityContext = nil },
		"runs as root": func(sb *corev1.Pod) { sb.Spec.SecurityContext.RunAsUser = &root },
	} {
		sb := sandboxPod("node-a", "10.0.0.1")
		mutate(sb)
		if exports, _ := plan("node-a", "/k", []pair{{sandbox: sb, workspace: workspacePod("node-b", "10.0.0.2")}}); len(exports) != 0 {
			t.Errorf("%s: exports = %+v, want none", name, exports)
		}
	}
}

func TestPlan_BracketsIPv6Source(t *testing.T) {
	_, targets := plan("node-b", "/k", []pair{{sandbox: sandboxPod("node-a", "fd00::1"), workspace: workspacePod("node-b", "fd00::2")}})
	if len(targets) != 1 || targets[0].source != "[fd00::1]:/0d6f0f3e-1111-4a8b-9c2d-000000000001" {
		t.Errorf("targets = %+v", targets)
	}
}
