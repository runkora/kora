package egressgw

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/egressgw/tlsca/testtlsca"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestRefuseLocalTarget(t *testing.T) {
	cases := []struct {
		address string
		refused bool
	}{
		{"127.0.0.1:6443", true},
		{"127.10.0.1:80", true},
		{"[::1]:443", true},
		{"[::ffff:127.0.0.1]:443", true},
		{"0.0.0.0:8080", true},
		{"[::]:8080", true},
		{"169.254.169.254:80", true},
		{"[fe80::1]:443", true},
		{"not-an-address", true},
		{"10.42.0.20:443", false},
		{"172.21.0.3:6443", false},
		{"140.82.112.3:443", false},
	}
	for _, c := range cases {
		t.Run(c.address, func(t *testing.T) {
			err := RefuseLocalTarget("tcp", c.address, nil)
			if got := errors.Is(err, ErrLocalTarget); got != c.refused {
				t.Errorf("RefuseLocalTarget(%q) refused = %v, want %v (err %v)", c.address, got, c.refused, err)
			}
		})
	}
}

// acceptCounter listens on loopback and counts accepted connections, so a test
// can prove a refused dial never reached the socket.
func acceptCounter(t *testing.T) (port string, accepted *int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var n int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&n, 1)
			_ = c.Close()
		}
	}()
	_, port, _ = net.SplitHostPort(ln.Addr().String())
	return port, &n
}

// An empty host and localhost both resolve to the gateway itself, so the dial
// guard refuses them after resolution, before any connection is made.
func TestRefuseLocalTarget_ResolvedHosts(t *testing.T) {
	port, accepted := acceptCounter(t)
	dialer := &net.Dialer{Timeout: time.Second, Control: RefuseLocalTarget}
	for _, host := range []string{"", "localhost", "127.0.0.1"} {
		conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(host, port))
		if err == nil {
			_ = conn.Close()
		}
		if !errors.Is(err, ErrLocalTarget) {
			t.Errorf("dial %q: err = %v, want ErrLocalTarget", host, err)
		}
	}
	if n := atomic.LoadInt32(accepted); n != 0 {
		t.Errorf("listener accepted %d connections, want 0", n)
	}
}

// The relay path refuses a CONNECT to a local target and never dials it. An
// empty host fails authority parsing (400); localhost and loopback fail the
// dial guard (403).
func TestConnect_RelayRefusesLocalTarget(t *testing.T) {
	port, accepted := acceptCounter(t)
	logger := logging.NewLoggerWithOutput(io.Discard)
	ca, _, err := testtlsca.NewCA("acme")
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	claims := claimsFor("acme", "spiffe://runkora.test/tenant/kora-tenant-acme/agent/coder", "run-1")
	connect := NewConnectHandler(&fixedCABundle{tenant: "acme", ca: ca},
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		&memConnRepo{items: map[string]*credentials.Connection{}}, logger)
	gw := httptest.NewServer(authInjector(claims, connect))
	defer gw.Close()

	for host, status := range map[string]string{"": " 400 ", "localhost": " 403 ", "127.0.0.1": " 403 "} {
		target := net.JoinHostPort(host, port)
		conn, err := net.Dial("tcp", gw.Listener.Addr().String())
		if err != nil {
			t.Fatalf("dial gateway: %v", err)
		}
		_, _ = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		_ = conn.Close()
		if !strings.Contains(string(buf[:n]), status) {
			t.Errorf("CONNECT %q: response %q, want%s", target, buf[:n], status)
		}
	}
	if n := atomic.LoadInt32(accepted); n != 0 {
		t.Errorf("listener accepted %d connections, want 0", n)
	}
}

// The intercept path's forward leg answers 403 when the guarded transport
// refuses a local target, and the upstream is never reached.
func TestConnect_InterceptRefusesLocalTarget(t *testing.T) {
	var hit int32
	upstream := recordingTLSUpstream(&hit)
	defer upstream.Close()

	logger := logging.NewLoggerWithOutput(io.Discard)
	ca, _, err := testtlsca.NewCA("acme")
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	broker := NewBroker(&stubEvaluator{allow: true}, &memConnRepo{items: map[string]*credentials.Connection{}},
		NewAdapterRegistry(), passthrough.New(), logger)
	forward := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext:     (&net.Dialer{Timeout: time.Second, Control: RefuseLocalTarget}).DialContext,
	}
	claims := claimsFor("acme", "spiffe://runkora.test/tenant/kora-tenant-acme/agent/coder", "run-1")
	inner := authInjector(claims, NewProxyHandler(broker, forward, testBrand, logger))
	connect := NewConnectHandler(&fixedCABundle{tenant: "acme", ca: ca}, inner, interceptAllConns{}, logger)
	gw := httptest.NewServer(authInjector(claims, connect))
	defer gw.Close()

	authority := strings.TrimPrefix(upstream.URL, "https://")
	host, _, _ := net.SplitHostPort(authority)
	status, err := rawProxyCONNECT(t, gw.Listener.Addr().String(), authority, host, authority, "/")
	if err != nil {
		t.Fatalf("CONNECT round-trip: %v", err)
	}
	if status != http.StatusForbidden {
		t.Errorf("inner status = %d, want 403", status)
	}
	if atomic.LoadInt32(&hit) != 0 {
		t.Error("local upstream was reached")
	}
}

func TestParseConnectAuthority(t *testing.T) {
	cases := []struct {
		target     string
		host, port string
		ok         bool
	}{
		{"k3d-server-0:6443", "k3d-server-0", "6443", true},
		{"api.github.com:443", "api.github.com", "443", true},
		{"172.21.0.3:6443", "172.21.0.3", "6443", true},
		{"[2001:db8::1]:443", "", "", false},
		{"github.com:x@evil.com:6443", "", "", false},
		{"github.com@evil.com:443", "", "", false},
		{"github.com", "", "", false},
		{":443", "", "", false},
		{"github.com:0", "", "", false},
		{"github.com:65536", "", "", false},
		{"github.com:0443", "", "", false},
		{"-bad.example.com:443", "", "", false},
		{"bad_name.example.com:443", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(
				"CONNECT " + c.target + " HTTP/1.1\r\nHost: " + c.target + "\r\n\r\n")))
			if err != nil {
				if c.ok {
					t.Fatalf("read request: %v", err)
				}
				return
			}
			host, port, ok := parseConnectAuthority(req)
			if ok != c.ok || host != c.host || port != c.port {
				t.Errorf("parseConnectAuthority(%q) = %q, %q, %v; want %q, %q, %v", c.target, host, port, ok, c.host, c.port, c.ok)
			}
		})
	}
}
