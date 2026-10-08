package mount

import (
	"context"
	"errors"
	"io"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clear-route/orbit/internal/platform/logging"
)

type fakeExporter struct{ applied [][]export }

func (f *fakeExporter) Apply(_ context.Context, e []export) error {
	f.applied = append(f.applied, e)
	return nil
}

// fakeMounter stands in for the node's mount table: current is what mountinfo shows.
type fakeMounter struct {
	current    map[string]string
	volumeDir  string
	resolveErr error
	mounted    []string
	unmounted  []string
	released   []string
	sources    map[string]bool
	reports    map[string]string
}

func (f *fakeMounter) Report(_, runID, content string) error {
	if f.reports == nil {
		f.reports = map[string]string{}
	}
	f.reports[runID] = content
	return nil
}

func (f *fakeMounter) Mounts() (map[string]string, error) {
	out := map[string]string{}
	maps.Copy(out, f.current)
	return out, nil
}

func (f *fakeMounter) Resolve(string, string) (string, error) { return f.volumeDir, f.resolveErr }

func (f *fakeMounter) Mount(source, dir, rel string) error {
	f.mounted = append(f.mounted, source+" "+filepath.Join(dir, rel))
	f.current[filepath.Join(dir, rel)] = source
	return nil
}

func (f *fakeMounter) Unmount(path string) error {
	f.unmounted = append(f.unmounted, path)
	delete(f.current, path)
	return nil
}

func (f *fakeMounter) Release(uids []string, sources map[string]bool) error {
	f.released = append(f.released, uids...)
	f.sources = sources
	return nil
}

// newTestAgent builds an agent whose ledger already records the given mounts.
func newTestAgent(t *testing.T, node string, m *fakeMounter, recorded map[string]string) (*Agent, *fakeExporter) {
	t.Helper()
	l, err := loadLedger(filepath.Join(t.TempDir(), "plugins", "orbit-sandbox-mount", "mounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(l.entries, recorded)
	e := &fakeExporter{}
	reach := func(context.Context, string) error { return nil }
	return &Agent{cfg: Config{Node: node, KubeletDir: "/k", Port: 20490}, exports: e, mounts: m, ledger: l, reach: reach, logger: logging.NewLoggerWithOutput(io.Discard)}, e
}

func lookup(pods ...*corev1.Pod) []*corev1.Pod { return pods }

const (
	testVolume = "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount"
	testTarget = testVolume + "/run-1/sandbox"
)

// A new sandbox is mounted under the session-manager's volume once, and the ledger records it.
func TestReconcile_MountsSandbox(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, volumeDir: testVolume}
	a, _ := newTestAgent(t, "node-b", m, nil)
	sb, ws := sandboxPod("node-a", "10.0.0.1"), workspacePod("node-b", "10.0.0.2")

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.mounted) != 1 || m.mounted[0] != "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001 "+testTarget {
		t.Errorf("mounted = %v", m.mounted)
	}
	saved, err := loadLedger(a.ledger.file)
	if err != nil || saved.entries[testTarget] != "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001" {
		t.Errorf("saved ledger = %+v, %v, want the new mount", saved, err)
	}

	m.mounted = nil
	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.mounted) != 0 || len(m.unmounted) != 0 {
		t.Errorf("second pass mounted %v, unmounted %v", m.mounted, m.unmounted)
	}
}

// A recorded mount whose sandbox is deleting, or that no pod asks for any more, is unmounted and forgotten.
func TestReconcile_UnmountsGoneSandboxes(t *testing.T) {
	old := testVolume + "/run-9/sandbox"
	m := &fakeMounter{current: map[string]string{testTarget: "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001", old: "10.0.0.1:/old"}, volumeDir: testVolume}
	a, _ := newTestAgent(t, "node-b", m, map[string]string{testTarget: "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001", old: "10.0.0.1:/old"})
	sb, ws := sandboxPod("node-a", "10.0.0.1"), workspacePod("node-b", "10.0.0.2")
	now := metav1.Now()
	sb.DeletionTimestamp = &now

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.unmounted) != 2 || len(a.ledger.entries) != 0 {
		t.Errorf("unmounted = %v, ledger = %v", m.unmounted, a.ledger.entries)
	}
}

