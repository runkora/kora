package mount

import (
	"context"
	"errors"
	"fmt"
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
	// Mounts returns the sandbox folder mounts on the node, keyed by mount point, with their sources.
	Mounts() (map[string]string, error)
	// Resolve returns the directory of the session-manager pod's volume that holds the run's workspace.
	Resolve(workspaceUID, runID string) (string, error)
	// Mount mounts source on rel beneath volumeDir without following symlinks.
	Mount(source, volumeDir, rel string) error
	// Unmount detaches the mount at path, failing any I/O still in flight on it.
	Unmount(path string) error
}

// Agent reconciles the node's exports and mounts with the sandbox pods.
type Agent struct {
	cfg     Config
	kube    kubernetes.Interface
	exports exporter
	mounts  mounter
	logger  logging.Logger
}

// Reconcile exports and mounts every sandbox folder the given pods call for on
// this node, and removes the rest. workspace returns a session-manager pod, or nil.
func (a *Agent) Reconcile(ctx context.Context, sandboxes []*corev1.Pod, workspace func(namespace, name string) *corev1.Pod) error {
	var pairs []pair
	for _, sb := range sandboxes {
		name := sb.Annotations[WorkspacePodAnnotation]
		if name == "" {
			continue
		}
		if ws := workspace(sb.Namespace, name); ws != nil {
			pairs = append(pairs, pair{sandbox: sb, workspace: ws})
		}
	}
	exports, targets := plan(a.cfg.Node, a.cfg.KubeletDir, pairs)
	return errors.Join(a.exports.Apply(ctx, exports), a.syncMounts(ctx, targets))
}

// wanted is a mount the node should hold.
type wanted struct {
	target
	volumeDir string
}

// syncMounts unmounts what is no longer wanted, then mounts what is missing.
func (a *Agent) syncMounts(ctx context.Context, targets []target) error {
	current, err := a.mounts.Mounts()
	if err != nil {
		return fmt.Errorf("read mounts: %w", err)
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
	for path, source := range current {
		if w, ok := want[path]; ok && w.source == source {
			continue
		}
		if err := a.mounts.Unmount(path); err != nil {
			errs = append(errs, fmt.Errorf("unmount %s: %w", path, err))
			continue
		}
		delete(current, path)
		a.logger.Info(ctx, "Unmounted sandbox folder", logging.Field{Key: "path", Value: path}, logging.Field{Key: "source", Value: source})
	}
	for path, w := range want {
		if _, ok := current[path]; ok {
			continue
		}
		if err := a.mounts.Mount(w.source, w.volumeDir, filepath.Join(w.runID, Dir)); err != nil {
			errs = append(errs, fmt.Errorf("mount %s on %s: %w", w.source, path, err))
			continue
		}
		a.logger.Info(ctx, "Mounted sandbox folder", logging.Field{Key: "run_id", Value: w.runID}, logging.Field{Key: "source", Value: w.source})
	}
	return errors.Join(errs...)
}
