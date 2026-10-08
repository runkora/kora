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
)

// The contract between orbit-agents, which builds the pods, and the node agent.
const (
	// WorkspacePodAnnotation on a sandbox pod names the session-manager pod, in
	// the same namespace, whose workspace receives the sandbox's folder.
	WorkspacePodAnnotation = "orbit.io/workspace-pod"
	// RunIDLabel carries the sandbox's run ID, which names its workspace directory.
	RunIDLabel = "orbit.io/run-id"
	// Volume is the sandbox pod's local disk, and Dir is the folder in it that
	// both pods see at /workspaces/<run>/sandbox.
	Volume = "local-disk"
	Dir    = "sandbox"
	// workspaceClaimPrefix starts the name of a session-manager pod's workspace
	// claim: the StatefulSet's volumeClaimTemplate name, then the pod name.
	workspaceClaimPrefix = "workspaces-"
)

// SandboxPodName is the name orbit-agents gives a run's sandbox pod. Names are
// unique in a namespace, so at most one pod can claim a run's folder.
func SandboxPodName(runID string) string { return "sandbox-" + runID }

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
}

// pair is a sandbox pod and the session-manager pod its annotation names.
type pair struct {
	sandbox   *corev1.Pod
	workspace *corev1.Pod
}

// plan returns what this node exports and mounts for the given pods.
func plan(node, kubeletDir string, pairs []pair) ([]export, []target) {
	var exports []export
	var targets []target
	for _, p := range pairs {
		runID := p.sandbox.Labels[RunIDLabel]
		if !live(p.sandbox) || !live(p.workspace) || !singleSegment(runID) || !owns(p, runID) || !isIP(p.sandbox.Status.HostIP) || !isIP(p.workspace.Status.HostIP) || !podUID.MatchString(string(p.sandbox.UID)) {
			continue
		}
		name := string(p.sandbox.UID)
		if p.sandbox.Spec.NodeName == node {
			if uid, gid, ok := runAs(p.sandbox); ok {
				exports = append(exports, export{
					name:   name,
					path:   filepath.Join(kubeletDir, "pods", name, "volumes", "kubernetes.io~empty-dir", Volume, Dir),
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
			})
		}
	}
	return exports, targets
}

// owns reports a sandbox pod built for this run and this session-manager pod: it
// carries the run's sandbox name, sits in the session-manager's namespace, and
// mounts that pod's workspace claim. A pod that claims another run's folder, or
// another session-manager's workspace, gets nothing.
func owns(p pair, runID string) bool {
	if p.sandbox.Name != SandboxPodName(runID) || p.sandbox.Namespace != p.workspace.Namespace {
		return false
	}
	return slices.ContainsFunc(p.sandbox.Spec.Volumes, func(v corev1.Volume) bool {
		return v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == workspaceClaimPrefix+p.workspace.Name
	})
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

// runAs returns the pod-level user and group the sandbox writes as. The export maps every client user to them.
func runAs(pod *corev1.Pod) (int64, int64, bool) {
	sc := pod.Spec.SecurityContext
	if sc == nil || sc.RunAsUser == nil || sc.RunAsGroup == nil {
		return 0, 0, false
	}
	return *sc.RunAsUser, *sc.RunAsGroup, true
}

// singleSegment reports a run ID that is one plain path component.
func singleSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && filepath.IsLocal(s)
}

// hostForSource brackets an IPv6 address for an NFS source.
func hostForSource(ip string) string {
	if strings.Contains(ip, ":") {
		return "[" + ip + "]"
	}
	return ip
}
