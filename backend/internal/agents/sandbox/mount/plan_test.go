package mount

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func sandboxPod(node, hostIP string) *corev1.Pod {
	uid, gid := int64(100), int64(100)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandbox-run-1", Namespace: "tenant", UID: types.UID("sb-uid"),
			Labels:      map[string]string{RunIDLabel: "run-1"},
			Annotations: map[string]string{WorkspacePodAnnotation: "agent-0"},
		},
		Spec: corev1.PodSpec{
			NodeName:        node,
			SecurityContext: &corev1.PodSecurityContext{RunAsUser: &uid, RunAsGroup: &gid},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, HostIP: hostIP},
	}
}

func workspacePod(node, hostIP string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-0", Namespace: "tenant", UID: types.UID("ws-uid")},
		Spec:       corev1.PodSpec{NodeName: node},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, HostIP: hostIP},
	}
}

// The sandbox's node exports the folder to the session-manager's node only, and the session-manager's node mounts it.
func TestPlan_SplitsRolesByNode(t *testing.T) {
	pairs := []pair{{sandbox: sandboxPod("node-a", "10.0.0.1"), workspace: workspacePod("node-b", "10.0.0.2")}}

	exports, targets := plan("node-a", "/var/lib/kubelet", pairs)
	want := export{name: "sb-uid", path: "/var/lib/kubelet/pods/sb-uid/volumes/kubernetes.io~empty-dir/local-disk/sandbox", client: "10.0.0.2", uid: 100, gid: 100}
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
	if len(targets) != 1 || targets[0].source != "10.0.0.1:/sb-uid" || targets[0].workspaceUID != "ws-uid" || targets[0].runID != "run-1" {
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

// Nothing is exported or mounted for a pod that is deleting or not running, or for a run ID that is not one path segment.
func TestPlan_SkipsUnusablePods(t *testing.T) {
	now := metav1.Now()
	cases := map[string]func(sb, ws *corev1.Pod){
		"sandbox deleting":     func(sb, _ *corev1.Pod) { sb.DeletionTimestamp = &now },
		"workspace deleting":   func(_, ws *corev1.Pod) { ws.DeletionTimestamp = &now },
		"sandbox pending":      func(sb, _ *corev1.Pod) { sb.Status.Phase = corev1.PodPending },
		"sandbox failed":       func(sb, _ *corev1.Pod) { sb.Status.Phase = corev1.PodFailed },
		"workspace no host IP": func(_, ws *corev1.Pod) { ws.Status.HostIP = "" },
		"run ID escapes":       func(sb, _ *corev1.Pod) { sb.Labels[RunIDLabel] = "../other" },
		"run ID dot-dot":       func(sb, _ *corev1.Pod) { sb.Labels[RunIDLabel] = ".." },
		"no run ID":            func(sb, _ *corev1.Pod) { delete(sb.Labels, RunIDLabel) },
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
}

// A sandbox without a pod-level user and group is not exported: the export maps every client user to them.
func TestPlan_RequiresRunAs(t *testing.T) {
	sb := sandboxPod("node-a", "10.0.0.1")
	sb.Spec.SecurityContext = nil
	exports, _ := plan("node-a", "/k", []pair{{sandbox: sb, workspace: workspacePod("node-b", "10.0.0.2")}})
	if len(exports) != 0 {
		t.Errorf("exports = %+v, want none", exports)
	}
}

func TestPlan_BracketsIPv6Source(t *testing.T) {
	_, targets := plan("node-b", "/k", []pair{{sandbox: sandboxPod("node-a", "fd00::1"), workspace: workspacePod("node-b", "fd00::2")}})
	if len(targets) != 1 || targets[0].source != "[fd00::1]:/sb-uid" {
		t.Errorf("targets = %+v", targets)
	}
}
