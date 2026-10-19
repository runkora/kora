package sessionmanager

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// reprovisionNotifier embeds NoopNotifier (satisfying the full Notifier
// interface) and records ProvisionSandbox calls, returning a fixed endpoint.
type reprovisionNotifier struct {
	NoopNotifier
	endpoint string
	calls    int
}

func (n *reprovisionNotifier) ProvisionSandbox(_ context.Context, _ string, _ string, _ bool) (Sandbox, error) {
	n.calls++
	return Sandbox{Endpoint: n.endpoint, ExecToken: "tok-" + n.endpoint}, nil
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
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), workspaces, n, time.Minute, testBrand)
	return reg, workspaces
}

// hostSession registers a run without starting a runner, which is what
// ProvisionSandbox checks before it answers.
func hostSession(reg *Registry, runID string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.sessions[runID] = &session{runID: runID, done: make(chan struct{})}
}

// writeEndpoint seeds a cached sandbox descriptor. Both files are written
// because ProvisionSandbox treats a missing token as a cache miss.
func writeEndpoint(t *testing.T, workspaces, runID, endpoint string) string {
	t.Helper()
	dir := filepath.Join(workspaces, runID, ".sandbox-exec")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(dir, "endpoint")
	if err := os.WriteFile(file, []byte(endpoint), 0o600); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exec-token"), []byte("cached-token"), 0o600); err != nil {
		t.Fatalf("write exec token: %v", err)
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
	hostSession(reg, "run-1")
	writeEndpoint(t, ws, "run-1", live)

	got, err := reg.ProvisionSandbox(context.Background(), "run-1", "", false)
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
	hostSession(reg, "run-1")
	file := writeEndpoint(t, ws, "run-1", dead)

	got, err := reg.ProvisionSandbox(context.Background(), "run-1", "", false)
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

// A run hosted on another pod is refused: loopback says the caller is in this
// pod, not which run it is.
func TestProvisionSandbox_RejectsRunNotHostedHere(t *testing.T) {
	n := &reprovisionNotifier{endpoint: "10.0.0.9:50051"}
	reg, _ := newTestRegistry(t, n)
	hostSession(reg, "run-mine")

	_, err := reg.ProvisionSandbox(context.Background(), "run-theirs", "", false)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
	if n.calls != 0 {
		t.Fatalf("expected no provisioning for a foreign run, got %d calls", n.calls)
	}
}

// The session remembers the sandbox, so deleting the workspace copy does not
// make the run look like it never had one.
func TestSandbox_RemembersAcrossADeletedDescriptor(t *testing.T) {
	const fresh = "10.0.0.8:50051"
	n := &reprovisionNotifier{endpoint: fresh}
	reg, ws := newTestRegistry(t, n)
	hostSession(reg, "run-1")

	if _, ok, err := reg.Sandbox(context.Background(), "run-1"); ok || err != nil {
		t.Fatal("a run with no sandbox must report none")
	}

	if _, err := reg.ProvisionSandbox(context.Background(), "run-1", "", false); err != nil {
		t.Fatalf("ProvisionSandbox: %v", err)
	}

	// The agent deletes its own descriptor.
	if err := os.RemoveAll(filepath.Join(ws, "run-1", ".sandbox-exec")); err != nil {
		t.Fatalf("remove descriptor: %v", err)
	}

	got, ok, _ := reg.Sandbox(context.Background(), "run-1")
	if !ok {
		t.Fatal("expected the sandbox to be remembered after the descriptor was deleted")
	}
	if got.Endpoint != fresh || got.ExecToken == "" {
		t.Errorf("remembered sandbox = %+v", got)
	}
}

// A run this pod does not host has no sandbox to report.
func TestSandbox_UnknownRun(t *testing.T) {
	reg, _ := newTestRegistry(t, &reprovisionNotifier{endpoint: "10.0.0.8:50051"})
	if _, ok, _ := reg.Sandbox(context.Background(), "run-elsewhere"); ok {
		t.Error("expected no sandbox for a run this pod does not host")
	}
}

// A run ID that is not a single path segment fails before orbit-agents is asked for a sandbox.
func TestProvisionSandbox_BadRunIDNeverProvisions(t *testing.T) {
	n := &reprovisionNotifier{endpoint: "10.0.0.1:50051"}
	reg, _ := newTestRegistry(t, n)
	hostSession(reg, "../run-1")

	if _, err := reg.ProvisionSandbox(context.Background(), "../run-1", "", false); err == nil {
		t.Fatal("expected an error for a run ID with a path separator")
	}
	if n.calls != 0 {
		t.Errorf("orbit-agents was called %d times, want none", n.calls)
	}
}
