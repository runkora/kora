package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

// folderRegistry returns a registry over a temporary workspaces root whose
// mountinfo file holds the given lines, with a short wait.
func folderRegistry(t *testing.T, notifier Notifier, lines ...string) *Registry {
	t.Helper()
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), notifier, time.Minute, testBrand)
	reg.mountInfo = filepath.Join(t.TempDir(), "mountinfo")
	reg.folderWait = 300 * time.Millisecond
	setMountInfo(t, reg, lines...)
	return reg
}

func setMountInfo(t *testing.T, reg *Registry, lines ...string) {
	t.Helper()
	if err := os.WriteFile(reg.mountInfo, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Error(err)
	}
}

// folderMount is the mountinfo line for a run's folder mounted from a sandbox pod.
func folderMount(reg *Registry, runID, podUID string) string {
	return fmt.Sprintf("9 1 0:9 / %s rw - nfs4 10.0.0.1:/%s rw\n", filepath.Join(reg.workspaces, runID, sandboxfolder.Dir), podUID)
}

// writeReport writes the node agent's failure report for run-1.
func writeReport(t *testing.T, reg *Registry, content string) string {
	t.Helper()
	report := filepath.Join(reg.workspaces, sandboxfolder.ReportDir, "run-1")
	if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
		t.Error(err)
	}
	if err := os.WriteFile(report, []byte(content), 0o644); err != nil {
		t.Error(err)
	}
	return report
}

