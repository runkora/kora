package egressgw

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/egressgw/tlsca/testtlsca"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// TestConnectResponseWriter_ChunkFraming asserts the on-wire format of a few
// Write calls is a well-formed chunked-transfer body, with the terminator
// emitted by finish.
func TestConnectResponseWriter_ChunkFraming(t *testing.T) {
	var buf bytes.Buffer
	w := newConnectResponseWriter(&closableBuf{Buffer: &buf})

	w.Header().Set("Content-Type", "text/plain")
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte(" world")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}

	out := buf.String()

	// Status line and headers.
	if !strings.HasPrefix(out, "HTTP/1.1 200 OK\r\n") {
		t.Errorf("status line missing or wrong: %q", firstLine(out))
	}
	if !strings.Contains(out, "Transfer-Encoding: chunked\r\n") {
		t.Errorf("missing Transfer-Encoding: chunked header; raw:\n%s", out)
	}
	if strings.Contains(out, "Content-Length:") {
		t.Errorf("Content-Length must not appear on a chunked response; raw:\n%s", out)
	}
	if strings.Contains(out, "\r\nContent-Type: text/plain\r\n") == false {
		t.Errorf("custom header dropped; raw:\n%s", out)
	}

	// Chunk frames: "5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
	body := bodyAfterHeaders(t, out)
	if body != "5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n" {
		t.Errorf("chunk framing wrong:\n  got %q\n  want %q", body, "5\\r\\nhello\\r\\n6\\r\\n world\\r\\n0\\r\\n\\r\\n")
	}
}

// TestConnectResponseWriter_NoBodyStillEmitsTerminator covers the case where
// the inner handler only sets headers (e.g. 204 No Content). The wire output
// must still be a valid chunked response: status + headers + 0-frame.
func TestConnectResponseWriter_NoBodyStillEmitsTerminator(t *testing.T) {
	var buf bytes.Buffer
	w := newConnectResponseWriter(&closableBuf{Buffer: &buf})
	w.WriteHeader(http.StatusNoContent)
	if err := w.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}

	out := buf.String()
	if !strings.HasPrefix(out, "HTTP/1.1 204 No Content\r\n") {
		t.Errorf("status line: %q", firstLine(out))
	}
	if !strings.HasSuffix(out, "0\r\n\r\n") {
		t.Errorf("missing terminator frame; raw:\n%s", out)
	}
}

// TestConnect_StreamingResponse_ChunksArriveBeforeUpstreamCloses is the test
// that justifies this whole rewrite: an upstream that flushes mid-response
// must deliver bytes to the client before closing. Under the previous buffered
// implementation, the client would have received nothing until upstream EOF.
func TestConnect_StreamingResponse_ChunksArriveBeforeUpstreamCloses(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)

	// Upstream that flushes 3 chunks 50ms apart and then waits for an explicit
	// release before returning. That way, if the gateway buffered the body,
	// the client would still be blocked in resp.Body.Read after we've already
	// sent and flushed all three chunks.
	release := make(chan struct{})
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i, msg := range []string{"chunk-1\n", "chunk-2\n", "chunk-3\n"} {
			_, _ = w.Write([]byte(msg))
			flusher.Flush()
			if i < 2 {
				time.Sleep(20 * time.Millisecond)
			}
		}
		<-release
	}))
	defer upstream.Close()
	defer close(release)

	upstreamHost := strings.TrimPrefix(upstream.URL, "https://")
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "conn-1", TenantID: "acme", Name: "github",
		Adapter: credentials.AdapterGitHubApp, BaseHost: upstreamHost,
		SecretRef: "ignored", Config: []byte(`{}`),
	})

	// Adapter that returns a no-op credential. This test is about a streaming
	// response flowing through the writer, not about minting.
	registry := NewAdapterRegistry()
	registry.Register(&streamTestAdapter{})

	broker := NewBroker(&stubEvaluator{allow: true}, repo, registry, passthrough.New(), logger, nil)
	forwardTransport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	proxyHandler := NewProxyHandler(broker, forwardTransport, testBrand, logger)
	innerWithAuth := authInjector(claimsFor("acme", "spiffe://runkora.test/tenant/kora-tenant-acme/agent/coder", "run-9"), proxyHandler)

	ca, rootPEM, err := testtlsca.NewCA("acme")
	if err != nil {
		t.Fatalf("testtlsca.NewCA: %v", err)
	}
	bundle := &fixedCABundle{tenant: "acme", ca: ca}
	connect := NewConnectHandler(bundle, innerWithAuth, repo, logger)
	gateway := httptest.NewServer(authInjector(claimsFor("acme", "spiffe://runkora.test/tenant/kora-tenant-acme/agent/coder", "run-9"), connect))
	defer gateway.Close()

	clientPool := x509.NewCertPool()
	clientPool.AppendCertsFromPEM(rootPEM)
	gatewayURL, _ := url.Parse(gateway.URL)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(gatewayURL),
			TLSClientConfig: &tls.Config{RootCAs: clientPool},
			DialContext:     (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			// DisableCompression keeps the chunked framing visible on the wire
			// (Go's transport otherwise transparently un-gzips, but we don't
			// gzip here; setting it makes the test's intent explicit).
			DisableCompression: true,
		},
	}

	reqURL := "https://" + upstreamHost + "/stream"
	req, _ := http.NewRequest(http.MethodGet, reqURL, nil)
	req.Host = upstreamHost
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	// The upstream is still blocked on <-release. Read the three chunks; if
	// they don't arrive within a reasonable budget, the writer is buffering.
	br := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	for i, want := range []string{"chunk-1\n", "chunk-2\n", "chunk-3\n"} {
		if time.Now().After(deadline) {
			t.Fatalf("read deadline exceeded waiting for chunk %d, writer is likely buffering", i+1)
		}
		got, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read chunk %d: %v", i+1, err)
		}
		if got != want {
			t.Errorf("chunk %d = %q, want %q", i+1, got, want)
		}
	}
	// All three streamed in before upstream EOF. The writer is flushing per chunk.
}