// A replacement sandbox for the same run has a new UID: the old mount comes off before the new one goes on.
func TestReconcile_RemountsReplacedSandbox(t *testing.T) {
	m := &fakeMounter{current: map[string]string{testTarget: "10.0.0.1:/old-uid"}, volumeDir: testVolume}
	a, _ := newTestAgent(t, "node-b", m, map[string]string{testTarget: "10.0.0.1:/old-uid"})

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1")}, lookup(workspacePod("node-b", "10.0.0.2"))); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.unmounted) != 1 || len(m.mounted) != 1 || m.current[testTarget] != "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001" || a.ledger.entries[testTarget] != "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001" {
		t.Errorf("unmounted %v, mounted %v, current %v, ledger %v", m.unmounted, m.mounted, m.current, a.ledger.entries)
	}
}

// Mounts the agent did not make are never unmounted: a lookalike at a sandbox folder
// path with a sandbox-like source, an unrelated NFS volume named sandbox, and the
// copies the host propagates into other binds of the volume.
func TestReconcile_LeavesMountsItDidNotMake(t *testing.T) {
	m := &fakeMounter{current: map[string]string{
		testTarget:                    "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001",
		testVolume + "/run-2/sandbox": "10.0.0.1:/forged-uid",
		"/k/pods/other/volumes/kubernetes.io~nfs/sandbox":                                               "nas:/export",
		"/k/pods/0d6f0f3e-1111-4a8b-9c2d-000000000001/volumes/kubernetes.io~csi/pv/mount/run-1/sandbox": "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001",
	}, volumeDir: testVolume}
	a, _ := newTestAgent(t, "node-b", m, map[string]string{testTarget: "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000001"})

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1")}, lookup(workspacePod("node-b", "10.0.0.2"))); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.unmounted) != 0 || len(m.mounted) != 0 {
		t.Errorf("unmounted %v, mounted %v, want neither", m.unmounted, m.mounted)
	}
}

// A mount the agent did not make at the path it would mount on is neither unmounted nor covered.
func TestReconcile_RefusesToMountOverForeignMount(t *testing.T) {
	m := &fakeMounter{current: map[string]string{testTarget: "tmpfs"}, volumeDir: testVolume}
	a, _ := newTestAgent(t, "node-b", m, nil)

	err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1")}, lookup(workspacePod("node-b", "10.0.0.2")))
	if !errors.Is(err, errForeignMount) {
		t.Errorf("err = %v, want errForeignMount", err)
	}
	if len(m.unmounted) != 0 || len(m.mounted) != 0 {
		t.Errorf("unmounted %v, mounted %v, want neither", m.unmounted, m.mounted)
	}
}

// A recorded path that now holds a different source was replaced by someone else.
// The agent forgets it and does not unmount it.
func TestReconcile_ForgetsReplacedRecords(t *testing.T) {
	old := testVolume + "/run-9/sandbox"
	m := &fakeMounter{current: map[string]string{old: "10.0.0.9:/someone-else"}, volumeDir: testVolume}
	a, _ := newTestAgent(t, "node-b", m, map[string]string{old: "10.0.0.1:/old"})

	if err := a.Reconcile(context.Background(), nil, nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.unmounted) != 0 || len(a.ledger.entries) != 0 {
		t.Errorf("unmounted %v, ledger %v", m.unmounted, a.ledger.entries)
	}
}

// A sandbox names its session-manager pod by name only, so it never pairs with a same-named pod in another tenant's namespace.
func TestReconcile_DoesNotPairAcrossNamespaces(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, volumeDir: testVolume}
	a, e := newTestAgent(t, "node-b", m, nil)
	other := workspacePod("node-b", "10.0.0.2")
	other.Namespace = "other-tenant"

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-b", "10.0.0.2")}, lookup(other)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(e.applied[0]) != 0 || len(m.mounted) != 0 {
		t.Errorf("applied %v, mounted %v, want nothing", e.applied, m.mounted)
	}
}

// A sandbox whose session-manager pod is gone, or that names none, is neither exported nor mounted.
func TestReconcile_IgnoresUnpairedSandboxes(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}}
	a, e := newTestAgent(t, "node-a", m, nil)
	unannotated := sandboxPod("node-a", "10.0.0.1")
	delete(unannotated.Annotations, WorkspacePodAnnotation)

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1"), unannotated}, lookup()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(e.applied) != 1 || len(e.applied[0]) != 0 || len(m.mounted) != 0 {
		t.Errorf("applied %v, mounted %v", e.applied, m.mounted)
	}
}

