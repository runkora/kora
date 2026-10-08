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
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w at %s: %w", errSandboxFolder, point, ctx.Err())
		case <-tick.C:
		}
	}
}
