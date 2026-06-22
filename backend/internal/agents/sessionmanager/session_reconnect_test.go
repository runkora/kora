package sessionmanager

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// reprovisionNotifier embeds NoopNotifier (satisfying the full Notifier
// interface) and records ProvisionSandbox calls, returning a fixed endpoint.
type reprovisionNotifier struct {
	NoopNotifier
	endpoint string
	calls    int
}

func (n *reprovisionNotifier) ProvisionSandbox(_ context.Context, _ string) (string, error) {
	n.calls++
	return n.endpoint, nil
}

func TestEndpointAlive(t *testing.T) {
	// A listening socket is alive.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close() //nolint:errcheck
	if !endpointAlive(ln.Addr().String()) {
		t.Fatalf("expected live listener %s to be alive", ln.Addr())
	}

	// A closed socket's address is dead (connection refused).
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	dead := ln2.Addr().String()
	ln2.Close() //nolint:errcheck
	if endpointAlive(dead) {
		t.Fatalf("expected closed address %s to be dead", dead)
	}
}

func newTestRegistry(t *testing.T, n Notifier) (*Registry, string) {
	t.Helper()
	workspaces := t.TempDir()
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), nil, workspaces, n, time.Minute)
	return reg, workspaces
}

func writeEndpoint(t *testing.T, workspaces, runID, endpoint string) string {
	t.Helper()
	dir := filepath.Join(workspaces, runID, ".sandbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(dir, "endpoint")
	if err := os.WriteFile(file, []byte(endpoint), 0o600); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}
	return file
}

func TestProvisionSandbox_LiveCacheReused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close() //nolint:errcheck
	live := ln.Addr().String()

	n := &reprovisionNotifier{endpoint: "10.0.0.9:50051"}
	reg, ws := newTestRegistry(t, n)
	writeEndpoint(t, ws, "run-1", live)

	got, err := reg.ProvisionSandbox(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ProvisionSandbox: %v", err)
	}
	if got != live {
		t.Fatalf("expected cached live endpoint %q, got %q", live, got)
	}
	if n.calls != 0 {
		t.Fatalf("expected no re-provision for a live cache, got %d calls", n.calls)
	}
}

func TestProvisionSandbox_DeadCacheReprovisions(t *testing.T) {
	// Get a definitely-dead address by opening then closing a listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	dead := ln.Addr().String()
	ln.Close() //nolint:errcheck

	const fresh = "10.0.0.42:50051"
	n := &reprovisionNotifier{endpoint: fresh}
	reg, ws := newTestRegistry(t, n)
	file := writeEndpoint(t, ws, "run-1", dead)

	got, err := reg.ProvisionSandbox(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ProvisionSandbox: %v", err)
	}
	if got != fresh {
		t.Fatalf("expected fresh endpoint %q after dead-cache re-provision, got %q", fresh, got)
	}
	if n.calls != 1 {
		t.Fatalf("expected exactly one re-provision, got %d", n.calls)
	}
	// The endpoint file must be rewritten with the fresh address so orbit-shell
	// picks it up on retry.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read endpoint file: %v", err)
	}
	if string(data) != fresh {
		t.Fatalf("expected endpoint file rewritten to %q, got %q", fresh, string(data))
	}
}
