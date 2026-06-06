package egressgw

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// IssuingCA is the per-tenant TLS authority the CONNECT handler uses to mint a
// leaf certificate matching the SNI the agent's tool sent. The package keeps
// the dependency narrow to a single-method interface so tlsca isn't dragged
// into every consumer.
type IssuingCA interface {
	GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// ConnectHandler is the gateway's HTTP handler for CONNECT-method requests
// from sandbox-resident tools configured with HTTPS_PROXY=<gateway>.
//
// Lifecycle of one request:
//
//  1. Tool dials the gateway and sends `CONNECT github.com:443 HTTP/1.1` with
//     the platform Bearer token in Proxy-Authorization (or Authorization, see
//     below). AuthMiddleware on the gateway has already verified the claims and
//     stored them in r.Context().
//  2. ConnectHandler hijacks the TCP socket and writes `200 Connection
//     Established` so the tool starts its TLS handshake.
//  3. tls.Server runs a handshake on the same socket. GetCertificate consults
//     the IssuingCA and returns a freshly-minted leaf for the SNI the tool
//     sent. The tool, trusting our CA via the sandbox image's trust store,
//     accepts the leaf and completes the handshake.
//  4. ConnectHandler reads one *http.Request from the inner TLS conn,
//     re-attaches the verified claims, and serves it via the inner handler
//     (typically ProxyHandler). The inner handler treats the call as a normal
//     HTTPS request — broker logic stays unchanged from Phase 2a.
//  5. After the inner handler returns, ConnectHandler closes the TLS conn and
//     the underlying TCP connection.
//
// The handler is deliberately one-shot per CONNECT: HTTP/1.1 connection reuse
// over the tunneled TLS is not supported in Phase 2b. The tool's HTTP client
// will dial a fresh CONNECT for the next request, which is the standard
// behavior for HTTPS_PROXY clients anyway.
type ConnectHandler struct {
	ca     IssuingCA
	inner  http.Handler
	logger logging.Logger
}

// NewConnectHandler constructs a ConnectHandler. ca mints leaves for the SNI
// the tool sends; inner is invoked with the parsed *http.Request after TLS
// termination — pass the gateway's ProxyHandler so the established Phase 2a
// pipeline runs unchanged.
func NewConnectHandler(ca IssuingCA, inner http.Handler, logger logging.Logger) *ConnectHandler {
	return &ConnectHandler{ca: ca, inner: inner, logger: logger}
}

// ServeHTTP handles one CONNECT request. Non-CONNECT methods are rejected; the
// gateway's outer mux routes other verbs to the admin API.
func (h *ConnectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodConnect {
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.ValidationError(platformapi.ErrCodeInvalidField,
				"only CONNECT is supported on this endpoint", nil), nil)
		return
	}

	// AuthMiddleware should have stored verified claims in context; fail closed
	// if absent. This catches mis-wiring where the CONNECT listener was mounted
	// without auth in front.
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "unauthenticated", nil),
			ErrUnauthenticated)
		return
	}

	// Hijack the TCP conn so we can run TLS on it directly.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError,
				"server does not support connection hijacking", nil), nil)
		return
	}
	clientConn, bufrw, hijackErr := hijacker.Hijack()
	if hijackErr != nil {
		h.logger.Error(ctx, "hijack failed", logging.Field{Key: "error", Value: hijackErr.Error()})
		return
	}
	defer func() { _ = clientConn.Close() }()

	// Tell the tool the tunnel is established. Standard HTTP/1.1 form.
	if _, err := bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		h.logger.Warn(ctx, "write CONNECT 200 failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	if err := bufrw.Flush(); err != nil {
		h.logger.Warn(ctx, "flush CONNECT 200 failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}

	host, _, splitErr := net.SplitHostPort(r.Host)
	if splitErr != nil {
		// Tools normally send "host:port"; tolerate a bare host.
		host = r.Host
	}

	// Mint the leaf using the CONNECT host as a fallback when the client did
	// not send SNI. RFC 6066 requires SNI only for hostnames; clients dialing
	// IP literals (or some legacy clients) omit SNI, but we always know the
	// dial target from the CONNECT request-target.
	getCert := func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if hello.ServerName == "" {
			hello = &tls.ClientHelloInfo{
				ServerName:        host,
				SupportedVersions: hello.SupportedVersions,
				CipherSuites:      hello.CipherSuites,
			}
		}
		return h.ca.GetCertificate(hello)
	}

	tlsConn := tls.Server(clientConn, &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: getCert,
	})
	defer func() { _ = tlsConn.Close() }()

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		h.logger.Warn(ctx, "tls handshake with client failed",
			logging.Field{Key: "host", Value: host},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	// Read the inner HTTP request the tool wants to send to the upstream.
	innerReq, err := http.ReadRequest(bufio.NewReader(tlsConn))
	if err != nil {
		h.logger.Warn(ctx, "read inner request failed",
			logging.Field{Key: "host", Value: host},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	// Re-attach the verified claims and ensure ProxyHandler's host-based
	// connection lookup sees the SNI host (the inner request's Host header may
	// be a bare host without port). Ensure URL is absolute-friendly: for an
	// inner request the Host header is the destination, the Path is the URI.
	innerReq.URL.Scheme = "https"
	innerReq.URL.Host = innerReq.Host
	innerReq = innerReq.WithContext(token.WithClaims(ctx, claims))

	// Serve the inner request over the tunneled TLS conn. The bufio.Writer is
	// constructed fresh because http.ReadRequest may have read past the
	// headers into the body; the response writer goes back through tlsConn.
	rw := newConnectResponseWriter(tlsConn)
	h.inner.ServeHTTP(rw, innerReq)
	if err := rw.finish(); err != nil {
		h.logger.Warn(ctx, "write inner response failed",
			logging.Field{Key: "host", Value: host},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// connectResponseWriter is the http.ResponseWriter implementation we use to
// write the inner request's response back to the tool over the tunneled TLS
// connection. It buffers headers until the first Write or finish(), so the
// inner handler can call WriteHeader followed by streaming Write calls and we
// emit a well-formed HTTP/1.1 response.
//
// Streaming bodies (chunked or content-length) and trailers are not supported
// in Phase 2b; this is sufficient for git smart-HTTP, REST, and the proxied
// upstream traffic the broker handles. A future revision can promote this to
// chunked transfer when the broker grows server-side streaming.
type connectResponseWriter struct {
	conn        net.Conn
	headers     http.Header
	wroteHeader bool
	statusCode  int
	body        []byte
	hadWrite    bool
}

func newConnectResponseWriter(conn net.Conn) *connectResponseWriter {
	return &connectResponseWriter{
		conn:       conn,
		headers:    make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (w *connectResponseWriter) Header() http.Header { return w.headers }

func (w *connectResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = code
}

func (w *connectResponseWriter) Write(p []byte) (int, error) {
	w.hadWrite = true
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.body = append(w.body, p...)
	return len(p), nil
}

// finish serializes the buffered response and writes it to the underlying
// TLS conn as one HTTP/1.1 response with Content-Length and Connection: close.
func (w *connectResponseWriter) finish() error {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.headers.Set("Content-Length", fmt.Sprintf("%d", len(w.body)))
	w.headers.Set("Connection", "close")

	bw := bufio.NewWriter(w.conn)
	statusText := http.StatusText(w.statusCode)
	if _, err := fmt.Fprintf(bw, "HTTP/1.1 %d %s\r\n", w.statusCode, statusText); err != nil {
		return err
	}
	if err := w.headers.Write(bw); err != nil {
		return err
	}
	if _, err := bw.WriteString("\r\n"); err != nil {
		return err
	}
	if len(w.body) > 0 {
		if _, err := bw.Write(w.body); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// Hijack is intentionally absent — the inner ProxyHandler must not hijack the
// inner TLS conn. If a future inner handler needs streaming it should instead
// call Flush(); we'll add http.Flusher support when that lands.
var _ http.ResponseWriter = (*connectResponseWriter)(nil)
