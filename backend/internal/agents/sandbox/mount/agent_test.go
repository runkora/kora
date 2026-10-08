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

type fakeMounter struct {
	current    map[string]string
	volumeDir  string
	resolveErr error
	mounted    []string
	unmounted  []string
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

func newTestAgent(node string, m *fakeMounter) (*Agent, *fakeExporter) {
	e := &fakeExporter{}
	return &Agent{cfg: Config{Node: node, KubeletDir: "/k"}, exports: e, mounts: m, logger: logging.NewLoggerWithOutput(io.Discard)}, e
}

func lookup(pods ...*corev1.Pod) []*corev1.Pod { return pods }

const testTarget = "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount/run-1/sandbox"

// A new sandbox is mounted under the session-manager's volume, once.
func TestReconcile_MountsSandbox(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, volumeDir: "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount"}
	a, _ := newTestAgent("node-b", m)
	sb, ws := sandboxPod("node-a", "10.0.0.1"), workspacePod("node-b", "10.0.0.2")

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.mounted) != 1 || m.mounted[0] != "10.0.0.1:/sb-uid "+testTarget {
		t.Errorf("mounted = %v", m.mounted)
	}

	// A second pass with the mount in place changes nothing.
	m.mounted = nil
	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.mounted) != 0 || len(m.unmounted) != 0 {
		t.Errorf("second pass mounted %v, unmounted %v", m.mounted, m.unmounted)
	}
}

// A sandbox that is deleting loses its mount, and so does a sandbox no pod lists any more.
func TestReconcile_UnmountsGoneSandboxes(t *testing.T) {
	m := &fakeMounter{current: map[string]string{
		testTarget: "10.0.0.1:/sb-uid",
		"/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount/run-9/sandbox": "10.0.0.1:/old",
	}, volumeDir: "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount"}
	a, _ := newTestAgent("node-b", m)
	sb, ws := sandboxPod("node-a", "10.0.0.1"), workspacePod("node-b", "10.0.0.2")
	now := metav1.Now()
	sb.DeletionTimestamp = &now

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.unmounted) != 2 || len(m.current) != 0 {
		t.Errorf("unmounted = %v, left = %v", m.unmounted, m.current)
	}
}

// A replacement sandbox for the same run has a new UID: the old mount comes off before the new one goes on.
func TestReconcile_RemountsReplacedSandbox(t *testing.T) {
	m := &fakeMounter{current: map[string]string{testTarget: "10.0.0.1:/old-uid"}, volumeDir: "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount"}
	a, _ := newTestAgent("node-b", m)

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1")}, lookup(workspacePod("node-b", "10.0.0.2"))); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.unmounted) != 1 || len(m.mounted) != 1 || m.current[testTarget] != "10.0.0.1:/sb-uid" {
		t.Errorf("unmounted %v, mounted %v, current %v", m.unmounted, m.mounted, m.current)
	}
}

// A sandbox whose session-manager pod is gone, or that names none, is neither exported nor mounted.
func TestReconcile_IgnoresUnpairedSandboxes(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}}
	a, e := newTestAgent("node-a", m)
	unannotated := sandboxPod("node-a", "10.0.0.1")
	delete(unannotated.Annotations, WorkspacePodAnnotation)

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1"), unannotated}, lookup()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(e.applied) != 1 || len(e.applied[0]) != 0 || len(m.mounted) != 0 {
		t.Errorf("applied %v, mounted %v", e.applied, m.mounted)
	}
}

// A sandbox names its session-manager pod by name only, so it never pairs with a same-named pod in another tenant's namespace.
func TestReconcile_DoesNotPairAcrossNamespaces(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, volumeDir: "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount"}
	a, e := newTestAgent("node-b", m)
	other := workspacePod("node-b", "10.0.0.2")
	other.Namespace = "other-tenant"

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-b", "10.0.0.2")}, lookup(other)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(e.applied[0]) != 0 || len(m.mounted) != 0 {
		t.Errorf("applied %v, mounted %v, want nothing", e.applied, m.mounted)
	}
}

// A workspace that cannot be resolved yet is reported and nothing is mounted.
func TestReconcile_ReportsUnresolvedWorkspace(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, resolveErr: errors.New("not yet")}
	a, _ := newTestAgent("node-b", m)

	err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1")}, lookup(workspacePod("node-b", "10.0.0.2")))
	if err == nil || !strings.Contains(err.Error(), "not yet") {
		t.Errorf("err = %v, want the resolve error", err)
	}
	if len(m.mounted) != 0 {
		t.Errorf("mounted = %v, want none", m.mounted)
	}
}

// Copies of a mount that the host propagates to other binds of the volume, such as a co-located
// sandbox pod's, and NFS mounts of unrelated pods are left alone.
func TestReconcile_IgnoresMountsOutsideLocalWorkspaces(t *testing.T) {
	m := &fakeMounter{current: map[string]string{
		testTarget: "10.0.0.1:/sb-uid",
		"/k/pods/sb-uid/volumes/kubernetes.io~csi/pv/mount/run-1/sandbox": "10.0.0.1:/sb-uid",
		"/k/pods/other/volumes/kubernetes.io~nfs/sandbox":                 "nas:/export",
	}, volumeDir: "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount"}
	a, _ := newTestAgent("node-b", m)

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1")}, lookup(workspacePod("node-b", "10.0.0.2"))); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.unmounted) != 0 || len(m.mounted) != 0 {
		t.Errorf("unmounted %v, mounted %v, want neither", m.unmounted, m.mounted)
	}
}
