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

	sandboxfolder "github.com/clear-route/orbit/internal/agents/sandbox/folder"
	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
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
	if _, err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid"); err != nil {
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
			if _, err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid"); !errors.Is(err, errSandboxFolder) {
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
	if _, err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid"); err != nil {
		t.Errorf("awaitSandboxFolder: %v", err)
	}
}

// The descriptor records the folder's sandbox pod, so a restarted session-manager can wait for the mount again.
func TestSandboxDescriptor_RecordsFolder(t *testing.T) {
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), nil, time.Minute)
	dir, ok := reg.runDir("run-1")
	if !ok {
		t.Fatal("runDir refused run-1")
	}
	if err := writeSandboxDescriptor(dir, Sandbox{Endpoint: "10.0.0.1:50051", ExecToken: "tok", Folder: "pod-uid"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := reg.sandboxFromDisk("run-1"); !ok || got.Folder != "pod-uid" {
		t.Errorf("sandboxFromDisk = %+v, %v, want folder pod-uid", got, ok)
	}
	// A sandbox without a folder clears the record left by an earlier one.
	if err := writeSandboxDescriptor(dir, Sandbox{Endpoint: "10.0.0.1:50051", ExecToken: "tok"}); err != nil {
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
	dir, _ := reg.runDir("run-1")
	if err := os.MkdirAll(filepath.Join(dir, runfs.ExecDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, runfs.ExecDir, runfs.FolderFile)); err != nil {
		t.Fatal(err)
	}

	if err := writeSandboxDescriptor(dir, Sandbox{Endpoint: "10.0.0.1:50051", ExecToken: "tok", Folder: "pod-uid"}); err == nil {
		t.Error("writeSandboxDescriptor wrote through a symlink out of the workspace")
	}
	if got, _ := os.ReadFile(victim); string(got) != "run 2 data" {
		t.Errorf("victim file = %q, want it untouched", got)
	}
}

// A node agent report that stays through the grace period ends the wait with fixed text.
// A report about an earlier sandbox of the run is ignored.
func TestAwaitSandboxFolder_EndsOnLastingReport(t *testing.T) {
	reg := folderRegistry(t, "")
	reg.workspaces = t.TempDir()
	reg.folderWait = 5 * time.Second
	reg.reportGrace = 300 * time.Millisecond
	report := filepath.Join(reg.workspaces, sandboxfolder.ReportDir, "run-1")
	if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, []byte("old-uid\n"+sandboxfolder.ReasonUnreachable+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := reg.mountFailure("run-1", "pod-uid"); got != "" {
		t.Errorf("mountFailure with a stale report = %q", got)
	}
	if err := os.WriteFile(report, []byte("pod-uid\n"+sandboxfolder.ReasonUnreachable+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid")
	if !errors.Is(err, errSandboxFolder) || !strings.Contains(err.Error(), "cannot reach the NFS port on the sandbox node") {
		t.Errorf("err = %v, want the fixed reason", err)
	}
	if took := time.Since(start); took < reg.reportGrace || took > 2*time.Second {
		t.Errorf("wait took %v, want it to end after the grace period", took)
	}
}

// A report the node agent clears on its next retry does not fail the sandbox: the mount that follows ends the wait.
func TestAwaitSandboxFolder_IgnoresTransientReport(t *testing.T) {
	reg := folderRegistry(t, "")
	reg.workspaces = t.TempDir()
	reg.folderWait = 5 * time.Second
	reg.reportGrace = 2 * time.Second
	report := filepath.Join(reg.workspaces, sandboxfolder.ReportDir, "run-1")
	if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, []byte("pod-uid\n"+sandboxfolder.ReasonUnreachable+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = os.Remove(report)
		_ = os.WriteFile(reg.mountInfo, fmt.Appendf(nil, "9 1 0:9 / %s rw - nfs4 10.0.0.1:/pod-uid rw\n", filepath.Join(reg.workspaces, "run-1", "sandbox")), 0o600)
	}()
	if _, err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid"); err != nil {
		t.Errorf("awaitSandboxFolder: %v", err)
	}
}

// The run sees fixed text for a report, never the report's own content, so no node,
// address or port reaches the run even if the report carried one.
func TestMountFailure_RunFacingTextNamesNoNode(t *testing.T) {
	reg := folderRegistry(t, "")
	reg.workspaces = t.TempDir()
	report := filepath.Join(reg.workspaces, sandboxfolder.ReportDir, "run-1")
	if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{
		"pod-uid\n" + sandboxfolder.ReasonUnreachable + "\n",
		"pod-uid\n" + sandboxfolder.ReasonMountFailed + "\n",
		"pod-uid\nnode ip-10-0-2-36 at 10.0.2.36 refused TCP 20490 on /var/lib/kubelet/pods/x\n",
	} {
		if err := os.WriteFile(report, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got := reg.mountFailure("run-1", "pod-uid")
		if got == "" {
			t.Errorf("report %q gave no text", content)
		}
		for _, leak := range []string{"ip-10", "10.0.", "20490", "/var/lib", "node ip"} {
			if strings.Contains(got, leak) {
				t.Errorf("run-facing text %q names %q", got, leak)
			}
		}
	}
}

// A folder mount failure reaches the agent with its reason, not the generic provision failure.
func TestWriteSandboxError_FolderReason(t *testing.T) {
	c := &Controller{logger: logging.NewLoggerWithOutput(io.Discard)}
	rec := httptest.NewRecorder()
	c.writeSandboxError(context.Background(), rec, "run-1", fmt.Errorf("%w: %s", errSandboxFolder, mountFailureText[sandboxfolder.ReasonUnreachable]), "failed to provision sandbox")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "cannot reach the NFS port on the sandbox node") {
		t.Errorf("response = %d %s, want 503 with the reason", rec.Code, rec.Body.String())
	}
}
