// Package k8sfile implements credentials.SecretResolver by reading
// Kubernetes-secret-mounted files under a configured root directory. The
// gateway projects per-tenant secrets at this root via standard Kubernetes
// volume mounts; the connection's SecretRef is interpreted as a relative path
// under that root.
//
// Reference shapes accepted by Resolve:
//
//	"acme/github/app.pem"               → {root}/acme/github/app.pem
//	"connections/orbit-prod-github.pem" → {root}/connections/orbit-prod-github.pem
//
// Path traversal is rejected: a SecretRef containing ".." or starting with "/"
// returns an error without touching the filesystem.
//
// Hot-reload on rotation is not implemented. Every Resolve call reads the
// file fresh, which means rotation is picked up on the next request. That's
// acceptable for credential minting where each Resolve call is a one-shot
// secret read.
package k8sfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// ErrInvalidRef is returned when a SecretRef is empty, contains "..", or is
// absolute. The egress gateway treats this as a programmer/config error
// (operator misconfigured the connection), never a runtime credential issue.
var ErrInvalidRef = errors.New("invalid secret reference")

// Resolver reads secret material under a fixed root directory.
type Resolver struct {
	root   string
	logger logging.Logger
}

// New constructs a Resolver. root must be an existing directory the gateway
// has read access to. Returns an error if root is empty or unreadable so
// startup fails loudly when the secret mount is misconfigured.
func New(ctx context.Context, root string, logger logging.Logger) (*Resolver, error) {
	if root == "" {
		return nil, fmt.Errorf("k8sfile: root is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		// Tolerate a missing root the same way cabundle does. The operator may
		// not have mounted the secret yet. Resolve calls will fail individually.
		if os.IsNotExist(err) {
			logger.Warn(ctx, "connection secret root does not exist; every Resolve will fail until the secret is mounted",
				logging.Field{Key: "root", Value: root})
			return &Resolver{root: root, logger: logger}, nil
		}
		return nil, fmt.Errorf("stat root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("k8sfile: root %q is not a directory", root)
	}
	return &Resolver{root: root, logger: logger}, nil
}

// Resolve reads the secret material identified by ref, a relative path under
// the configured root.
//
// Containment is checked against the resolved path, not the literal one.
// filepath.IsLocal rejects absolute paths and dot segments, but a lexically
// clean ref can still name a symlink inside the mount that points anywhere on
// the host, so the target is resolved and compared with the resolved root.
//
// os.Root is not usable here: Kubernetes projects ConfigMap and Secret volumes
// through a "..data" symlink whose target begins with "..", which os.Root
// refuses even though it stays inside the mount.
func (r *Resolver) Resolve(_ context.Context, ref string) ([]byte, error) {
	if ref == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidRef)
	}
	if !filepath.IsLocal(ref) {
		return nil, fmt.Errorf("%w: not a path under the secret root: %q", ErrInvalidRef, ref)
	}

	// The root itself may be a symlink, so resolve both sides before comparing.
	root, err := filepath.EvalSymlinks(r.root)
	if err != nil {
		return nil, fmt.Errorf("resolve secret root: %w", err)
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, filepath.Clean(ref)))
	if err != nil {
		return nil, fmt.Errorf("read secret %q: %w", ref, err)
	}
	rel, err := filepath.Rel(root, full)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("%w: resolves outside the secret root: %q", ErrInvalidRef, ref)
	}

	data, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("read secret %q: %w", ref, err)
	}
	return data, nil
}
