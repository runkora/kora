package k8sfile_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials/resolvers/k8sfile"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// TestResolve_SymlinkOutOfRootRefused locks containment against a symlink
// inside the mount that points outside it. A lexical path check passes such a
// ref, because nothing in the string names a parent directory.
func TestResolve_SymlinkOutOfRootRefused(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "host-secret")
	if err := os.WriteFile(outside, []byte("SECRET-OUTSIDE-ROOT"), 0o600); err != nil {
		t.Fatalf("write outside secret: %v", err)
	}

	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	r, err := k8sfile.New(t.Context(), root, logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := r.Resolve(t.Context(), "escape"); err == nil {
		t.Fatal("Resolve followed a symlink out of the secret root")
	}
}

// TestResolve_KubernetesDataSymlinkFollowed locks the other side: Kubernetes
// projects ConfigMap and Secret volumes through a "..data" symlink chain, and
// containment must not break that.
func TestResolve_KubernetesDataSymlinkFollowed(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "..2026_09_09_10_00_00")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatalf("mkdir timestamped dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "token"), []byte("mounted"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(root, "..data")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join("..data", "token"), filepath.Join(root, "token")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	r, err := k8sfile.New(t.Context(), root, logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := r.Resolve(t.Context(), "token")
	if err != nil {
		t.Fatalf("Resolve through the ..data chain: %v", err)
	}
	if string(got) != "mounted" {
		t.Errorf("Resolve = %q, want %q", got, "mounted")
	}
}

// TestResolve_NonLocalRefsRefused locks the lexical rejections.
func TestResolve_NonLocalRefsRefused(t *testing.T) {
	r, err := k8sfile.New(t.Context(), t.TempDir(), logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, ref := range []string{"/etc/passwd", "../outside", "a/../../outside", ""} {
		if _, err := r.Resolve(t.Context(), ref); !errors.Is(err, k8sfile.ErrInvalidRef) {
			t.Errorf("Resolve(%q) error = %v, want ErrInvalidRef", ref, err)
		}
	}
}
