package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sandboxmount "github.com/clear-route/orbit/internal/agents/sandbox/mount"
)

// sandboxFolderFile in the sandbox descriptor folder holds the UID of the sandbox pod whose folder is mounted.
const sandboxFolderFile = "folder"

// awaitFolderOrFail waits for the sandbox's folder when it has one, and records a
// failed provision with the node agent's reason, or the timeout.
func (reg *Registry) awaitFolderOrFail(ctx context.Context, runID, podUID string, started time.Time) error {
	if podUID == "" {
		return nil
	}
	err := reg.awaitSandboxFolder(ctx, runID, podUID)
	if err == nil {
		return nil
	}
	msg := "the sandbox folder was not mounted in time"
	if reason := reg.mountFailure(runID, podUID); reason != "" {
		msg = "the sandbox folder could not be mounted: " + reason
	}
	reg.sandboxStatus(ctx, runID, SandboxPhaseFailed, started, msg)
	return err
}

// mountFailure returns the node agent's reason for not mounting the run's folder
// from the given sandbox pod, or empty. A report about another sandbox is ignored.
func (reg *Registry) mountFailure(runID, podUID string) string {
	data, err := os.ReadFile(filepath.Join(reg.workspaces, sandboxmount.ReportDir, runID))
	if err != nil {
		return ""
	}
	uid, reason, _ := strings.Cut(string(data), "\n")
	if uid != podUID {
		return ""
	}
	return strings.TrimSpace(reason)
}

// errSandboxFolder reports a sandbox whose working folder the node agent did not mount in time.
var errSandboxFolder = errors.New("sandbox folder not mounted")

// The node agent mounts within a few seconds of the sandbox starting. The wait bounds a missing or failing agent.
const (
	sandboxFolderWait = 60 * time.Second
	sandboxFolderPoll = 250 * time.Millisecond
)

// awaitSandboxFolder waits until the run's sandbox folder is mounted from the
// sandbox pod with the given UID (docs/adr/2026-10-08-sandbox-workspace-mount.md).
// A mount left from a replaced sandbox has another source and does not count.
// When the node agent reports why it cannot mount this sandbox's folder, the wait
// ends with that reason.
func (reg *Registry) awaitSandboxFolder(ctx context.Context, runID, podUID string) error {
	point := filepath.Join(reg.workspaces, runID, sandboxmount.Dir)
	ctx, cancel := context.WithTimeout(ctx, reg.folderWait)
	defer cancel()
	tick := time.NewTicker(sandboxFolderPoll)
	defer tick.Stop()
	for {
		if f, err := os.Open(reg.mountInfo); err == nil {
			source := sandboxmount.SourceAt(f, point)
			_ = f.Close()
			if strings.HasSuffix(source, ":/"+podUID) {
				return nil
			}
		}
		if reason := reg.mountFailure(runID, podUID); reason != "" {
			return fmt.Errorf("%w: %s", errSandboxFolder, reason)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w at %s: %w", errSandboxFolder, point, ctx.Err())
		case <-tick.C:
		}
	}
}
