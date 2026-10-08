package mount

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// exporter serves sandbox folders from this node.
type exporter interface {
	// Apply makes the node's exports exactly the given set.
	Apply(ctx context.Context, exports []export) error
}

// mounter mounts sandbox folders into session-manager volumes on this node.
type mounter interface {
	// Mounts returns every mount at a sandbox folder path on the node, keyed by mount point, with its source.
	Mounts() (map[string]string, error)
	// Resolve returns the directory of the session-manager pod's volume that holds the run's workspace.
	Resolve(workspaceUID, runID string) (string, error)
	// Mount mounts source on rel beneath volumeDir without following symlinks.
	Mount(source, volumeDir, rel string) error
	// Unmount detaches the mount at path, failing any I/O still in flight on it.
	Unmount(path string) error
	// Release detaches propagated copies of the given sources' mounts below the given pods' directories.
	Release(podUIDs []string, sources map[string]bool) error
}

// Agent reconciles the node's exports and mounts with the sandbox pods.
type Agent struct {
	cfg     Config
	kube    kubernetes.Interface
	exports exporter
	mounts  mounter
	ledger  *ledger
	logger  logging.Logger
}

// Reconcile exports and mounts every sandbox folder the given pods call for on
// this node, and removes the rest.
func (a *Agent) Reconcile(ctx context.Context, sandboxes, workspaces []*corev1.Pod) error {
	byName := make(map[string]*corev1.Pod, len(workspaces))
	for _, ws := range workspaces {
		byName[ws.Namespace+"/"+ws.Name] = ws
	}
	var pairs []pair
	for _, sb := range sandboxes {
		name := sb.Annotations[WorkspacePodAnnotation]
		if name == "" {
			continue
		}
		if ws := byName[sb.Namespace+"/"+name]; ws != nil {
			pairs = append(pairs, pair{sandbox: sb, workspace: ws})
		}
	}
	exports, targets := plan(a.cfg.Node, a.cfg.KubeletDir, pairs)
	// Release matches the sources held before this pass too. An unmount here removes
	// a copy from every bind still sharing propagation, and one made private earlier keeps it.
	sources := a.ledger.sources()
	syncErr := a.syncMounts(ctx, targets)
	maps.Copy(sources, a.ledger.sources())
	return errors.Join(a.exports.Apply(ctx, exports), syncErr, a.mounts.Release(dying(a.cfg.Node, sandboxes), sources))
}

// dying returns the UIDs of the sandbox pods on node that are deleting or finished.
// The host propagated other runs' folder mounts into their volume binds, and
// kubelet cannot tear those binds down until the copies are released.
func dying(node string, sandboxes []*corev1.Pod) []string {
	var uids []string
	for _, sb := range sandboxes {
		if sb.Spec.NodeName == node && !live(sb) && sb.Status.Phase != corev1.PodPending {
			uids = append(uids, string(sb.UID))
		}
	}
	return uids
}

// wanted is a mount the node should hold.
type wanted struct {
	target
	volumeDir string
}

// syncMounts unmounts what is no longer wanted, then mounts what is missing. It
// touches only the mounts its ledger lists with the source mountinfo shows, and
// it never mounts over a mount it did not make. Copies the host propagates into
// other binds of the same volume carry the same source at other paths. They go
// when the original does.
func (a *Agent) syncMounts(ctx context.Context, targets []target) error {
	all, err := a.mounts.Mounts()
	if err != nil {
		return fmt.Errorf("read mounts: %w", err)
	}
	for path, source := range a.ledger.snapshot() {
		if all[path] != source {
			delete(a.ledger.entries, path)
		}
	}
	var errs []error
	want := make(map[string]wanted, len(targets))
	for _, t := range targets {
		dir, err := a.mounts.Resolve(t.workspaceUID, t.runID)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolve workspace of run %s: %w", t.runID, err))
			continue
		}
		want[filepath.Join(dir, t.runID, Dir)] = wanted{target: t, volumeDir: dir}
	}
	for path, source := range a.ledger.snapshot() {
		if w, ok := want[path]; ok && w.source == source {
			continue
		}
		if err := a.mounts.Unmount(path); err != nil {
			errs = append(errs, fmt.Errorf("unmount %s: %w", path, err))
			continue
		}
		delete(a.ledger.entries, path)
		delete(all, path)
		a.logger.Info(ctx, "Unmounted sandbox folder", logging.Field{Key: "path", Value: path}, logging.Field{Key: "source", Value: source})
	}
	for path, w := range want {
		if a.ledger.entries[path] == w.source {
			continue
		}
		if foreign, ok := all[path]; ok {
			errs = append(errs, fmt.Errorf("%w: %s holds %s", errForeignMount, path, foreign))
			continue
		}
		if err := a.mounts.Mount(w.source, w.volumeDir, filepath.Join(w.runID, Dir)); err != nil {
			errs = append(errs, fmt.Errorf("mount %s on %s: %w", w.source, path, err))
			continue
		}
		a.ledger.entries[path] = w.source
		a.logger.Info(ctx, "Mounted sandbox folder", logging.Field{Key: "run_id", Value: w.runID}, logging.Field{Key: "source", Value: w.source})
	}
	if err := a.ledger.save(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// errForeignMount reports a mount the agent did not make at a path it would mount on.
var errForeignMount = errors.New("a mount the node agent did not make is in the way")
