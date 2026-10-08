package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func folderRegistry(t *testing.T, mountinfo string) *Registry {
	t.Helper()
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), "/workspaces", nil, time.Minute)
	reg.mountInfo = filepath.Join(t.TempDir(), "mountinfo")
	reg.folderWait = 300 * time.Millisecond
	if err := os.WriteFile(reg.mountInfo, []byte(mountinfo), 0o600); err != nil {
		t.Fatal(err)
	}
	return reg
}

const folderLine = "9 1 0:9 / /workspaces/run-1/sandbox rw - nfs4 10.0.0.1:/%s rw\n"

// The wait ends once the run's folder is mounted from this sandbox pod.
func TestAwaitSandboxFolder_Mounted(t *testing.T) {
	reg := folderRegistry(t, "1 0 0:1 / /workspaces rw - ext4 /dev/vdb1 rw\n"+fmt.Sprintf(folderLine, "pod-uid"))
	if err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid"); err != nil {
		t.Errorf("awaitSandboxFolder: %v", err)
	}
}

// A folder still mounted from a replaced sandbox, or a folder mounted for another run, does not end the wait.
func TestAwaitSandboxFolder_WrongSourceTimesOut(t *testing.T) {
	for name, info := range map[string]string{
		"replaced sandbox": fmt.Sprintf(folderLine, "old-uid"),
		"another run":      "9 1 0:9 / /workspaces/run-2/sandbox rw - nfs4 10.0.0.1:/pod-uid rw\n",
		"nothing mounted":  "",
	} {
		t.Run(name, func(t *testing.T) {
			reg := folderRegistry(t, info)
			if err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid"); !errors.Is(err, errSandboxFolder) {
				t.Errorf("err = %v, want errSandboxFolder", err)
			}
		})
	}
}

// A mount that appears during the wait ends it.
func TestAwaitSandboxFolder_MountArrives(t *testing.T) {
	reg := folderRegistry(t, "")
	reg.folderWait = 5 * time.Second
	go func() {
		time.Sleep(400 * time.Millisecond)
		_ = os.WriteFile(reg.mountInfo, fmt.Appendf(nil, folderLine, "pod-uid"), 0o600)
	}()
	if err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid"); err != nil {
		t.Errorf("awaitSandboxFolder: %v", err)
	}
}

// The descriptor records the folder's sandbox pod, so a restarted session-manager can wait for the mount again.
func TestSandboxDescriptor_RecordsFolder(t *testing.T) {
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), nil, time.Minute)
	dir, endpoint, token, ok := reg.sandboxPaths("run-1")
	if !ok {
		t.Fatal("sandboxPaths refused run-1")
	}
	if err := writeSandboxDescriptor(dir, token, endpoint, Sandbox{Endpoint: "10.0.0.1:50051", ExecToken: "tok", Folder: "pod-uid"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := reg.sandboxFromDisk("run-1"); !ok || got.Folder != "pod-uid" {
		t.Errorf("sandboxFromDisk = %+v, %v, want folder pod-uid", got, ok)
	}
	// A sandbox without a folder clears the record left by an earlier one.
	if err := writeSandboxDescriptor(dir, token, endpoint, Sandbox{Endpoint: "10.0.0.1:50051", ExecToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := reg.sandboxFromDisk("run-1"); got.Folder != "" {
		t.Errorf("folder = %q after a sandbox without one", got.Folder)
	}
}