// TestConnectResponseWriter_FlushBeforeWriteEmitsHeaders covers the SSE case
// where the handler sets headers, calls Flush so the client knows the
// connection is alive, then writes events later.
func TestConnectResponseWriter_FlushBeforeWriteEmitsHeaders(t *testing.T) {
	var buf bytes.Buffer
	w := newConnectResponseWriter(&closableBuf{Buffer: &buf})
	w.Header().Set("X-Marker", "early")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	out := buf.String()
	if !strings.Contains(out, "X-Marker: early\r\n") {
		t.Errorf("Flush before any Write should emit headers; raw:\n%s", out)
	}
	if !strings.Contains(out, "Transfer-Encoding: chunked\r\n") {
		t.Errorf("missing Transfer-Encoding: chunked; raw:\n%s", out)
	}
	if strings.Contains(out, "0\r\n\r\n") {
		t.Errorf("Flush must not emit terminator (only finish does); raw:\n%s", out)
	}
}

// streamTestAdapter is a no-op CredentialAdapter for the streaming integration
// test: it accepts any request and injects no header, so the upstream sees a
// request with whatever the proxy already had.
type streamTestAdapter struct{}

func (*streamTestAdapter) Type() credentials.AdapterType { return credentials.AdapterGitHubApp }
func (*streamTestAdapter) Actions() []credentials.ActionDeclaration {
	return []credentials.ActionDeclaration{{Action: "repo:read"}}
}
func (*streamTestAdapter) Open(credentials.Connection) (credentials.Connector, error) {
	return streamTestConnector{}, nil
}

type streamTestConnector struct{}

func (streamTestConnector) Classify(*http.Request) (credentials.Classification, error) {
	return credentials.Classification{Action: "repo:read"}, nil
}
func (streamTestConnector) Mint(context.Context, credentials.CallerIdentity) (credentials.Credential, error) {
	return credentials.Credential{}, nil
}
func (streamTestConnector) QualifierCeiling() []string { return nil }

// closableBuf wraps a *bytes.Buffer so it satisfies net.Conn-ish writer
// requirements (specifically, Close()) used by newConnectResponseWriter.
type closableBuf struct {
	*bytes.Buffer
	closed atomic.Bool
}

func (c *closableBuf) Close() error                       { c.closed.Store(true); return nil }
func (c *closableBuf) Read(p []byte) (int, error)         { return 0, errors.New("not a real conn") }
func (c *closableBuf) LocalAddr() net.Addr                { return nil }
func (c *closableBuf) RemoteAddr() net.Addr               { return nil }
func (c *closableBuf) SetDeadline(time.Time) error        { return nil }
func (c *closableBuf) SetReadDeadline(time.Time) error    { return nil }
func (c *closableBuf) SetWriteDeadline(_ time.Time) error { return nil }

// firstLine returns the first line of out (sans CR/LF).
func firstLine(out string) string {
	first, _, _ := strings.Cut(out, "\r")
	return first
}

// bodyAfterHeaders splits a serialized HTTP response at the blank line and
// returns the chunked body bytes.
func bodyAfterHeaders(t *testing.T, out string) string {
	t.Helper()
	_, body, ok := strings.Cut(out, "\r\n\r\n")
	if !ok {
		t.Fatalf("response has no header terminator:\n%s", out)
	}
	return body
}
