package k8sfile

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestResolve_ReadsRelativeFile(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "acme", "github")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "app.pem"), []byte("PEMBYTES"), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := New(root, logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := r.Resolve(context.Background(), "acme/github/app.pem")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != "PEMBYTES" {
		t.Errorf("got %q, want PEMBYTES", got)
	}
}

func TestResolve_RejectsEmpty(t *testing.T) {
	r := &Resolver{root: t.TempDir(), logger: logging.NewLoggerWithOutput(io.Discard)}
	if _, err := r.Resolve(context.Background(), ""); !errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v, want ErrInvalidRef", err)
	}
}

func TestResolve_RejectsAbsolutePath(t *testing.T) {
	r := &Resolver{root: t.TempDir(), logger: logging.NewLoggerWithOutput(io.Discard)}
	if _, err := r.Resolve(context.Background(), "/etc/passwd"); !errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v, want ErrInvalidRef", err)
	}
}

func TestResolve_RejectsParentTraversal(t *testing.T) {
	r := &Resolver{root: t.TempDir(), logger: logging.NewLoggerWithOutput(io.Discard)}
	for _, ref := range []string{"../etc/passwd", "x/../../etc/passwd", ".."} {
		if _, err := r.Resolve(context.Background(), ref); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("ref=%q: err = %v, want ErrInvalidRef", ref, err)
		}
	}
}

func TestResolve_MissingFile(t *testing.T) {
	r := &Resolver{root: t.TempDir(), logger: logging.NewLoggerWithOutput(io.Discard)}
	_, err := r.Resolve(context.Background(), "absent")
	if err == nil || errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v, want a read error (not ErrInvalidRef)", err)
	}
}

func TestNew_RootMustExist(t *testing.T) {
	if _, err := New("", logging.NewLoggerWithOutput(io.Discard)); err == nil {
		t.Error("expected error on empty root")
	}
	tmp := t.TempDir()
	file := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(file, logging.NewLoggerWithOutput(io.Discard)); err == nil {
		t.Error("expected error when root is a file")
	}
}

func TestNew_TolerantOfMissingRoot(t *testing.T) {
	// Operator hasn't mounted the secret yet. New should succeed; Resolve fails per call.
	logger := logging.NewLoggerWithOutput(io.Discard)
	r, err := New("/no/such/dir/at/all", logger)
	if err != nil {
		t.Fatalf("New on missing dir: want graceful start, got: %v", err)
	}
	if _, err := r.Resolve(context.Background(), "x"); err == nil {
		t.Error("expected Resolve to fail when root absent")
	}
}
