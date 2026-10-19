package egressgw

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// relayDialTimeout bounds the upstream TCP connect on the raw-relay path.
const relayDialTimeout = 10 * time.Second

// relayIdleTimeout tears down a relayed tunnel after this much inactivity in
// both directions. It reclaims abandoned tunnels without cutting off
// legitimately long-lived but quiet sessions (interactive SSH, a pooled DB
// connection) too aggressively.
const relayIdleTimeout = 10 * time.Minute

// serveRelay handles a CONNECT to a non-intercepted port. It opens a plain TCP
// tunnel to the upstream and shuttles bytes both ways, with no TLS termination,
// no HTTP parsing and no credential injection. Open and close are audit-logged.
func (h *ConnectHandler) serveRelay(ctx context.Context, w http.ResponseWriter, claims *token.PlatformClaims, host, port string) {
	ctx, span := tracer.Start(ctx, "egress.relay "+host,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("server.address", host),
			attribute.String("server.port", port),
			attribute.String("orbit.tenant_id", claims.Orbit.TenantID),
			attribute.String("orbit.principal_id", claims.Subject),
		))
	defer span.End()

	target := net.JoinHostPort(host, port)

	// Dial the upstream BEFORE hijack so a failure surfaces as a clean HTTP
	// error to the tool rather than a silently-closed tunnel.
	dialer := &net.Dialer{Timeout: relayDialTimeout}
	upstream, dialErr := dialer.DialContext(ctx, "tcp", target)
	if dialErr != nil {
		span.RecordError(dialErr)
		span.SetStatus(codes.Error, errRelayDial.Error())
		h.logger.Warn(ctx, "egress relay upstream dial failed",
			logging.Field{Key: "host", Value: host},
			logging.Field{Key: "port", Value: port},
			logging.Field{Key: "error", Value: dialErr.Error()},
		)
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError, "upstream unreachable", nil),
			dialErr)
		return
	}
	defer func() { _ = upstream.Close() }()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError,
				"server does not support connection hijacking", nil), nil)
		return
	}
	clientConn, bufrw, hijackErr := hijacker.Hijack()
	if hijackErr != nil {
		h.logger.Error(ctx, "hijack failed (relay)", logging.Field{Key: "error", Value: hijackErr.Error()})
		return
	}
	defer func() { _ = clientConn.Close() }()

	if _, err := bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := bufrw.Flush(); err != nil {
		return
	}

	h.logger.Info(ctx, "egress relay opened",
		logging.Field{Key: "host", Value: host},
		logging.Field{Key: "port", Value: port},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
	)

	// Forward any bytes the tool pipelined after CONNECT. Clients normally wait
	// for the 200, but the hijacked reader may already hold them.
	if n := bufrw.Reader.Buffered(); n > 0 {
		if pending, peekErr := bufrw.Peek(n); peekErr == nil {
			if _, wErr := upstream.Write(pending); wErr != nil {
				return
			}
			_, _ = bufrw.Discard(n)
		}
	}

	start := time.Now()
	sent, recv := relayTCP(clientConn, upstream, relayIdleTimeout)

	span.SetAttributes(
		attribute.Int64("orbit.relay.bytes_sent", sent),
		attribute.Int64("orbit.relay.bytes_received", recv),
	)
	h.logger.Info(ctx, "egress relay closed",
		logging.Field{Key: "host", Value: host},
		logging.Field{Key: "port", Value: port},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "bytes_sent", Value: sent},
		logging.Field{Key: "bytes_recv", Value: recv},
		logging.Field{Key: "duration_ms", Value: time.Since(start).Milliseconds()},
	)
}

// relayTCP shuttles bytes between the agent (client) and the upstream until
// either side closes or the idle timeout fires. It returns bytes sent
// (client→upstream) and received (upstream→client). Each direction half-closes
// its write side on EOF so protocols that signal end-of-stream that way (e.g. a
// graceful SQL/SSH shutdown) propagate correctly.
func relayTCP(client, upstream net.Conn, idle time.Duration) (sent, recv int64) {
	c := &idleConn{Conn: client, idle: idle}
	u := &idleConn{Conn: upstream, idle: idle}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sent, _ = io.Copy(u, c) // client to upstream
		closeWrite(upstream)
	}()
	go func() {
		defer wg.Done()
		recv, _ = io.Copy(c, u) // upstream to client
		closeWrite(client)
	}()
	wg.Wait()
	return sent, recv
}

// closeWrite half-closes the write side of a TCP connection if supported, so
// the peer sees EOF while the other direction keeps flowing.
func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

// idleConn refreshes a read/write deadline on every I/O so an idle relay is
// torn down after `idle` of no activity in either direction, while an active
// one is never timed out.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleConn) Read(b []byte) (int, error) {
	_ = c.SetReadDeadline(time.Now().Add(c.idle))
	return c.Conn.Read(b)
}

func (c *idleConn) Write(b []byte) (int, error) {
	_ = c.SetWriteDeadline(time.Now().Add(c.idle))
	return c.Conn.Write(b)
}
