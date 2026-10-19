//go:build linux

// Package mount shares a sandbox's local folder with its session-manager pod
// over kernel NFS (docs/adr/2026-10-08-sandbox-workspace-mount.md). orbit-agents
// marks the sandbox pod, and the node agent on each node exports or mounts the
// folder for the pods it hosts.
package mount

import (
	"net"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/runkora/kora/backend/internal/agents/sandbox/folder"
	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
)

// export is one sandbox folder this node serves.
type export struct {
	// name is the sandbox pod's UID. It names the export and is its NFS fsid.
	name string
	// path is the sandbox folder under the node's kubelet directory.
	path string
	// client is the IP of the session-manager's node, the only host allowed to mount it.
	client string
	uid    int64
	gid    int64
}

// target is one sandbox folder this node mounts into a session-manager pod.
type target struct {
	// workspaceUID is the session-manager pod's UID, which locates its volumes.
	workspaceUID string
	runID        string
	// source is the NFS source, server:/name.
	source string
	// serverIP and serverNode are the sandbox's node, clientNode the session-manager's.
	serverIP   string
	serverNode string
	clientNode string
	// sandboxUID is the sandbox pod's UID, which a failure report carries.
	sandboxUID string
}

// pair is a sandbox pod and the session-manager pod its workspace claim names.
type pair struct {
	sandbox   *corev1.Pod
	workspace *corev1.Pod
}

// plan returns what this node exports and mounts for the given pods.
func plan(node, kubeletDir string, pairs []pair) ([]export, []target) {
	var exports []export
	var targets []target
	for _, p := range pairs {
		runID := p.sandbox.Labels[folder.RunIDLabel]
		if !live(p.sandbox) || !live(p.workspace) || !runfs.IsRunID(runID) || !owns(p, runID) || !isIP(p.sandbox.Status.HostIP) || !isIP(p.workspace.Status.HostIP) || !podUID.MatchString(string(p.sandbox.UID)) || !podUID.MatchString(string(p.workspace.UID)) {
			continue
		}
		name := string(p.sandbox.UID)
		if p.sandbox.Spec.NodeName == node {
			if uid, gid, ok := runAs(p.sandbox); ok {
				exports = append(exports, export{
					name:   name,
					path:   filepath.Join(kubeletDir, "pods", name, "volumes", "kubernetes.io~empty-dir", folder.Volume, folder.Dir),
					client: p.workspace.Status.HostIP,
					uid:    uid,
					gid:    gid,
				})
			}
		}
		if p.workspace.Spec.NodeName == node {
			targets = append(targets, target{
				workspaceUID: string(p.workspace.UID),
				runID:        runID,
				source:       hostForSource(p.sandbox.Status.HostIP) + ":/" + name,
				serverIP:     p.sandbox.Status.HostIP,
				serverNode:   p.sandbox.Spec.NodeName,
				clientNode:   p.workspace.Spec.NodeName,
				sandboxUID:   name,
			})
		}
	}
	return exports, targets
}

// workspaceOf returns the session-manager pod whose workspace the sandbox shares
// its folder with: the pod its workspace volume's claim belongs to. A sandbox
// without the folder mount or a workspace claim shares nothing.
func workspaceOf(sb *corev1.Pod) (string, bool) {
	if !slices.ContainsFunc(sb.Spec.Containers, func(c corev1.Container) bool {
		return slices.ContainsFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool { return folder.IsFolderMount(m.Name, m.SubPath) })
	}) {
		return "", false
	}
	for _, v := range sb.Spec.Volumes {
		if v.Name == folder.WorkspaceVolume && v.PersistentVolumeClaim != nil {
			return folder.WorkspacePod(v.PersistentVolumeClaim.ClaimName)
		}
	}
	return "", false
}

// owns reports a sandbox pod built for this run and this session-manager pod: it
// carries the run's sandbox name, sits in the session-manager's namespace, and
// its workspace volume claims that pod's workspace. A pod that claims another
// run's folder, or another session-manager's workspace, gets nothing.
func owns(p pair, runID string) bool {
	ws, ok := workspaceOf(p.sandbox)
	return ok && ws == p.workspace.Name && p.sandbox.Name == folder.SandboxPodName(runID) && p.sandbox.Namespace == p.workspace.Namespace
}

// podUID matches a Kubernetes UID. It names the export directory, the fsid and an
// exports table entry, so nothing else may reach those.
var podUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isIP reports a literal IP address. Node addresses name export directories and exports table entries.
func isIP(s string) bool { return net.ParseIP(s) != nil }

// live reports a running pod that is not being deleted.
func live(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning
}

// runAs returns the pod-level user and group the sandbox writes as. The export maps
// every client user to them, so root is refused.
func runAs(pod *corev1.Pod) (int64, int64, bool) {
	sc := pod.Spec.SecurityContext
	if sc == nil || sc.RunAsUser == nil || sc.RunAsGroup == nil || *sc.RunAsUser == 0 || *sc.RunAsGroup == 0 {
		return 0, 0, false
	}
	return *sc.RunAsUser, *sc.RunAsGroup, true
}

// hostForSource brackets an IPv6 address for an NFS source.
func hostForSource(ip string) string {
	if strings.Contains(ip, ":") {
		return "[" + ip + "]"
	}
	return ip
}
