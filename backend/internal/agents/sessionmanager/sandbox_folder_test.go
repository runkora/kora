package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sandboxmount "github.com/clear-route/orbit/internal/agents/sandbox/mount"
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

// A descriptor file the run replaced with a symlink out of its workspace is refused,
// so the session-manager never writes into another run's files.
func TestSandboxDescriptor_RefusesSymlinkOutOfWorkspace(t *testing.T) {
	workspaces := t.TempDir()
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), workspaces, nil, time.Minute)
	victim := filepath.Join(workspaces, "run-2", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(victim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("run 2 data"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, endpoint, token, _ := reg.sandboxPaths("run-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, sandboxFolderFile)); err != nil {
		t.Fatal(err)
	}

	if err := writeSandboxDescriptor(dir, token, endpoint, Sandbox{Endpoint: "10.0.0.1:50051", ExecToken: "tok", Folder: "pod-uid"}); err == nil {
		t.Error("writeSandboxDescriptor wrote through a symlink out of the workspace")
	}
	if got, _ := os.ReadFile(victim); string(got) != "run 2 data" {
		t.Errorf("victim file = %q, want it untouched", got)
	}
}

// A node agent report about this sandbox ends the wait at once with its reason.
// A report about an earlier sandbox of the run is ignored.
func TestAwaitSandboxFolder_EndsOnNodeAgentReport(t *testing.T) {
	reg := folderRegistry(t, "")
	reg.workspaces = t.TempDir()
	reg.folderWait = 5 * time.Second
	report := filepath.Join(reg.workspaces, sandboxmount.ReportDir, "run-1")
	if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
		t.Fatal(err)
	}
	reason := "node b cannot reach the NFS server on node a on TCP port 20490. Allow TCP 20490 from b to a."
	if err := os.WriteFile(report, []byte("old-uid\nstale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := reg.mountFailure("run-1", "pod-uid"); got != "" {
		t.Errorf("mountFailure with a stale report = %q", got)
	}
	if err := os.WriteFile(report, []byte("pod-uid\n"+reason+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid")
	if !errors.Is(err, errSandboxFolder) || !strings.Contains(err.Error(), "TCP port 20490") {
		t.Errorf("err = %v, want the reported reason", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("wait took %v, want it to end on the report", time.Since(start))
	}
}

// A folder mount failure reaches the agent with its reason, not the generic provision failure.
func TestWriteSandboxError_FolderReason(t *testing.T) {
	c := &Controller{logger: logging.NewLoggerWithOutput(io.Discard)}
	rec := httptest.NewRecorder()
	c.writeSandboxError(context.Background(), rec, "run-1", fmt.Errorf("%w: node b cannot reach the NFS server on node a on TCP port 20490", errSandboxFolder), "failed to provision sandbox")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "TCP port 20490") {
		t.Errorf("response = %d %s, want 503 with the reason", rec.Code, rec.Body.String())
	}
}
