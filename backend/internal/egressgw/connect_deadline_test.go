package egressgw

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/egressgw/tlsca/testtlsca"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// TestConnect_IdleTunnelClosed locks the intercept path's idle deadline.
// Hijacking detaches the conn from the server, so ReadHeaderTimeout stops
// applying and the request context is never cancelled. Without a deadline of
// its own, a client that opens a tunnel and sends no ClientHello holds the
// goroutine and the socket indefinitely.
func TestConnect_IdleTunnelClosed(t *testing.T) {
	restore := interceptIdleTimeout
	interceptIdleTimeout = 200 * time.Millisecond
	t.Cleanup(func() { interceptIdleTimeout = restore })

	logger := logging.NewLoggerWithOutput(io.Discard)
	ca, _, err := testtlsca.NewCA("acme")
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	connect := NewConnectHandler(
		&fixedCABundle{tenant: "acme", ca: ca},
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		&memConnRepo{items: map[string]*credentials.Connection{}},
		logger,
	)
	gw := httptest.NewServer(authInjector(
		claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-1"), connect))
	defer gw.Close()

	conn, err := net.Dial("tcp", gw.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")

	// Read the 200, then send nothing. The handler is now blocked in the TLS
	// handshake waiting for a ClientHello that never comes.
	if _, err := conn.Read(make([]byte, 64)); err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}

	// The deadline must close the tunnel from the server side. Give the client
	// a far longer read window so a hang here is the handler's, not the test's.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("read succeeded; the idle tunnel was never closed")
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("client timed out waiting for close: the idle deadline did not fire")
	}
}
