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

// CABundle resolves the IssuingCA for a tenant. The egress gateway holds one
// IssuingCA per tenant it serves; before TLS termination, the CONNECT handler
// reads the verified caller's tenant from claims and asks the bundle for the
// right CA to mint a leaf with.
type CABundle interface {
	For(tenant string) (IssuingCA, error)
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
	bundle CABundle
	inner  http.Handler
	logger logging.Logger
}

// NewConnectHandler constructs a ConnectHandler. bundle resolves the per-tenant
// IssuingCA from the verified caller's claims; inner is invoked with the parsed
// *http.Request after TLS termination — pass the gateway's ProxyHandler so the
// established Phase 2a pipeline runs unchanged.
func NewConnectHandler(bundle CABundle, inner http.Handler, logger logging.Logger) *ConnectHandler {
	return &ConnectHandler{bundle: bundle, inner: inner, logger: logger}
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

	// Resolve the per-tenant CA before hijack so we fail cleanly with an HTTP
	// error if the bundle has no CA for this tenant. After hijack, errors can
	// only be surfaced as a closed connection.
	tenantID := claims.Orbit.TenantID
	if tenantID == "" {
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "platform token missing tenant", nil),
			ErrUnauthenticated)
		return
	}
	ca, err := h.bundle.For(tenantID)
	if err != nil {
		h.logger.Error(ctx, "no issuing CA for tenant",
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError, "tenant CA unavailable", nil),
			err)
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
	if _, writeErr := bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); writeErr != nil {
		h.logger.Warn(ctx, "write CONNECT 200 failed", logging.Field{Key: "error", Value: writeErr.Error()})
		return
	}
	if flushErr := bufrw.Flush(); flushErr != nil {
		h.logger.Warn(ctx, "flush CONNECT 200 failed", logging.Field{Key: "error", Value: flushErr.Error()})
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
		return ca.GetCertificate(hello)
	}

	tlsConn := tls.Server(clientConn, &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: getCert,
	})
	defer func() { _ = tlsConn.Close() }()

	if hsErr := tlsConn.HandshakeContext(ctx); hsErr != nil {
		h.logger.Warn(ctx, "tls handshake with client failed",
			logging.Field{Key: "host", Value: host},
			logging.Field{Key: "error", Value: hsErr.Error()},
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
// connection.
//
// It serializes the response as HTTP/1.1 with Transfer-Encoding: chunked, so
// large or open-ended upstream responses (git pack-receive, kubectl logs,
// SSE) flow byte-for-byte to the agent without buffering the full body in
// memory. Each Write becomes one chunk frame, flushed immediately. finish()
// writes the terminator frame.
//
// connectResponseWriter implements http.Flusher so httputil.ReverseProxy can
// propagate upstream flushes (the ReverseProxy is wired to flush whenever the
// upstream itself flushes its body).
type connectResponseWriter struct {
	bw          *bufio.Writer
	headers     http.Header
	wroteHeader bool
	statusCode  int
	headersOut  bool // true once the status line + headers have been emitted
	finishErr   error
}

func newConnectResponseWriter(conn net.Conn) *connectResponseWriter {
	return &connectResponseWriter{
		bw:         bufio.NewWriter(conn),
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

// Write emits one chunk frame per call. The first Write also serializes the
// status line and headers. A zero-length write is a no-op for chunk emission
// (a 0-length frame is the chunked-encoding terminator and must only appear
// in finish()).
func (w *connectResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.headersOut {
		if err := w.emitHeaders(); err != nil {
			return 0, err
		}
	}
	if len(p) == 0 {
		return 0, nil
	}
	if _, err := fmt.Fprintf(w.bw, "%x\r\n", len(p)); err != nil {
		return 0, err
	}
	if _, err := w.bw.Write(p); err != nil {
		return 0, err
	}
	if _, err := w.bw.WriteString("\r\n"); err != nil {
		return 0, err
	}
	if err := w.bw.Flush(); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush implements http.Flusher. It triggers header emission if the inner
// handler has only set headers (no Write yet); otherwise it is a no-op
// because every Write already flushes its chunk to the wire.
func (w *connectResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.headersOut {
		// Best-effort — if header emission fails the next Write will see the
		// error. We can't return an error from http.Flusher.
		w.finishErr = w.emitHeaders()
		return
	}
	// Subsequent flushes have nothing to push: each Write already wrote and
	// flushed a complete chunk frame.
	_ = w.bw.Flush()
}

// emitHeaders writes the status line and headers to the wire. Called exactly
// once per response, on the first Write or first Flush.
func (w *connectResponseWriter) emitHeaders() error {
	w.headersOut = true
	w.headers.Set("Transfer-Encoding", "chunked")
	w.headers.Set("Connection", "close")
	// Content-Length is incompatible with chunked; strip if the inner handler
	// set one.
	w.headers.Del("Content-Length")

	statusText := http.StatusText(w.statusCode)
	if _, err := fmt.Fprintf(w.bw, "HTTP/1.1 %d %s\r\n", w.statusCode, statusText); err != nil {
		return err
	}
	if err := w.headers.Write(w.bw); err != nil {
		return err
	}
	if _, err := w.bw.WriteString("\r\n"); err != nil {
		return err
	}
	return w.bw.Flush()
}

// finish writes the chunked-encoding terminator and flushes. If the inner
// handler never wrote anything (and never explicitly called Flush), finish
// emits the headers first so the response is well-formed.
func (w *connectResponseWriter) finish() error {
	if w.finishErr != nil {
		return w.finishErr
	}
	if !w.headersOut {
		if err := w.emitHeaders(); err != nil {
			return err
		}
	}
	if _, err := w.bw.WriteString("0\r\n\r\n"); err != nil {
		return err
	}
	return w.bw.Flush()
}

// Hijack is intentionally absent — the inner ProxyHandler must not hijack the
// inner TLS conn. If a future inner handler needs streaming it should instead
// call Flush(); we'll add http.Flusher support when that lands.
var _ http.ResponseWriter = (*connectResponseWriter)(nil)
