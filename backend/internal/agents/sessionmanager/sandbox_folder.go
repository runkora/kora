package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sandboxfolder "github.com/clear-route/orbit/internal/agents/sandbox/folder"
)

// awaitFolderOrFail waits for the sandbox's folder when it has one, and records a
// failed provision with the node agent's reason, or the timeout.
func (reg *Registry) awaitFolderOrFail(ctx context.Context, runID, podUID string, started time.Time) error {
	if podUID == "" {
		return nil
	}
	reason, err := reg.awaitSandboxFolder(ctx, runID, podUID)
	if err == nil {
		return nil
	}
	msg := "the sandbox folder was not mounted in time"
	if reason != "" {
		msg = "the sandbox folder could not be mounted: " + reason
	}
	reg.sandboxStatus(ctx, runID, SandboxPhaseFailed, started, msg)
	return err
}

// mountFailure returns the run-facing text for the node agent's reason for not
// mounting the run's folder from the given sandbox pod, or empty. A report about
// another sandbox is ignored, and the report's own text never reaches the run.
func (reg *Registry) mountFailure(runID, podUID string) string {
	data, err := os.ReadFile(filepath.Join(reg.workspaces, sandboxfolder.ReportDir, runID))
	if err != nil {
		return ""
	}
	uid, code, _ := strings.Cut(string(data), "\n")
	if uid != podUID {
		return ""
	}
	if text, ok := mountFailureText[strings.TrimSpace(code)]; ok {
		return text
	}
	return mountFailureText[sandboxfolder.ReasonMountFailed]
}

// errSandboxFolder reports a sandbox whose working folder the node agent did not mount in time.
var errSandboxFolder = errors.New("sandbox folder not mounted")

// The node agent mounts within a few seconds of the sandbox starting. The wait bounds a missing or failing agent.
// A failure report that stays through several of the agent's retries ends the wait early.
const (
	sandboxFolderWait  = 60 * time.Second
	sandboxFolderPoll  = 250 * time.Millisecond
	sandboxReportGrace = 10 * time.Second
)

// mountFailureText is what the run sees for each reason the node agent reports. It names no node, address or port.
var mountFailureText = map[string]string{
	sandboxfolder.ReasonUnreachable: "the node agent cannot reach the NFS port on the sandbox node",
	sandboxfolder.ReasonMountFailed: "the node agent could not mount the sandbox folder",
}

// awaitSandboxFolder waits until the run's sandbox folder is mounted from the
// sandbox pod with the given UID (docs/adr/2026-10-08-sandbox-workspace-mount.md).
// A mount left from a replaced sandbox has another source and does not count.
// When the node agent keeps reporting why it cannot mount this sandbox's folder,
// the wait ends with that reason once the report outlasts sandboxReportGrace. A
// failed wait returns the last reason the node agent reported, or empty.
func (reg *Registry) awaitSandboxFolder(ctx context.Context, runID, podUID string) (string, error) {
	point := filepath.Join(reg.workspaces, runID, sandboxfolder.Dir)
	ctx, cancel := context.WithTimeout(ctx, reg.folderWait)
	defer cancel()
	tick := time.NewTicker(sandboxFolderPoll)
	defer tick.Stop()
	var reportedAt time.Time
	for {
		if f, err := os.Open(reg.mountInfo); err == nil {
			source := sandboxfolder.SourceAt(f, point)
			_ = f.Close()
			if strings.HasSuffix(source, ":/"+podUID) {
				return "", nil
			}
		}
		reason := reg.mountFailure(runID, podUID)
		switch {
		case reason == "":
			reportedAt = time.Time{}
		case reportedAt.IsZero():
			reportedAt = time.Now()
		case time.Since(reportedAt) >= reg.reportGrace:
			return reason, fmt.Errorf("%w: %s", errSandboxFolder, reason)
		}
		select {
		case <-ctx.Done():
			if reason != "" {
				return reason, fmt.Errorf("%w: %s", errSandboxFolder, reason)
			}
			return "", fmt.Errorf("%w at %s: %w", errSandboxFolder, point, ctx.Err())
		case <-tick.C:
		}
	}
}