// A workspace that cannot be resolved yet is reported and nothing is mounted.
func TestReconcile_ReportsUnresolvedWorkspace(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, resolveErr: errors.New("not yet")}
	a, _ := newTestAgent(t, "node-b", m, nil)

	err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1")}, lookup(workspacePod("node-b", "10.0.0.2")))
	if err == nil || !strings.Contains(err.Error(), "not yet") {
		t.Errorf("err = %v, want the resolve error", err)
	}
	if len(m.mounted) != 0 {
		t.Errorf("mounted = %v, want none", m.mounted)
	}
}

// A sandbox on this node that is deleting or finished has the copies of the agent's own mounts
// under its volume binds released. A live sandbox and a sandbox on another node do not.
func TestReconcile_ReleasesCopiesUnderDyingSandboxes(t *testing.T) {
	gone := testVolume + "/run-9/sandbox"
	m := &fakeMounter{current: map[string]string{testTarget: "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000002", gone: "10.0.0.1:/gone"}, volumeDir: testVolume}
	a, _ := newTestAgent(t, "node-a", m, map[string]string{testTarget: "10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000002", gone: "10.0.0.1:/gone"})
	now := metav1.Now()
	deleting, finished, live, elsewhere := sandboxPod("node-a", "10.0.0.1"), sandboxPod("node-a", "10.0.0.1"), sandboxPod("node-a", "10.0.0.1"), sandboxPod("node-b", "10.0.0.2")
	deleting.UID, finished.UID, live.UID, elsewhere.UID = "deleting", "finished", "0d6f0f3e-1111-4a8b-9c2d-000000000002", "elsewhere"
	deleting.DeletionTimestamp = &now
	finished.Status.Phase = corev1.PodSucceeded
	elsewhere.DeletionTimestamp = &now

	if err := a.Reconcile(context.Background(), []*corev1.Pod{deleting, finished, live, elsewhere}, lookup(workspacePod("node-a", "10.0.0.1"))); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if strings.Join(m.released, ",") != "deleting,finished" {
		t.Errorf("released = %v, want deleting and finished", m.released)
	}
	// The agent's own mounts, including one this pass unmounted, and nothing else.
	if !m.sources["10.0.0.1:/0d6f0f3e-1111-4a8b-9c2d-000000000002"] || !m.sources["10.0.0.1:/gone"] || len(m.sources) != 2 {
		t.Errorf("release sources = %v, want the agent's own mounts", m.sources)
	}
}

// When the NFS port on the sandbox's node cannot be reached, nothing is mounted and the run's
// report names the port and both nodes. A later successful mount clears the report.
func TestReconcile_ReportsUnreachableNFSPort(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, volumeDir: testVolume}
	a, _ := newTestAgent(t, "node-b", m, nil)
	var dialed string
	a.reach = func(_ context.Context, addr string) error { dialed = addr; return errors.New("i/o timeout") }
	sb, ws := sandboxPod("node-a", "10.0.0.1"), workspacePod("node-b", "10.0.0.2")

	err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws))
	if err == nil || !strings.Contains(err.Error(), "TCP port 20490") {
		t.Errorf("err = %v, want the port named", err)
	}
	if dialed != "10.0.0.1:20490" || len(m.mounted) != 0 {
		t.Errorf("dialed %q, mounted %v", dialed, m.mounted)
	}
	// The report carries a reason code only. The nodes and the port stay in the agent's error and log.
	if got, want := m.reports["run-1"], string(sb.UID)+"\n"+ReasonUnreachable+"\n"; got != want {
		t.Errorf("report = %q, want %q", got, want)
	}
	for _, leak := range []string{"node-a", "node-b", "10.0.0.1", "10.0.0.2", "20490"} {
		if strings.Contains(m.reports["run-1"], leak) {
			t.Errorf("report names %q", leak)
		}
	}
	if !strings.Contains(err.Error(), "node node-b cannot reach the NFS server on node node-a") {
		t.Errorf("err = %v, want the nodes named for the operator", err)
	}

	a.reach = func(context.Context, string) error { return nil }
	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.mounted) != 1 || m.reports["run-1"] != "" {
		t.Errorf("mounted %v, report %q, want a mount and a cleared report", m.mounted, m.reports["run-1"])
	}
}
