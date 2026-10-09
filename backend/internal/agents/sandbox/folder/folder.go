// Package folder is the contract for a sandbox's working folder between
// orbit-agents, the session-manager and the sandbox-mount node agent
// (docs/adr/2026-10-08-sandbox-workspace-mount.md). It imports nothing from
// Kubernetes, so the session-manager can use it.
package folder

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	// RoleLabel set to RoleSandbox marks a sandbox pod, and ManagedAgentLabel set to
	// "true" marks a session-manager pod. The node agent watches pods by them.
	RoleLabel         = "orbit.io/role"
	RoleSandbox       = "sandbox"
	ManagedAgentLabel = "orbit.io/managed-agent"
	// RunIDLabel carries the sandbox's run ID, which names its workspace directory.
	RunIDLabel = "orbit.io/run-id"
	// Volume is the sandbox pod's local disk, and Dir is the folder in it that
	// both pods see at /workspaces/<run>/sandbox.
	Volume = "local-disk"
	Dir    = "sandbox"
	// ReportDir, in the root of a session-manager's workspace volume, holds one file
	// per run whose folder could not be mounted: the sandbox pod's UID, then the reason.
	// Runs cannot write the volume root.
	ReportDir = ".sandbox-mount"
	// ReasonUnreachable and ReasonMountFailed are the reasons a report carries. They
	// name no node, address or port, and the session-manager maps them to fixed text.
	ReasonUnreachable = "nfs-port-unreachable"
	ReasonMountFailed = "mount-failed"
)

// workspaceClaimPrefix starts a session-manager pod's workspace claim name: the
// StatefulSet's volumeClaimTemplate name, then the pod name.
const workspaceClaimPrefix = "workspaces-"

// SandboxPodName is the name orbit-agents gives a run's sandbox pod. Names are
// unique in a namespace, so at most one pod can claim a run's folder.
func SandboxPodName(runID string) string { return "sandbox-" + runID }

// WorkspaceClaim is the name of a session-manager pod's workspace claim.
func WorkspaceClaim(podName string) string { return workspaceClaimPrefix + podName }

// WorkspacePod returns the session-manager pod whose workspace claim this is.
func WorkspacePod(claim string) (string, bool) {
	pod, ok := strings.CutPrefix(claim, workspaceClaimPrefix)
	if !ok || pod == "" {
		return "", false
	}
	return pod, true
}

// IsFolderMount reports the sandbox container's mount of its working folder: the
// local disk's Dir subdirectory. A sandbox pod without it has no folder to share.
func IsFolderMount(volume, subPath string) bool { return volume == Volume && subPath == Dir }

// MountEntry is one line of a mountinfo listing.
type MountEntry struct {
	ID, Parent, Point, FSType, Source string
}

// ReadMountInfo parses a mountinfo listing. Lines it cannot parse are skipped.
func ReadMountInfo(r io.Reader) ([]MountEntry, error) {
	var out []MountEntry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if e, ok := parseMountInfo(sc.Text()); ok {
			out = append(out, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read mountinfo: %w", err)
	}
	return out, nil
}

// SourceAt returns the source of the topmost mount at point in a mountinfo
// listing, or an empty string when nothing is mounted there.
func SourceAt(mountinfo io.Reader, point string) string {
	entries, _ := ReadMountInfo(mountinfo)
	var source string
	for _, e := range entries {
		if e.Point == point {
			source = e.Source
		}
	}
	return source
}

// parseMountInfo reads the mount and parent IDs, mount point, file system type and source from one mountinfo line.
func parseMountInfo(line string) (MountEntry, bool) {
	before, after, ok := strings.Cut(line, " - ")
	if !ok {
		return MountEntry{}, false
	}
	pre, post := strings.Fields(before), strings.Fields(after)
	if len(pre) < 5 || len(post) < 2 {
		return MountEntry{}, false
	}
	return MountEntry{ID: pre[0], Parent: pre[1], Point: unescape(pre[4]), FSType: post[0], Source: unescape(post[1])}, true
}

// unescape decodes the octal escapes mountinfo uses for spaces, tabs, newlines and backslashes.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
