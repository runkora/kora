package sessionmanager

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/agents/sandbox/execauth"
	execpb "github.com/runkora/kora/backend/internal/agents/sandbox/proto"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"google.golang.org/grpc"
)

// ── stub gRPC exec server ─────────────────────────────────────────────────────

// stubExecServer implements ExecService for testing the egress canary.
// The first N calls (configurable) return exit code 1; subsequent calls return 0.
type stubExecServer struct {
	execpb.UnimplementedExecServiceServer
	callCount atomic.Int32 // total Execute calls
	failUntil int32        // return exit 1 for the first failUntil calls
	lastToken atomic.Value // string; the exec token presented on the most recent call
	lastCmd   atomic.Value // string; the command run on the most recent call
}

func (s *stubExecServer) Execute(req *execpb.ExecuteRequest, stream grpc.ServerStreamingServer[execpb.ExecuteResponse]) error {
	token, _ := execauth.TokenFrom(stream.Context())
	s.lastToken.Store(token)
	s.lastCmd.Store(req.GetCommand())
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

// testExecToken is the token the canary is expected to present.
const testExecToken = "canary-exec-token"

// testCanaryHost stands in for a host the agent is allowed to reach.
const testCanaryHost = "pypi.org"

func testSandbox(addr string) Sandbox {
	return Sandbox{Endpoint: addr, ExecToken: testExecToken, CanaryHost: testCanaryHost}
}

// ── awaitEgressReady tests ────────────────────────────────────────────────────

func newTestRegistryForCanary(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), NoopNotifier{}, time.Minute, testBrand)
}

// TestAwaitEgressReady_PassesOnFirstSuccess verifies that awaitEgressReady
// returns nil immediately when the sandbox exec exits 0 on the first attempt.
func TestAwaitEgressReady_PassesOnFirstSuccess(t *testing.T) {
	srv := &stubExecServer{failUntil: 0} // always exit 0
	addr := startStubExecServer(t, srv)

	reg := newTestRegistryForCanary(t)
	result, err := reg.awaitEgressReady(context.Background(), "run-1", testSandbox(addr))
	if err != nil || result != egressCanaryPassed {
		t.Fatalf("awaitEgressReady = %q, %v; want passed", result, err)
	}
	if got, _ := srv.lastToken.Load().(string); got != testExecToken {
		t.Errorf("canary presented exec token %q, want %q", got, testExecToken)
	}
	if n := srv.callCount.Load(); n != 1 {
		t.Errorf("expected 1 exec call, got %d", n)
	}
	if got, _ := srv.lastCmd.Load().(string); got != egressCanaryCommand(testCanaryHost) {
		t.Errorf("canary ran %q, want a probe of the agent's own host %q", got, testCanaryHost)
	}
}

// An agent with no host to probe, or a host that is not a plain hostname,
// skips the canary instead of waiting out a probe that cannot pass.
func TestAwaitEgressReady_SkipsWithoutProbeableHost(t *testing.T) {
	for _, host := range []string{"", "*.example.com", "pypi.org; id", "pypi.org/simple"} {
		t.Run(host, func(t *testing.T) {
			srv := &stubExecServer{failUntil: 1000} // would never pass
			addr := startStubExecServer(t, srv)
			sandbox := testSandbox(addr)
			sandbox.CanaryHost = host

			reg := newTestRegistryForCanary(t)
			result, err := reg.awaitEgressReadyWithInterval(context.Background(), "run-skip", sandbox, 10*time.Millisecond, 5*time.Second)
			if err != nil || result != egressCanarySkipped {
				t.Fatalf("awaitEgressReady = %q, %v; want skipped", result, err)
			}
			if n := srv.callCount.Load(); n != 0 {
				t.Errorf("expected no exec calls, got %d", n)
			}
		})
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
	result, err := reg.awaitEgressReadyWithInterval(context.Background(), "run-retry", testSandbox(addr), 10*time.Millisecond, 5*time.Second)
	if err != nil || result != egressCanaryPassed {
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
	result, err := reg.awaitEgressReadyWithInterval(context.Background(), "run-timeout", testSandbox(addr), 20*time.Millisecond, 80*time.Millisecond)
	if err == nil || result != egressCanaryTimedOut {
		t.Fatalf("awaitEgressReady = %q, %v; want timed_out with an error", result, err)
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
	result, err := reg.awaitEgressReadyWithInterval(ctx, "run-cancel", testSandbox(addr), 20*time.Millisecond, 5*time.Second)
	if err == nil || result != egressCanaryTimedOut {
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

	ok, err := reg.sandboxExec(context.Background(), testSandbox(addrOK), "true")
	if err != nil {
		t.Fatalf("sandboxExec(ok): unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("sandboxExec(ok): expected true, got false")
	}

	ok, err = reg.sandboxExec(context.Background(), testSandbox(addrFail), "false")
	if err != nil {
		t.Fatalf("sandboxExec(fail): unexpected error: %v", err)
	}
	if ok {
		t.Fatal("sandboxExec(fail): expected false, got true")
	}
}
