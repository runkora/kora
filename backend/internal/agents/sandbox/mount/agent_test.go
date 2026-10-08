package mount

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clear-route/orbit/internal/platform/logging"
)

type fakeExporter struct{ applied [][]export }

func (f *fakeExporter) Apply(e []export) error { f.applied = append(f.applied, e); return nil }

type fakeMounter struct {
	current    map[string]string
	volumeDir  string
	resolveErr error
	mounted    []string
	unmounted  []string
}

func (f *fakeMounter) Mounts() (map[string]string, error) {
	out := map[string]string{}
	for k, v := range f.current {
		out[k] = v
	}
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

type fakeStatus struct{ patched []string }

func (f *fakeStatus) PatchStatus(_ context.Context, ns, name string, patch []byte) error {
	f.patched = append(f.patched, ns+"/"+name+" "+string(patch))
	return nil
}

func newTestAgent(node string, m *fakeMounter) (*Agent, *fakeExporter, *fakeStatus) {
	e, s := &fakeExporter{}, &fakeStatus{}
	return &Agent{cfg: Config{Node: node, KubeletDir: "/k"}, exports: e, mounts: m, status: s, logger: logging.NewLoggerWithOutput(io.Discard)}, e, s
}

func lookup(pods ...*corev1.Pod) func(string, string) *corev1.Pod {
	return func(ns, name string) *corev1.Pod {
		for _, p := range pods {
			if p.Namespace == ns && p.Name == name {
				return p
			}
		}
		return nil
	}
}

const testTarget = "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount/run-1/sandbox"

// A new sandbox is mounted under the session-manager's volume and marked mounted.
func TestReconcile_MountsAndMarksSandbox(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, volumeDir: "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount"}
	a, _, s := newTestAgent("node-b", m)
	sb, ws := sandboxPod("node-a", "10.0.0.1"), workspacePod("node-b", "10.0.0.2")

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.mounted) != 1 || m.mounted[0] != "10.0.0.1:/sb-uid "+testTarget {
		t.Errorf("mounted = %v", m.mounted)
	}
	if len(s.patched) != 1 || !strings.HasPrefix(s.patched[0], "tenant/sandbox-run-1 ") || !strings.Contains(s.patched[0], `"type":"orbit.io/workspace-mounted","status":"True"`) {
		t.Errorf("patched = %v", s.patched)
	}

	// A second pass with the condition set and the mount in place changes nothing.
	sb.Status.Conditions = []corev1.PodCondition{{Type: MountedCondition, Status: corev1.ConditionTrue}}
	m.mounted = nil
	if err := a.Reconcile(context.Background(), []*corev1.Pod{sb}, lookup(ws)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(m.mounted) != 0 || len(m.unmounted) != 0 || len(s.patched) != 1 {
		t.Errorf("second pass mounted %v, unmounted %v, patched %v", m.mounted, m.unmounted, s.patched)
	}
}

// A sandbox that is deleting loses its mount, and so does a sandbox no pod lists any more.
func TestReconcile_UnmountsGoneSandboxes(t *testing.T) {
	m := &fakeMounter{current: map[string]string{
		testTarget: "10.0.0.1:/sb-uid",
		"/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount/run-9/sandbox": "10.0.0.1:/old",
	}, volumeDir: "/k/pods/ws-uid/volumes/kubernetes.io~csi/pv/mount"}
	a, _, _ := newTestAgent("node-b", m)
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
	a, _, _ := newTestAgent("node-b", m)

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
	a, e, _ := newTestAgent("node-a", m)
	unannotated := sandboxPod("node-a", "10.0.0.1")
	delete(unannotated.Annotations, WorkspacePodAnnotation)

	if err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1"), unannotated}, lookup()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(e.applied) != 1 || len(e.applied[0]) != 0 || len(m.mounted) != 0 {
		t.Errorf("applied %v, mounted %v", e.applied, m.mounted)
	}
}

// A workspace that cannot be resolved yet is reported, and the sandbox is not marked mounted.
func TestReconcile_ReportsUnresolvedWorkspace(t *testing.T) {
	m := &fakeMounter{current: map[string]string{}, resolveErr: errors.New("not yet")}
	a, _, s := newTestAgent("node-b", m)

	err := a.Reconcile(context.Background(), []*corev1.Pod{sandboxPod("node-a", "10.0.0.1")}, lookup(workspacePod("node-b", "10.0.0.2")))
	if err == nil || !strings.Contains(err.Error(), "not yet") {
		t.Errorf("err = %v, want the resolve error", err)
	}
	if len(s.patched) != 0 {
		t.Errorf("patched = %v, want none", s.patched)
	}
}