// The wait ends once the run's folder is mounted from this sandbox pod, including a
// mount that arrives after a report the node agent then clears. A folder still mounted
// from a replaced sandbox, or mounted for another run, does not end it.
func TestAwaitSandboxFolder(t *testing.T) {
	none := func(*Registry) string { return "" }
	cases := map[string]struct {
		mounted func(reg *Registry) string
		later   func(t *testing.T, reg *Registry)
		wantErr bool
	}{
		"mounted":          {mounted: func(reg *Registry) string { return folderMount(reg, "run-1", "pod-uid") }},
		"replaced sandbox": {mounted: func(reg *Registry) string { return folderMount(reg, "run-1", "old-uid") }, wantErr: true},
		"another run":      {mounted: func(reg *Registry) string { return folderMount(reg, "run-2", "pod-uid") }, wantErr: true},
		"nothing mounted":  {mounted: none, wantErr: true},
		"mount arrives": {
			mounted: none,
			later:   func(t *testing.T, reg *Registry) { setMountInfo(t, reg, folderMount(reg, "run-1", "pod-uid")) },
		},
		"transient report": {
			mounted: none,
			later: func(t *testing.T, reg *Registry) {
				report := writeReport(t, reg, "pod-uid\n"+sandboxfolder.ReasonUnreachable+"\n")
				time.Sleep(500 * time.Millisecond)
				if err := os.Remove(report); err != nil {
					t.Error(err)
				}
				setMountInfo(t, reg, folderMount(reg, "run-1", "pod-uid"))
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reg := folderRegistry(t, nil)
			reg.reportGrace = 2 * time.Second
			setMountInfo(t, reg, tc.mounted(reg))
			done := make(chan struct{})
			if tc.later != nil {
				reg.folderWait = 5 * time.Second
				go func() {
					defer close(done)
					time.Sleep(200 * time.Millisecond)
					tc.later(t, reg)
				}()
			} else {
				close(done)
			}
			_, err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid")
			<-done
			if tc.wantErr && !errors.Is(err, errSandboxFolder) || !tc.wantErr && err != nil {
				t.Errorf("awaitSandboxFolder = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

// A node agent report that stays through the grace period ends the wait with fixed text.
// A report about an earlier sandbox of the run is ignored.
func TestAwaitSandboxFolder_EndsOnLastingReport(t *testing.T) {
	reg := folderRegistry(t, nil)
	reg.folderWait = 5 * time.Second
	reg.reportGrace = 300 * time.Millisecond
	writeReport(t, reg, "old-uid\n"+sandboxfolder.ReasonUnreachable+"\n")
	if got := reg.mountFailure("run-1", "pod-uid"); got != "" {
		t.Errorf("mountFailure with a stale report = %q", got)
	}
	writeReport(t, reg, "pod-uid\n"+sandboxfolder.ReasonUnreachable+"\n")
	start := time.Now()
	reason, err := reg.awaitSandboxFolder(context.Background(), "run-1", "pod-uid")
	if !errors.Is(err, errSandboxFolder) || reason != mountFailureText[sandboxfolder.ReasonUnreachable] {
		t.Errorf("awaitSandboxFolder = %q, %v, want the fixed reason", reason, err)
	}
	// Under folderWait, so the report ended the wait and the timeout did not.
	if took := time.Since(start); took < reg.reportGrace || took > 4*time.Second {
		t.Errorf("wait took %v, want it to end after the grace period", took)
	}
}

// A failed wait records the sandbox as failed with the node agent's reason, or the
// timeout. A sandbox without a folder has nothing to wait for.
func TestAwaitFolderOrFail(t *testing.T) {
	cases := map[string]struct{ podUID, report, want string }{
		"no folder": {},
		"timeout":   {podUID: "pod-uid", want: "the sandbox folder was not mounted in time"},
		"report": {
			podUID: "pod-uid",
			report: "pod-uid\n" + sandboxfolder.ReasonUnreachable + "\n",
			want:   "the sandbox folder could not be mounted: " + mountFailureText[sandboxfolder.ReasonUnreachable],
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			n := newStateNotifier()
			reg := folderRegistry(t, n)
			reg.reportGrace = time.Hour
			if tc.report != "" {
				writeReport(t, reg, tc.report)
			}
			err := reg.awaitFolderOrFail(context.Background(), "run-1", tc.podUID, time.Now())
			if (tc.want != "") != errors.Is(err, errSandboxFolder) {
				t.Errorf("awaitFolderOrFail = %v", err)
			}
			var failed []string
			_, events := n.snapshot()
			for _, ev := range events {
				var p SandboxStatusPayload
				if ev.Type == EventTypeSandboxStatus && json.Unmarshal(ev.Payload, &p) == nil && p.Phase == SandboxPhaseFailed {
					failed = append(failed, p.Message)
				}
			}
			if got := strings.Join(failed, "|"); got != tc.want {
				t.Errorf("failed statuses = %q, want %q", got, tc.want)
			}
		})
	}
}

// A restarted session-manager reuses a live sandbox from its descriptor only once
// the folder is mounted again, and returns the error while it is not.
func TestCachedSandbox_WaitsForFolder(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	reg := folderRegistry(t, nil)
	dir, _ := reg.runDir("run-1")
	if err := writeSandboxDescriptor(dir, Sandbox{Endpoint: ln.Addr().String(), ExecToken: "tok", Folder: "pod-uid"}); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := reg.cachedSandbox(context.Background(), "run-1"); ok || !errors.Is(err, errSandboxFolder) {
		t.Errorf("cachedSandbox without the mount = %v, %v, want errSandboxFolder", ok, err)
	}
	setMountInfo(t, reg, folderMount(reg, "run-1", "pod-uid"))
	if endpoint, ok, err := reg.cachedSandbox(context.Background(), "run-1"); !ok || err != nil || endpoint != ln.Addr().String() {
		t.Errorf("cachedSandbox with the mount = %q, %v, %v, want the live endpoint", endpoint, ok, err)
	}
}

// The descriptor records the folder's sandbox pod, so a restarted session-manager can wait for the mount again.
func TestSandboxDescriptor_RecordsFolder(t *testing.T) {
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), nil, time.Minute, testBrand)
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
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), workspaces, nil, time.Minute, testBrand)
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

// The run sees the fixed text for each reason, and an unknown reason maps to the
// mount-failed text, so no node, address or port in a report reaches the run.
func TestMountFailure_RunFacingTextNamesNoNode(t *testing.T) {
	reg := folderRegistry(t, nil)
	for content, want := range map[string]string{
		"pod-uid\n" + sandboxfolder.ReasonUnreachable + "\n":                                     mountFailureText[sandboxfolder.ReasonUnreachable],
		"pod-uid\n" + sandboxfolder.ReasonMountFailed + "\n":                                     mountFailureText[sandboxfolder.ReasonMountFailed],
		"pod-uid\nnode ip-10-0-2-36 at 10.0.2.36 refused TCP 20490 on /var/lib/kubelet/pods/x\n": mountFailureText[sandboxfolder.ReasonMountFailed],
	} {
		writeReport(t, reg, content)
		if got := reg.mountFailure("run-1", "pod-uid"); got != want {
			t.Errorf("report %q gave %q, want %q", content, got, want)
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
