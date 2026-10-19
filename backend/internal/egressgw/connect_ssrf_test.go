package egressgw

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/egressgw/tlsca/testtlsca"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// rawProxyCONNECT performs a MANUAL CONNECT + inner TLS handshake + single inner
// request through the gateway, so the test can send an inner Host that differs
// from the CONNECT target: something a well-behaved http.Client never does, and
// the exact shape of the inner-Host SSRF. Cert verification is skipped: the SUT
// here is the gateway's inner-Host routing, not its leaf cert. Returns the inner
// response status, or an error if the tunnel / handshake / inner read failed.
func rawProxyCONNECT(t *testing.T, gatewayAddr, connectTarget, sni, innerHost, innerPath string) (int, error) {
	t.Helper()
	raw, err := net.Dial("tcp", gatewayAddr)
	if err != nil {
		return 0, fmt.Errorf("dial gateway: %w", err)
	}
	defer func() { _ = raw.Close() }()

	if _, err := fmt.Fprintf(raw, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", connectTarget, connectTarget); err != nil {
		return 0, fmt.Errorf("write CONNECT: %w", err)
	}
	connResp, err := http.ReadResponse(bufio.NewReader(raw), &http.Request{Method: http.MethodConnect})
	if err != nil {
		return 0, fmt.Errorf("read CONNECT response: %w", err)
	}
	_ = connResp.Body.Close()
	if connResp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("CONNECT status %d", connResp.StatusCode)
	}

	tlsConn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, ServerName: sni})
	if err := tlsConn.HandshakeContext(context.Background()); err != nil {
		return 0, fmt.Errorf("inner TLS handshake: %w", err)
	}
	if _, err := fmt.Fprintf(tlsConn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", innerPath, innerHost); err != nil {
		return 0, fmt.Errorf("write inner request: %w", err)
	}
	innerResp, err := http.ReadResponse(bufio.NewReader(tlsConn), &http.Request{Method: http.MethodGet})
	if err != nil {
		return 0, fmt.Errorf("read inner response: %w", err)
	}
	defer func() { _ = innerResp.Body.Close() }()
	_, _ = io.Copy(io.Discard, innerResp.Body)
	return innerResp.StatusCode, nil
}

func recordingTLSUpstream(hit *int32) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.StoreInt32(hit, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
}

// interceptAllConns forces the CONNECT handler down the TLS-intercept path for
// ANY destination. Test upstreams bind ephemeral ports (never :443), so without
// this shouldIntercept() would relay them raw, bypassing the inner-request
// parse where the Host pinning and the bounded read live. The broker is given a
// SEPARATE empty repo so Authorize still takes the passthrough branch (forward
// to the request Host, inject nothing), exactly the path pinning protects.
type interceptAllConns struct{}

func (interceptAllConns) ListByHost(context.Context, string, string) ([]*credentials.Connection, error) {
	return []*credentials.Connection{{}}, nil
}

// buildPassthroughGateway wires a gateway whose broker has NO registered
// connections (every host is passthrough) but whose CONNECT handler intercepts
// every destination, returning the gateway.
func buildPassthroughGateway(t *testing.T) *httptest.Server {
	t.Helper()
	logger := logging.NewLoggerWithOutput(io.Discard)
	ca, _, err := testtlsca.NewCA("acme")
	if err != nil {
		t.Fatalf("testtlsca.NewCA: %v", err)
	}
	repo := &memConnRepo{items: map[string]*credentials.Connection{}} // empty → broker passthrough
	broker := NewBroker(&stubEvaluator{allow: true}, repo, NewAdapterRegistry(), passthrough.New(), logger)
	forward := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	claims := claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-1")
	inner := authInjector(claims, NewProxyHandler(broker, forward, testBrand, logger))
	connect := NewConnectHandler(&fixedCABundle{tenant: "acme", ca: ca}, inner, interceptAllConns{}, logger)
	return httptest.NewServer(authInjector(claims, connect))
}

// TestConnect_InnerHostPinnedToConnectTarget is the regression test for the
// inner-Host SSRF: an agent CONNECTs an allowed host but sends an inner Host
// pointing at a different upstream. The gateway must follow the verified CONNECT
// target, never the attacker-controlled inner Host.
func TestConnect_InnerHostPinnedToConnectTarget(t *testing.T) {
	var targetHit, forbiddenHit int32
	target := recordingTLSUpstream(&targetHit)
	defer target.Close()
	forbidden := recordingTLSUpstream(&forbiddenHit)
	defer forbidden.Close()

	gateway := buildPassthroughGateway(t)
	defer gateway.Close()

	targetAuthority := strings.TrimPrefix(target.URL, "https://")       // 127.0.0.1:PORT_T
	forbiddenAuthority := strings.TrimPrefix(forbidden.URL, "https://") // 127.0.0.1:PORT_F
	targetHost, _, _ := net.SplitHostPort(targetAuthority)
	gatewayAddr := strings.TrimPrefix(gateway.URL, "http://")

	status, err := rawProxyCONNECT(t, gatewayAddr, targetAuthority, targetHost, forbiddenAuthority, "/")
	if err != nil {
		t.Fatalf("CONNECT round-trip: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("inner status = %d, want 200", status)
	}
	if atomic.LoadInt32(&targetHit) != 1 {
		t.Error("CONNECT target was NOT reached: request did not follow the verified host")
	}
	if atomic.LoadInt32(&forbiddenHit) != 0 {
		t.Error("forbidden host WAS reached: inner-Host SSRF is not blocked")
	}
}

// TestConnect_InnerRequestSizeBounded verifies the inner read is bounded: with a
// tiny cap the gateway cannot parse the request and closes the tunnel without
// forwarding (no OOM, no upstream call).
func TestConnect_InnerRequestSizeBounded(t *testing.T) {
	var targetHit int32
	target := recordingTLSUpstream(&targetHit)
	defer target.Close()
	gateway := buildPassthroughGateway(t)
	defer gateway.Close()

	orig := maxInnerRequestBytes
	maxInnerRequestBytes = 8 // smaller than any real request line
	defer func() { maxInnerRequestBytes = orig }()

	targetAuthority := strings.TrimPrefix(target.URL, "https://")
	targetHost, _, _ := net.SplitHostPort(targetAuthority)
	gatewayAddr := strings.TrimPrefix(gateway.URL, "http://")

	_, err := rawProxyCONNECT(t, gatewayAddr, targetAuthority, targetHost, targetAuthority, "/a/long/enough/path")
	if err == nil {
		t.Error("expected the oversized inner request to fail (bounded read), got a response")
	}
	if atomic.LoadInt32(&targetHit) != 0 {
		t.Error("upstream was reached despite the inner read exceeding the cap")
	}
}
