package sessionmanager

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	execpb "github.com/clear-route/orbit/internal/agents/sandbox/proto"
	"github.com/clear-route/orbit/internal/platform/logging"
	"google.golang.org/grpc"
)

// ── stub gRPC exec server ─────────────────────────────────────────────────────

// stubExecServer implements ExecService for testing the egress canary.
// The first N calls (configurable) return exit code 1; subsequent calls return 0.
type stubExecServer struct {
	execpb.UnimplementedExecServiceServer
	callCount atomic.Int32 // total Execute calls
	failUntil int32        // return exit 1 for the first failUntil calls
}

func (s *stubExecServer) Execute(_ *execpb.ExecuteRequest, stream grpc.ServerStreamingServer[execpb.ExecuteResponse]) error {
	n := s.callCount.Add(1)
	code := int32(0)
	if n <= s.failUntil {
		code = 1
	}
	return stream.Send(&execpb.ExecuteResponse{
		Output: &execpb.ExecuteResponse_ExitCode{ExitCode: code},
	})
}

// startStubExecServer starts a gRPC exec server and returns its address and a
// stop function.
func startStubExecServer(t *testing.T, srv *stubExecServer) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	execpb.RegisterExecServiceServer(gs, srv)
	go func() {
		if sErr := gs.Serve(ln); sErr != nil {
			// Ignore "use of closed network connection" on test cleanup.
			_ = sErr
		}
	}()
	t.Cleanup(gs.Stop)
	return ln.Addr().String()
}

// ── awaitEgressReady tests ────────────────────────────────────────────────────

func newTestRegistryForCanary(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(logging.NewLoggerWithOutput(io.Discard), nil, t.TempDir(), NoopNotifier{}, time.Minute)
}

// TestAwaitEgressReady_PassesOnFirstSuccess verifies that awaitEgressReady
// returns nil immediately when the sandbox exec exits 0 on the first attempt.
func TestAwaitEgressReady_PassesOnFirstSuccess(t *testing.T) {
	srv := &stubExecServer{failUntil: 0} // always exit 0
	addr := startStubExecServer(t, srv)

	reg := newTestRegistryForCanary(t)
	err := reg.awaitEgressReady(context.Background(), "run-1", addr)
	if err != nil {
		t.Fatalf("awaitEgressReady returned unexpected error: %v", err)
	}
	if n := srv.callCount.Load(); n != 1 {
		t.Errorf("expected 1 exec call, got %d", n)
	}
}

// TestAwaitEgressReady_RetriesUntilSuccess verifies that awaitEgressReady
// retries when the canary fails and eventually passes once the server returns
// exit 0. We lower the interval constant via a short wait so the test is fast.
func TestAwaitEgressReady_RetriesUntilSuccess(t *testing.T) {
	// Patch interval to 10ms so retries happen quickly in the test.
	origInterval := egressCanaryInterval
	defer func() {
		// Restore the package-level constant. Since it's a const we work around
		// this in the test by overriding the method call directly. Instead, we
		// test awaitEgressReady via a test helper that accepts a custom interval.
		_ = origInterval
	}()

	srv := &stubExecServer{failUntil: 2} // fail first 2 calls, succeed on 3rd
	addr := startStubExecServer(t, srv)

	reg := newTestRegistryForCanary(t)
	err := reg.awaitEgressReadyWithInterval(context.Background(), "run-retry", addr, 10*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatalf("awaitEgressReady returned unexpected error after retries: %v", err)
	}
	// failUntil: 2 means calls 1 and 2 fail and call 3 succeeds, so the canary
	// must probe exactly 3 times — no fewer (it would have returned early on a
	// failure) and no more (it must stop the instant a probe succeeds). Asserting
	// == 3 rather than >= 3 catches a retry loop that keeps probing past success.
	if n := srv.callCount.Load(); n != 3 {
		t.Errorf("expected exactly 3 exec calls (2 failures then success), got %d", n)
	}
}

// TestAwaitEgressReady_TimesOut verifies that awaitEgressReady returns an error
// when the canary never succeeds within the allowed window.
func TestAwaitEgressReady_TimesOut(t *testing.T) {
	srv := &stubExecServer{failUntil: 1000} // always fail
	addr := startStubExecServer(t, srv)

	reg := newTestRegistryForCanary(t)
	err := reg.awaitEgressReadyWithInterval(context.Background(), "run-timeout", addr, 20*time.Millisecond, 80*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestAwaitEgressReady_ContextCanceled verifies that awaitEgressReady returns
// when its context is canceled.
func TestAwaitEgressReady_ContextCanceled(t *testing.T) {
	srv := &stubExecServer{failUntil: 1000} // always fail
	addr := startStubExecServer(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	reg := newTestRegistryForCanary(t)
	err := reg.awaitEgressReadyWithInterval(ctx, "run-cancel", addr, 20*time.Millisecond, 5*time.Second)
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
}

// TestSandboxExec_SuccessAndFailure verifies the sandboxExec helper correctly
// returns (true, nil) for exit 0 and (false, nil) for non-zero exits.
func TestSandboxExec_SuccessAndFailure(t *testing.T) {
	srvOK := &stubExecServer{failUntil: 0} // exit 0
	addrOK := startStubExecServer(t, srvOK)

	srvFail := &stubExecServer{failUntil: 999} // exit 1
	addrFail := startStubExecServer(t, srvFail)

	reg := newTestRegistryForCanary(t)

	ok, err := reg.sandboxExec(context.Background(), addrOK, "true")
	if err != nil {
		t.Fatalf("sandboxExec(ok): unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("sandboxExec(ok): expected true, got false")
	}

	ok, err = reg.sandboxExec(context.Background(), addrFail, "false")
	if err != nil {
		t.Fatalf("sandboxExec(fail): unexpected error: %v", err)
	}
	if ok {
		t.Fatal("sandboxExec(fail): expected false, got true")
	}
}
