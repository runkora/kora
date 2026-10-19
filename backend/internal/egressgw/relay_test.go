package egressgw

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/credentials"
)

func TestShouldIntercept(t *testing.T) {
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "c1", TenantID: "acme", Name: "db",
		Adapter: credentials.AdapterPassthrough, BaseHost: "db.internal:5432",
	})
	h := &ConnectHandler{conns: repo}

	cases := []struct {
		name              string
		tenant, authority string
		port              string
		want              bool
	}{
		{"https on 443 always intercepts", "acme", "github.com:443", "443", true},
		{"registered non-standard port intercepts", "acme", "db.internal:5432", "5432", true},
		{"unregistered non-standard port relays", "acme", "8.8.8.8:53", "53", false},
		{"registered but wrong tenant relays", "other", "db.internal:5432", "5432", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := h.shouldIntercept(context.Background(), c.tenant, c.authority, c.port); got != c.want {
				t.Errorf("shouldIntercept(%q, %q, %q) = %v, want %v", c.tenant, c.authority, c.port, got, c.want)
			}
		})
	}

	// With no connection store, only :443 is intercepted.
	hNil := &ConnectHandler{}
	if !hNil.shouldIntercept(context.Background(), "acme", "x:443", "443") {
		t.Error("nil conns: :443 should still intercept")
	}
	if hNil.shouldIntercept(context.Background(), "acme", "x:5432", "5432") {
		t.Error("nil conns: non-443 should relay")
	}
}

// tcpPair returns the two ends of a connected loopback TCP socket so tests can
// exercise CloseWrite-based half-close (net.Pipe cannot).
func tcpPair(t *testing.T) (dialEnd, acceptEnd net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	type accepted struct {
		c   net.Conn
		err error
	}
	ch := make(chan accepted, 1)
	go func() {
		c, err := ln.Accept()
		ch <- accepted{c, err}
	}()

	d, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	a := <-ch
	if a.err != nil {
		t.Fatalf("accept: %v", a.err)
	}
	return d, a.c
}

func TestRelayTCP_Bidirectional(t *testing.T) {
	clientLocal, clientRelay := tcpPair(t)
	upstreamRelay, upstreamApp := tcpPair(t)
	defer func() { _ = clientLocal.Close() }()
	defer func() { _ = clientRelay.Close() }()
	defer func() { _ = upstreamRelay.Close() }()

	// Upstream echoes, then closes so the relay's upstream→client side sees EOF.
	go func() {
		_, _ = io.Copy(upstreamApp, upstreamApp)
		_ = upstreamApp.Close()
	}()

	type counts struct{ sent, recv int64 }
	done := make(chan counts, 1)
	go func() {
		s, r := relayTCP(clientRelay, upstreamRelay, 5*time.Second)
		done <- counts{s, r}
	}()

	msg := []byte("hello-orbit")
	if _, err := clientLocal.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(clientLocal, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("echo mismatch: got %q want %q", got, msg)
	}

	// Closing the client tears the whole relay down via half-close propagation.
	_ = clientLocal.Close()
	select {
	case c := <-done:
		if c.sent < int64(len(msg)) {
			t.Errorf("sent=%d want >= %d", c.sent, len(msg))
		}
		if c.recv < int64(len(msg)) {
			t.Errorf("recv=%d want >= %d", c.recv, len(msg))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not close after client closed")
	}
}

func TestRelayTCP_IdleTimeout(t *testing.T) {
	dialA, clientRelay := tcpPair(t)
	dialB, upstreamRelay := tcpPair(t)
	defer func() { _ = dialA.Close() }()
	defer func() { _ = dialB.Close() }()
	defer func() { _ = clientRelay.Close() }()
	defer func() { _ = upstreamRelay.Close() }()

	done := make(chan struct{})
	go func() {
		relayTCP(clientRelay, upstreamRelay, 150*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("idle relay did not time out")
	}
}
